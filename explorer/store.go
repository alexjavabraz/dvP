package main

import (
	"math/big"
	"sort"
	"sync"
)

// BlockSummary e o registro compacto mantido para TODOS os blocos (inclusive vazios).
type BlockSummary struct {
	Hash      Hash
	Timestamp uint64
	Miner     Address
	GasUsed   uint64
	Size      uint32
	TxCount   uint32
}

type Log struct {
	Address  Address
	Topics   []Hash
	Data     []byte
	LogIndex uint64
}

// Tx guarda a transacao completa com o recibo.
type Tx struct {
	Hash        Hash
	BlockNumber uint64
	Index       uint32
	Timestamp   uint64
	From        Address
	To          Address
	HasTo       bool
	Value       *big.Int
	Input       []byte
	Nonce       uint64
	Gas         uint64
	GasPrice    *big.Int
	Type        uint64
	// recibo
	Status          uint64
	GasUsed         uint64
	ContractAddress Address
	HasContract     bool
	RevertReason    string
	Logs            []Log
}

// TokenTransfer e um evento Transfer(address,address,uint256) do token monitorado.
type TokenTransfer struct {
	TxHash      Hash
	BlockNumber uint64
	LogIndex    uint64
	Timestamp   uint64
	From        Address
	To          Address
	Value       *big.Int
}

// Store e o banco em memoria. Tudo some ao reiniciar o processo.
type Store struct {
	mu sync.RWMutex

	startBlock  uint64
	blocks      []BlockSummary    // blocks[n - startBlock]
	blockTxs    map[uint64][]Hash // so blocos com transacoes
	nonEmpty    []uint64          // numeros dos blocos com transacoes, em ordem
	txs         map[Hash]*Tx
	txOrder     []Hash             // ordem cronologica
	addrTxs     map[Address][]Hash // from / to / contrato criado
	blockByHash map[Hash]uint64

	token         Address
	tokenEnabled  bool
	transfers     []TokenTransfer
	addrTransfers map[Address][]int // indices em transfers
	tokenBalances map[Address]*big.Int
	supplyChange  uint64 // ultimo bloco com emissao ou queima (unicos eventos que mudam o total)
	hasSupplyChg  bool
}

func NewStore(startBlock uint64, token Address, tokenEnabled bool) *Store {
	return &Store{
		startBlock:    startBlock,
		blockTxs:      make(map[uint64][]Hash),
		txs:           make(map[Hash]*Tx),
		addrTxs:       make(map[Address][]Hash),
		blockByHash:   make(map[Hash]uint64),
		token:         token,
		tokenEnabled:  tokenEnabled,
		addrTransfers: make(map[Address][]int),
		tokenBalances: make(map[Address]*big.Int),
	}
}

var transferTopic = Keccak256([]byte("Transfer(address,address,uint256)"))

// NextBlock retorna o proximo bloco esperado.
func (s *Store) NextBlock() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.startBlock + uint64(len(s.blocks))
}

// LastHash retorna o hash do ultimo bloco indexado.
func (s *Store) LastHash() (Hash, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.blocks) == 0 {
		return Hash{}, false
	}
	return s.blocks[len(s.blocks)-1].Hash, true
}

// AddBlock insere um bloco (em ordem) com suas transacoes completas.
func (s *Store) AddBlock(num uint64, sum BlockSummary, txs []*Tx) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if num != s.startBlock+uint64(len(s.blocks)) {
		return // fora de ordem: o indexador garante a ordem; ignora defensivamente
	}
	s.blocks = append(s.blocks, sum)
	s.blockByHash[sum.Hash] = num
	if len(txs) == 0 {
		return
	}
	s.nonEmpty = append(s.nonEmpty, num)
	hashes := make([]Hash, 0, len(txs))
	for _, tx := range txs {
		hashes = append(hashes, tx.Hash)
		s.txs[tx.Hash] = tx
		s.txOrder = append(s.txOrder, tx.Hash)
		s.addrTxs[tx.From] = append(s.addrTxs[tx.From], tx.Hash)
		if tx.HasTo && tx.To != tx.From {
			s.addrTxs[tx.To] = append(s.addrTxs[tx.To], tx.Hash)
		}
		if tx.HasContract {
			s.addrTxs[tx.ContractAddress] = append(s.addrTxs[tx.ContractAddress], tx.Hash)
		}
		if s.tokenEnabled {
			s.indexTransfers(tx)
		}
	}
	s.blockTxs[num] = hashes
}

