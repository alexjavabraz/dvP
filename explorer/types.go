package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------- tipos base

type Address [20]byte
type Hash [32]byte

func (a Address) Hex() string  { return "0x" + hex.EncodeToString(a[:]) }
func (h Hash) Hex() string     { return "0x" + hex.EncodeToString(h[:]) }
func (a Address) IsZero() bool { return a == Address{} }

func ParseAddress(s string) (Address, bool) {
	var a Address
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "0x"), "0X")
	if len(s) != 40 {
		return a, false
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return a, false
	}
	copy(a[:], b)
	return a, true
}

func ParseHash(s string) (Hash, bool) {
	var h Hash
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "0x"), "0X")
	if len(s) != 64 {
		return h, false
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return h, false
	}
	copy(h[:], b)
	return h, true
}

// ------------------------------------------------- campos hexadecimais do JSON-RPC

type HexU64 uint64

func (h *HexU64) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		*h = 0
		return nil
	}
	v, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
	if err != nil {
		return fmt.Errorf("hex u64 %q: %w", s, err)
	}
	*h = HexU64(v)
	return nil
}

type HexBig struct{ *big.Int }

func (h *HexBig) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v := new(big.Int)
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		h.Int = v
		return nil
	}
	if _, ok := v.SetString(s, 16); !ok {
		return fmt.Errorf("hex big %q", s)
	}
	h.Int = v
	return nil
}

type HexBytes []byte

func (h *HexBytes) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	s = strings.TrimPrefix(s, "0x")
	if len(s)%2 == 1 {
		s = "0" + s
	}
	v, err := hex.DecodeString(s)
	if err != nil {
		return err
	}
	*h = v
	return nil
}

type JAddr struct {
	Address
	Valid bool
}

func (a *JAddr) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	v, ok := ParseAddress(s)
	if !ok {
		return fmt.Errorf("endereco invalido %q", s)
	}
	a.Address, a.Valid = v, true
	return nil
}

type JHash struct{ Hash }

func (h *JHash) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, ok := ParseHash(s)
	if !ok {
		return fmt.Errorf("hash invalido %q", s)
	}
	h.Hash = v
	return nil
}

// ------------------------------------------------------- respostas do no

type RPCTx struct {
	Hash             JHash    `json:"hash"`
	From             JAddr    `json:"from"`
	To               JAddr    `json:"to"`
	Value            HexBig   `json:"value"`
	Input            HexBytes `json:"input"`
	Nonce            HexU64   `json:"nonce"`
	Gas              HexU64   `json:"gas"`
	GasPrice         HexBig   `json:"gasPrice"`
	TransactionIndex HexU64   `json:"transactionIndex"`
	Type             HexU64   `json:"type"`
}

type RPCBlock struct {
	Number       HexU64  `json:"number"`
	Hash         JHash   `json:"hash"`
	ParentHash   JHash   `json:"parentHash"`
	Timestamp    HexU64  `json:"timestamp"`
	Miner        JAddr   `json:"miner"`
	GasUsed      HexU64  `json:"gasUsed"`
	GasLimit     HexU64  `json:"gasLimit"`
	Size         HexU64  `json:"size"`
	Transactions []RPCTx `json:"transactions"`
}

type RPCLog struct {
	Address  JAddr    `json:"address"`
	Topics   []JHash  `json:"topics"`
	Data     HexBytes `json:"data"`
	LogIndex HexU64   `json:"logIndex"`
}

type RPCReceipt struct {
	TransactionHash JHash    `json:"transactionHash"`
	Status          HexU64   `json:"status"`
	GasUsed         HexU64   `json:"gasUsed"`
	ContractAddress JAddr    `json:"contractAddress"`
	Logs            []RPCLog `json:"logs"`
	RevertReason    string   `json:"revertReason"`
}
