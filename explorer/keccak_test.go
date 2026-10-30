package main

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestKeccak256Vectors(t *testing.T) {
	cases := map[string]string{
		"":                                  "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
		"Transfer(address,address,uint256)": "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
		"TradeRegistered(uint256,uint256,address,address,uint256,uint64,uint256[],uint256)": "ffb4aed9bfc74183a77bc0ac033f09ea872bc676fd8658b4330a55f828846ed8",
		strings.Repeat("a", 200): "", // so garante que mensagens > 1 bloco nao quebram
	}
	for in, want := range cases {
		got := Keccak256([]byte(in))
		if want != "" && hex.EncodeToString(got[:]) != want {
			t.Errorf("keccak(%q) = %x, want %s", in, got, want)
		}
	}
	// transfer(address,uint256) -> a9059cbb
	h := Keccak256([]byte("transfer(address,uint256)"))
	if hex.EncodeToString(h[:4]) != "a9059cbb" {
		t.Errorf("selector transfer = %x", h[:4])
	}
}