func (s *Store) indexTransfers(tx *Tx) {
	for _, l := range tx.Logs {
		// Transfer padrao ERC-20: 3 topics + valor em data (ERC-721 tem 4 topics e e ignorado)
		if l.Address != s.token || len(l.Topics) != 3 || l.Topics[0] != transferTopic || len(l.Data) < 32 {
			continue
		}
		var from, to Address
		copy(from[:], l.Topics[1][12:])
		copy(to[:], l.Topics[2][12:])
		val := new(big.Int).SetBytes(l.Data[:32])
		if from.IsZero() || to.IsZero() {
			s.supplyChange, s.hasSupplyChg = tx.BlockNumber, true
		}
		idx := len(s.transfers)
		s.transfers = append(s.transfers, TokenTransfer{tx.Hash, tx.BlockNumber, l.LogIndex, tx.Timestamp, from, to, val})
		if !from.IsZero() {
			s.addrTransfers[from] = append(s.addrTransfers[from], idx)
			s.addBalance(from, new(big.Int).Neg(val))
		}
		if !to.IsZero() && to != from {
			s.addrTransfers[to] = append(s.addrTransfers[to], idx)
		}
		if !to.IsZero() {
			s.addBalance(to, val)
		}
	}
}

func (s *Store) addBalance(a Address, delta *big.Int) {
	b, ok := s.tokenBalances[a]
	if !ok {
		b = new(big.Int)
		s.tokenBalances[a] = b
	}
	b.Add(b, delta)
}

// ------------------------------------------------------------------ consultas

type Stats struct {
	Indexed    uint64
	FirstBlock uint64
	LastBlock  uint64
	HasBlocks  bool
	NonEmpty   int
	TxCount    int
	Transfers  int
	Holders    int
}

func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := Stats{Indexed: uint64(len(s.blocks)), FirstBlock: s.startBlock, NonEmpty: len(s.nonEmpty),
		TxCount: len(s.txOrder), Transfers: len(s.transfers)}
	if len(s.blocks) > 0 {
		st.HasBlocks = true
		st.LastBlock = s.startBlock + uint64(len(s.blocks)) - 1
	}
	for _, b := range s.tokenBalances {
		if b.Sign() > 0 {
			st.Holders++
		}
	}
	return st
}

type BlockView struct {
	Number uint64
	BlockSummary
}

func (s *Store) Block(num uint64) (BlockView, []*Tx, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if num < s.startBlock || num >= s.startBlock+uint64(len(s.blocks)) {
		return BlockView{}, nil, false
	}
	b := BlockView{num, s.blocks[num-s.startBlock]}
	var txs []*Tx
	for _, h := range s.blockTxs[num] {
		txs = append(txs, s.txs[h])
	}
	return b, txs, true
}

func (s *Store) BlockByHash(h Hash) (uint64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.blockByHash[h]
	return n, ok
}

// Blocks lista blocos do mais novo para o mais antigo. Com onlyNonEmpty, so os com transacoes.
func (s *Store) Blocks(offset, limit int, onlyNonEmpty bool) ([]BlockView, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []BlockView
	if onlyNonEmpty {
		total := len(s.nonEmpty)
		for i := total - 1 - offset; i >= 0 && len(out) < limit; i-- {
			n := s.nonEmpty[i]
			out = append(out, BlockView{n, s.blocks[n-s.startBlock]})
		}
		return out, total
	}
	total := len(s.blocks)
	for i := total - 1 - offset; i >= 0 && len(out) < limit; i-- {
		out = append(out, BlockView{s.startBlock + uint64(i), s.blocks[i]})
	}
	return out, total
}

