package main

import "testing"

// O topic0 de Transfer e igual no ERC-20 e no ERC-721; o decodificador deve escolher pelo numero de topics.
func TestTransferERC20vsERC721(t *testing.T) {
	d := NewDecoder()
	d.addHuman("event", "Transfer(address indexed from,address indexed to,uint256 indexed tokenId)") // como a ABI do CCBToken
	topic := Hash(Keccak256([]byte("Transfer(address,address,uint256)")))
	var from, to Hash
	from[31], to[31] = 1, 2
	val := make([]byte, 32)
	val[31] = 7
	erc20 := Log{Topics: []Hash{topic, from, to}, Data: val}
	dec, ok := d.Event(erc20)
	if !ok || len(dec.Args) != 3 || dec.Args[2].Name != "value" || dec.Args[2].Value != "7" {
		t.Fatalf("ERC-20 mal decodificado: %+v", dec)
	}
	var id Hash
	id[31] = 9
	erc721 := Log{Topics: []Hash{topic, from, to, id}}
	dec, ok = d.Event(erc721)
	if !ok || len(dec.Args) != 3 || dec.Args[2].Name != "tokenId" || dec.Args[2].Value != "9" {
		t.Fatalf("ERC-721 mal decodificado: %+v", dec)
	}
}
