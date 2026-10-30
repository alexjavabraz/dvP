package main

import (
	"math/big"
	"testing"
)

func transferLog(token, from, to Address, v int64) Log {
	var t1, t2 Hash
	copy(t1[12:], from[:])
	copy(t2[12:], to[:])
	val := make([]byte, 32)
	big.NewInt(v).FillBytes(val)
	return Log{Address: token, Topics: []Hash{transferTopic, t1, t2}, Data: val}
}

// A conferencia do supply so compara leituras do contrato feitas depois da ultima emissao/queima.
func TestCheckSupply(t *testing.T) {
	token, _ := ParseAddress("0x00000000000000000000000000000000000000b1")
	a, _ := ParseAddress("0x00000000000000000000000000000000000000aa")
	b, _ := ParseAddress("0x00000000000000000000000000000000000000bb")
	zero := Address{}
	st := NewStore(1, token, true)
	tok := NewTokenWatcher(nil, token, true, 6)
	ix := &Indexer{store: st}
	rt := &Runtime{Store: st, Token: tok, Ix: ix}
	block := func(n uint64, logs ...Log) {
		var txs []*Tx
		if len(logs) > 0 {
			txs = []*Tx{{Hash: Hash{byte(n)}, BlockNumber: n, Logs: logs, Value: new(big.Int), GasPrice: new(big.Int)}}
		}
		st.AddBlock(n, BlockSummary{Hash: Hash{0xff, byte(n)}}, txs)
	}
	setSupply := func(v int64, at uint64) {
		tok.mu.Lock()
		tok.info.TotalSupply, tok.info.SupplyBlock = big.NewInt(v), at
		tok.mu.Unlock()
	}
	expect := func(want string) {
		t.Helper()
		if got := checkSupply(rt).State; got != want {
			t.Fatalf("estado %q, quer %q", got, want)
		}
	}

	block(1, transferLog(token, zero, a, 100)) // emissao
	block(2, transferLog(token, a, b, 30))     // transferencia: nao muda o total
	setSupply(100, 1)
	expect("aguardando") // indexador ainda sincronizando
	ix.initialDone = true
	expect("ok") // leitura no bloco 1 vale ate o bloco 2

	block(3, transferLog(token, zero, b, 50)) // nova emissao
	expect("conferindo")                      // contrato ainda lido no bloco 1: sem aviso falso
	setSupply(150, 3)
	expect("ok")

	block(4, transferLog(token, b, zero, 20)) // queima
	setSupply(150, 4)                         // contrato nao refletiu a queima: divergencia real
	expect("divergente")
	setSupply(130, 4)
	expect("ok")
}
