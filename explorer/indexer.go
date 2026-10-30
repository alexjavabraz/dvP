package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"
)

// Indexer le a rede pelo JSON-RPC e preenche o Store em memoria.
type Indexer struct {
	rpc     *RPC
	store   *Store
	batch   int
	workers int
	poll    time.Duration
	notify  func() // chamado a cada lote de blocos gravado (aviso para as paginas abertas)

	mu              sync.RWMutex
	head            uint64
	chainID         uint64
	validators      []Address
	lastErr         string
	lastErrAt       time.Time
	syncStart       time.Time
	syncFrom        uint64
	initialDone     bool
	receiptsByBlock bool // eth_getBlockReceipts disponivel
}

type IndexStatus struct {
	Head        uint64
	Next        uint64
	ChainID     uint64
	Validators  []Address
	Syncing     bool
	InitialDone bool
	Percent     float64
	Rate        float64 // blocos/s na sincronizacao atual
	ETA         time.Duration
	LastErr     string
	LastErrAt   time.Time
}

// RecentErr indica erro do indexador no ultimo minuto (o ultimo erro fica guardado mesmo depois de resolvido).
func (st IndexStatus) RecentErr() bool {
	return st.LastErr != "" && time.Since(st.LastErrAt) < time.Minute
}

func NewIndexer(rpc *RPC, store *Store, batch, workers int, poll time.Duration, notify func()) *Indexer {
	if notify == nil {
		notify = func() {}
	}
	return &Indexer{rpc: rpc, store: store, batch: batch, workers: workers, poll: poll, notify: notify, receiptsByBlock: true}
}

func (ix *Indexer) Status() IndexStatus {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	next := ix.store.NextBlock()
	st := IndexStatus{Head: ix.head, Next: next, ChainID: ix.chainID, Validators: ix.validators,
		InitialDone: ix.initialDone, LastErr: ix.lastErr, LastErrAt: ix.lastErrAt}
	st.Syncing = next <= ix.head
	total := float64(ix.head + 1 - ix.store.startBlock)
	if total > 0 {
		st.Percent = 100 * float64(next-ix.store.startBlock) / total
		if st.Percent > 100 {
			st.Percent = 100
		}
	}
	if st.Syncing && !ix.syncStart.IsZero() {
		el := time.Since(ix.syncStart).Seconds()
		done := float64(next - ix.syncFrom)
		if el > 0 && done > 0 {
			st.Rate = done / el
			st.ETA = time.Duration(float64(ix.head+1-next)/st.Rate) * time.Second
		}
	}
	return st
}

func (ix *Indexer) setErr(err error) {
	ix.mu.Lock()
	ix.lastErr = err.Error()
	ix.lastErrAt = time.Now()
	ix.mu.Unlock()
	log.Printf("indexador: %v", err)
}

// Run executa o laco principal ate o contexto ser cancelado.
func (ix *Indexer) Run(ctx context.Context) {
	var cid HexU64
	if err := ix.rpc.Do("eth_chainId", &cid); err == nil {
		ix.mu.Lock()
		ix.chainID = uint64(cid)
		ix.mu.Unlock()
	}
	go ix.refreshValidators(ctx)
	for {
		if ctx.Err() != nil {
			return
		}
		var head HexU64
		if err := ix.rpc.Do("eth_blockNumber", &head); err != nil {
			ix.setErr(fmt.Errorf("eth_blockNumber: %w", err))
			sleepCtx(ctx, ix.poll*2)
			continue
		}
		ix.mu.Lock()
		ix.head = uint64(head)
		ix.mu.Unlock()
		next := ix.store.NextBlock()
		if next <= uint64(head) {
			if err := ix.syncRange(ctx, next, uint64(head)); err != nil {
				ix.setErr(err)
				sleepCtx(ctx, ix.poll*2)
				continue
			}
		} else {
			ix.mu.Lock()
			first := !ix.initialDone
			if first {
				ix.initialDone = true
				log.Printf("sincronizacao inicial concluida no bloco %d", uint64(head))
			}
			ix.mu.Unlock()
			if first {
				ix.notify() // fim da sincronizacao: as paginas trocam o aviso "sincronizando"
			}
			sleepCtx(ctx, ix.poll)
		}
	}
}