func (s *Store) Txs(offset, limit int) ([]*Tx, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := len(s.txOrder)
	var out []*Tx
	for i := total - 1 - offset; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.txs[s.txOrder[i]])
	}
	return out, total
}

func (s *Store) Tx(h Hash) (*Tx, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.txs[h]
	return t, ok
}

func (s *Store) AddressTxs(a Address, offset, limit int) ([]*Tx, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := s.addrTxs[a]
	total := len(list)
	var out []*Tx
	for i := total - 1 - offset; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.txs[list[i]])
	}
	return out, total
}

// Transfers lista transferencias do token (mais novas primeiro); filtro opcional por endereco.
func (s *Store) Transfers(filter *Address, offset, limit int) ([]TokenTransfer, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []TokenTransfer
	if filter != nil {
		idx := s.addrTransfers[*filter]
		total := len(idx)
		for i := total - 1 - offset; i >= 0 && len(out) < limit; i-- {
			out = append(out, s.transfers[idx[i]])
		}
		return out, total
	}
	total := len(s.transfers)
	for i := total - 1 - offset; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.transfers[i])
	}
	return out, total
}

// TxTransfers retorna as transferencias do token dentro de uma transacao.
func (s *Store) TxTransfers(h Hash) []TokenTransfer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.txs[h]
	if !ok {
		return nil
	}
	var out []TokenTransfer
	for _, l := range t.Logs {
		if l.Address != s.token || len(l.Topics) != 3 || l.Topics[0] != transferTopic || len(l.Data) < 32 {
			continue
		}
		var from, to Address
		copy(from[:], l.Topics[1][12:])
		copy(to[:], l.Topics[2][12:])
		out = append(out, TokenTransfer{h, t.BlockNumber, l.LogIndex, t.Timestamp, from, to, new(big.Int).SetBytes(l.Data[:32])})
	}
	return out
}

type Holder struct {
	Address Address
	Balance *big.Int
	Count   int
}

// Holders retorna os saldos calculados pelos eventos Transfer, maior saldo primeiro.
func (s *Store) Holders(limit int) []Holder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Holder
	for a, b := range s.tokenBalances {
		if b.Sign() == 0 {
			continue
		}
		out = append(out, Holder{a, new(big.Int).Set(b), len(s.addrTransfers[a])})
	}
	sort.Slice(out, func(i, j int) bool {
		if c := out[i].Balance.Cmp(out[j].Balance); c != 0 {
			return c > 0
		}
		return out[i].Address.Hex() < out[j].Address.Hex()
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) TokenBalance(a Address) (*big.Int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.tokenBalances[a]
	if !ok {
		return new(big.Int), 0
	}
	return new(big.Int).Set(b), len(s.addrTransfers[a])
}

// EventSupply e o total calculado pelos eventos, com os blocos necessarios para conferir com o contrato.
type EventSupply struct {
	Sum        *big.Int // emissoes - queimas
	LastBlock  uint64   // ultimo bloco indexado
	LastChange uint64   // ultimo bloco com emissao ou queima
	HasBlocks  bool
	HasChange  bool
}

// SupplyFromEvents le soma e blocos de uma vez (sob o mesmo lock), para nao comparar estados diferentes.
func (s *Store) SupplyFromEvents() EventSupply {
	s.mu.RLock()
	defer s.mu.RUnlock()
	es := EventSupply{Sum: new(big.Int), LastChange: s.supplyChange, HasChange: s.hasSupplyChg, HasBlocks: len(s.blocks) > 0}
	if es.HasBlocks {
		es.LastBlock = s.startBlock + uint64(len(s.blocks)) - 1
	}
	for _, b := range s.tokenBalances {
		es.Sum.Add(es.Sum, b)
	}
	return es
}
