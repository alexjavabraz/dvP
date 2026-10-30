package main

import (
	"context"
	"encoding/hex"
	"math/big"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// TokenInfo guarda os metadados do ERC-20 monitorado (lidos com eth_call).
type TokenInfo struct {
	Address     Address
	Enabled     bool
	Name        string
	Symbol      string
	Decimals    int
	TotalSupply *big.Int // totalSupply() lido no bloco SupplyBlock
	SupplyBlock uint64   // bloco da leitura do totalSupply (0 = ainda nao lido)
	SupplyErr   string
	Loaded      bool
	Err         string
	UpdatedAt   time.Time
}

type TokenWatcher struct {
	rpc      *RPC
	mu       sync.RWMutex
	info     TokenInfo
	fallback int

	kick     chan struct{}
	source   func() (block uint64, ok bool) // ultimo bloco indexado, so depois da sincronizacao
	onUpdate func()                         // chamado quando o totalSupply muda de bloco
}

func NewTokenWatcher(rpc *RPC, addr Address, enabled bool, fallbackDecimals int) *TokenWatcher {
	return &TokenWatcher{rpc: rpc, info: TokenInfo{Address: addr, Enabled: enabled, Decimals: fallbackDecimals}, fallback: fallbackDecimals,
		kick: make(chan struct{}, 1), onUpdate: func() {}}
}

// Follow liga o totalSupply ao indexador: ele e lido no ultimo bloco indexado, para comparar
// com a soma dos eventos do mesmo bloco (ler no "latest" dava divergencia falsa logo apos emissao/queima).
func (w *TokenWatcher) Follow(source func() (uint64, bool), onUpdate func()) {
	w.source, w.onUpdate = source, onUpdate
}

// Kick pede uma nova leitura do totalSupply (chamado a cada bloco indexado); nao bloqueia.
func (w *TokenWatcher) Kick() {
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

func (w *TokenWatcher) Info() TokenInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.info
}

func (w *TokenWatcher) Run(ctx context.Context) {
	if !w.info.Enabled {
		return
	}
	w.refresh()
	meta := time.NewTicker(30 * time.Second) // nome, simbolo e decimais quase nunca mudam
	defer meta.Stop()
	var lastRead uint64
	read := false
	for {
		if w.source != nil {
			if block, ok := w.source(); ok && (!read || block != lastRead) && w.readSupply(block) {
				lastRead, read = block, true
				w.onUpdate()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-w.kick:
		case <-meta.C:
			w.refresh()
		}
	}
}

// readSupply le totalSupply() no bloco dado.
func (w *TokenWatcher) readSupply(block uint64) bool {
	var out HexBytes
	err := w.rpc.Do("eth_call", &out, map[string]string{"to": w.info.Address.Hex(), "data": selector("totalSupply()")}, hexU(block))
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		w.info.SupplyErr = err.Error()
		return false
	}
	w.info.SupplyErr = ""
	if b := []byte(out); len(b) >= 32 {
		w.info.TotalSupply = new(big.Int).SetBytes(b[:32])
		w.info.SupplyBlock = block
	}
	return true
}

func selector(sig string) string {
	h := Keccak256([]byte(sig))
	return "0x" + hex.EncodeToString(h[:4])
}

func (w *TokenWatcher) call(data string) ([]byte, error) {
	var out HexBytes
	err := w.rpc.Do("eth_call", &out, map[string]string{"to": w.info.Address.Hex(), "data": data}, "latest")
	return []byte(out), err
}

func (w *TokenWatcher) refresh() {
	addr := w.info.Address.Hex()
	calls := []*Call{
		{Method: "eth_call", Params: []interface{}{map[string]string{"to": addr, "data": selector("name()")}, "latest"}, Result: new(HexBytes)},
		{Method: "eth_call", Params: []interface{}{map[string]string{"to": addr, "data": selector("symbol()")}, "latest"}, Result: new(HexBytes)},
		{Method: "eth_call", Params: []interface{}{map[string]string{"to": addr, "data": selector("decimals()")}, "latest"}, Result: new(HexBytes)},
	}
	err := w.rpc.Batch(calls)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.info.UpdatedAt = time.Now()
	if err != nil {
		w.info.Err = err.Error()
		return
	}
	w.info.Err = ""
	if calls[0].Err == nil {
		w.info.Name = decodeABIString([]byte(*calls[0].Result.(*HexBytes)))
	}
	if calls[1].Err == nil {
		w.info.Symbol = decodeABIString([]byte(*calls[1].Result.(*HexBytes)))
	}
	if calls[2].Err == nil {
		if b := []byte(*calls[2].Result.(*HexBytes)); len(b) >= 32 {
			w.info.Decimals = int(new(big.Int).SetBytes(b[:32]).Int64())
		}
	} else {
		w.info.Decimals = w.fallback
	}
	if w.info.Symbol == "" {
		w.info.Symbol = "BRLN"
	}
	w.info.Loaded = true
}

// BalanceOf consulta o saldo atual no contrato (para conferir o saldo calculado pelos eventos).
func (w *TokenWatcher) BalanceOf(a Address) (*big.Int, error) {
	data := selector("balanceOf(address)") + strings.Repeat("0", 24) + hex.EncodeToString(a[:])
	b, err := w.call(data)
	if err != nil {
		return nil, err
	}
	if len(b) < 32 {
		return new(big.Int), nil
	}
	return new(big.Int).SetBytes(b[:32]), nil
}

// decodeABIString decodifica um retorno `string` ABI (ou bytes32, para tokens antigos).
func decodeABIString(b []byte) string {
	if len(b) == 32 {
		return strings.TrimRight(string(b), "\x00")
	}
	if len(b) < 64 {
		return ""
	}
	off := new(big.Int).SetBytes(b[:32]).Uint64()
	if off+32 > uint64(len(b)) {
		return ""
	}
	l := new(big.Int).SetBytes(b[off : off+32]).Uint64()
	if off+32+l > uint64(len(b)) {
		return ""
	}
	s := string(b[off+32 : off+32+l])
	if !utf8.ValidString(s) {
		return ""
	}
	return s
}