func (ix *Indexer) refreshValidators(ctx context.Context) {
	for {
		var vals []string
		if err := ix.rpc.Do("qbft_getValidatorsByBlockNumber", &vals, "latest"); err == nil {
			var out []Address
			for _, v := range vals {
				if a, ok := ParseAddress(v); ok {
					out = append(out, a)
				}
			}
			ix.mu.Lock()
			ix.validators = out
			ix.mu.Unlock()
		}
		if !sleepCtx(ctx, 30*time.Second) {
			return
		}
	}
}

type chunkResult struct {
	idx    int
	blocks []*RPCBlock
	recs   map[uint64][]RPCReceipt
	err    error
}

// syncRange busca [from, to] em lotes paralelos e grava em ordem.
func (ix *Indexer) syncRange(ctx context.Context, from, to uint64) error {
	ix.mu.Lock()
	if to-from > uint64(ix.batch) || ix.syncStart.IsZero() {
		ix.syncStart = time.Now()
		ix.syncFrom = from
	}
	ix.mu.Unlock()
	if to-from > uint64(ix.batch) {
		log.Printf("sincronizando blocos %d..%d (%d blocos)", from, to, to-from+1)
	}

	nChunks := int((to-from)/uint64(ix.batch)) + 1
	jobs := make(chan int)
	results := make(chan chunkResult, ix.workers*2)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for w := 0; w < ix.workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				a := from + uint64(i)*uint64(ix.batch)
				b := a + uint64(ix.batch) - 1
				if b > to {
					b = to
				}
				blocks, recs, err := ix.fetchChunkRetry(cctx, a, b)
				select {
				case results <- chunkResult{i, blocks, recs, err}:
				case <-cctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := 0; i < nChunks; i++ {
			select {
			case jobs <- i:
			case <-cctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()

	pending := map[int]chunkResult{}
	nextIdx := 0
	lastLog := time.Now()
	for r := range results {
		if r.err != nil {
			return r.err
		}
		pending[r.idx] = r
		for {
			c, ok := pending[nextIdx]
			if !ok {
				break
			}
			delete(pending, nextIdx)
			if err := ix.commit(c); err != nil {
				return err
			}
			ix.notify()
			nextIdx++
		}
		if time.Since(lastLog) > 10*time.Second {
			st := ix.Status()
			log.Printf("  progresso: bloco %d de %d (%.1f%%, %.0f blocos/s)", st.Next, st.Head, st.Percent, st.Rate)
			lastLog = time.Now()
		}
	}
	if nextIdx != nChunks {
		return ctx.Err()
	}
	return nil
}

func (ix *Indexer) commit(c chunkResult) error {
	for _, b := range c.blocks {
		num := uint64(b.Number)
		if last, ok := ix.store.LastHash(); ok && num > ix.store.startBlock && b.ParentHash.Hash != last {
			// QBFT tem finalidade imediata; isso nao deveria acontecer. Registra e segue.
			ix.setErr(fmt.Errorf("bloco %d: parentHash diferente do bloco anterior indexado", num))
		}
		sum := BlockSummary{Hash: b.Hash.Hash, Timestamp: uint64(b.Timestamp), Miner: b.Miner.Address,
			GasUsed: uint64(b.GasUsed), Size: uint32(b.Size), TxCount: uint32(len(b.Transactions))}
		var txs []*Tx
		if len(b.Transactions) > 0 {
			recs := map[Hash]*RPCReceipt{}
			for i := range c.recs[num] {
				r := &c.recs[num][i]
				recs[r.TransactionHash.Hash] = r
			}
			for _, rt := range b.Transactions {
				tx := &Tx{Hash: rt.Hash.Hash, BlockNumber: num, Index: uint32(rt.TransactionIndex), Timestamp: uint64(b.Timestamp),
					From: rt.From.Address, To: rt.To.Address, HasTo: rt.To.Valid, Value: bigOrZero(rt.Value.Int),
					Input: []byte(rt.Input), Nonce: uint64(rt.Nonce), Gas: uint64(rt.Gas), GasPrice: bigOrZero(rt.GasPrice.Int),
					Type: uint64(rt.Type)}
				if r, ok := recs[tx.Hash]; ok {
					tx.Status, tx.GasUsed, tx.RevertReason = uint64(r.Status), uint64(r.GasUsed), r.RevertReason
					tx.ContractAddress, tx.HasContract = r.ContractAddress.Address, r.ContractAddress.Valid
					for _, l := range r.Logs {
						lg := Log{Address: l.Address.Address, Data: []byte(l.Data), LogIndex: uint64(l.LogIndex)}
						for _, t := range l.Topics {
							lg.Topics = append(lg.Topics, t.Hash)
						}
						tx.Logs = append(tx.Logs, lg)
					}
				} else {
					return fmt.Errorf("recibo ausente para %s no bloco %d", tx.Hash.Hex(), num)
				}
				txs = append(txs, tx)
			}
		}
		ix.store.AddBlock(num, sum, txs)
	}
	return nil
}

func (ix *Indexer) fetchChunkRetry(ctx context.Context, a, b uint64) ([]*RPCBlock, map[uint64][]RPCReceipt, error) {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		blocks, recs, err := ix.fetchChunk(a, b)
		if err == nil {
			return blocks, recs, nil
		}
		lastErr = err
		if !sleepCtx(ctx, time.Duration(attempt+1)*time.Second) {
			return nil, nil, ctx.Err()
		}
	}
	return nil, nil, fmt.Errorf("blocos %d..%d: %w", a, b, lastErr)
}

func (ix *Indexer) fetchChunk(a, b uint64) ([]*RPCBlock, map[uint64][]RPCReceipt, error) {
	n := int(b - a + 1)
	blocks := make([]*RPCBlock, n)
	calls := make([]*Call, n)
	for i := 0; i < n; i++ {
		blocks[i] = &RPCBlock{}
		calls[i] = &Call{Method: "eth_getBlockByNumber", Params: []interface{}{hexU(a + uint64(i)), true}, Result: blocks[i]}
	}
	if err := ix.rpc.Batch(calls); err != nil {
		return nil, nil, err
	}
	for i, c := range calls {
		if c.Err != nil {
			return nil, nil, c.Err
		}
		if blocks[i].Hash.Hash == (Hash{}) {
			return nil, nil, fmt.Errorf("bloco %d nao retornado pelo no", a+uint64(i))
		}
	}
	recs := map[uint64][]RPCReceipt{}
	var withTx []*RPCBlock
	for _, bl := range blocks {
		if len(bl.Transactions) > 0 {
			withTx = append(withTx, bl)
		}
	}
	if len(withTx) == 0 {
		return blocks, recs, nil
	}
	ix.mu.RLock()
	byBlock := ix.receiptsByBlock
	ix.mu.RUnlock()
	if byBlock {
		rcalls := make([]*Call, len(withTx))
		out := make([][]RPCReceipt, len(withTx))
		for i, bl := range withTx {
			rcalls[i] = &Call{Method: "eth_getBlockReceipts", Params: []interface{}{hexU(uint64(bl.Number))}, Result: &out[i]}
		}
		if err := ix.rpc.Batch(rcalls); err != nil {
			return nil, nil, err
		}
		unavailable := false
		for i, c := range rcalls {
			if c.Err != nil {
				if !c.MethodUnavailable() {
					return nil, nil, c.Err // erro transitorio ou de dados: tenta de novo
				}
				unavailable = true
				break
			}
			recs[uint64(withTx[i].Number)] = out[i]
		}
		if !unavailable {
			return blocks, recs, nil
		}
		// no sem eth_getBlockReceipts: usa recibo por transacao daqui em diante
		log.Printf("eth_getBlockReceipts indisponivel (%v); usando eth_getTransactionReceipt", rcalls[0].Err)
		ix.mu.Lock()
		ix.receiptsByBlock = false
		ix.mu.Unlock()
		recs = map[uint64][]RPCReceipt{}
	}
	var tcalls []*Call
	var owners []uint64
	var outs []*RPCReceipt
	for _, bl := range withTx {
		for _, t := range bl.Transactions {
			r := &RPCReceipt{}
			tcalls = append(tcalls, &Call{Method: "eth_getTransactionReceipt", Params: []interface{}{t.Hash.Hex()}, Result: r})
			owners = append(owners, uint64(bl.Number))
			outs = append(outs, r)
		}
	}
	for start := 0; start < len(tcalls); start += ix.batch {
		end := start + ix.batch
		if end > len(tcalls) {
			end = len(tcalls)
		}
		if err := ix.rpc.Batch(tcalls[start:end]); err != nil {
			return nil, nil, err
		}
	}
	for i, c := range tcalls {
		if c.Err != nil {
			return nil, nil, c.Err
		}
		recs[owners[i]] = append(recs[owners[i]], *outs[i])
	}
	return blocks, recs, nil
}

// ------------------------------------------------------------------ utilidades

func hexU(n uint64) string { return fmt.Sprintf("0x%x", n) }

func bigOrZero(b *big.Int) *big.Int {
	if b == nil {
		return new(big.Int)
	}
	return b
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
