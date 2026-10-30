package main

// Pagina de transacoes pendentes (mempool) via API TXPOOL do Besu.
// Usa os metodos txpool_besu* (presentes nas versoes atuais do Besu) e calcula,
// pelo nonce confirmado de cada conta, quais transacoes estao prontas e quais
// estao paradas esperando um nonce anterior.

import (
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type poolTx struct {
	Hash     JHash    `json:"hash"`
	From     JAddr    `json:"from"`
	To       JAddr    `json:"to"`
	Value    HexBig   `json:"value"`
	Input    HexBytes `json:"input"`
	Nonce    HexU64   `json:"nonce"`
	Gas      HexU64   `json:"gas"`
	GasPrice HexBig   `json:"gasPrice"`
	MaxFee   HexBig   `json:"maxFeePerGas"`
}

type poolMeta struct {
	Hash    JHash  `json:"hash"`
	Local   bool   `json:"isReceivedFromLocalSource"`
	AddedAt string `json:"addedToPoolAt"`
}

type poolStats struct {
	MaxSize     HexOrNum `json:"maxSize"`
	LocalCount  HexOrNum `json:"localCount"`
	RemoteCount HexOrNum `json:"remoteCount"`
}

// HexOrNum aceita numero JSON ou string hexadecimal.
type HexOrNum uint64

func (h *HexOrNum) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if strings.HasPrefix(s, "0x") {
		v, err := strconv.ParseUint(s[2:], 16, 64)
		*h = HexOrNum(v)
		return err
	}
	v, err := strconv.ParseUint(s, 10, 64)
	*h = HexOrNum(v)
	return err
}

// PendingRow e uma linha da tabela de pendentes.
type PendingRow struct {
	Hash         Hash
	From         Address
	To           Address
	HasTo        bool
	Value        *big.Int
	Input        []byte
	Nonce        uint64
	AccountNonce uint64
	Gas          uint64
	GasPrice     *big.Int
	State        string // "pronta", "aguardando", "antiga"
	WaitingFor   uint64
	AddedAt      time.Time
	Local        bool
}

const pendingLimit = 1000

type pendingView struct {
	base
	Unavailable bool
	Err         string
	Rows        []PendingRow
	Total       int
	Ready       int
	Waiting     int
	Stats       poolStats
	HasStats    bool
	Filter      string
	BadFilter   bool
	Truncated   bool
}

func (s *Server) pending(w http.ResponseWriter, r *http.Request) {
	rt := s.app.RT()
	v := pendingView{base: s.base(r, "Pendentes", "pending")}
	q := strings.TrimSpace(r.URL.Query().Get("addr"))
	var filter *Address
	if q != "" {
		if a, ok := s.resolveAddr(q); ok {
			filter = &a
			v.Filter = a.Hex()
		} else {
			v.Filter, v.BadFilter = q, true
		}
	}

	var txs []poolTx
	var metas []poolMeta
	calls := []*Call{
		{Method: "txpool_besuPendingTransactions", Params: []interface{}{pendingLimit}, Result: &txs},
		{Method: "txpool_besuTransactions", Result: &metas},
		{Method: "txpool_besuStatistics", Result: &v.Stats},
	}
	if err := rt.RPC.Batch(calls); err != nil {
		v.Err = err.Error()
		s.render(w, "pending", v)
		return
	}
	if calls[0].Err != nil {
		if calls[0].MethodUnavailable() {
			v.Unavailable = true
		} else {
			v.Err = calls[0].Err.Error()
		}
		s.render(w, "pending", v)
		return
	}
	v.HasStats = calls[2].Err == nil
	v.Truncated = len(txs) >= pendingLimit

	added := map[Hash]poolMeta{}
	if calls[1].Err == nil {
		for _, m := range metas {
			added[m.Hash.Hash] = m
		}
	}

	// nonce confirmado de cada remetente
	senders := map[Address]uint64{}
	for _, t := range txs {
		senders[t.From.Address] = 0
	}
	var ncalls []*Call
	var order []Address
	outs := make([]HexU64, len(senders))
	i := 0
	for a := range senders {
		ncalls = append(ncalls, &Call{Method: "eth_getTransactionCount", Params: []interface{}{a.Hex(), "latest"}, Result: &outs[i]})
		order = append(order, a)
		i++
	}
	if err := rt.RPC.Batch(ncalls); err == nil {
		for i, c := range ncalls {
			if c.Err == nil {
				senders[order[i]] = uint64(outs[i])
			}
		}
	}

	// classifica por remetente, em ordem de nonce
	bySender := map[Address][]poolTx{}
	for _, t := range txs {
		bySender[t.From.Address] = append(bySender[t.From.Address], t)
	}
	var rows []PendingRow
	for from, list := range bySender {
		sort.Slice(list, func(i, j int) bool { return list[i].Nonce < list[j].Nonce })
		expected := senders[from]
		gap := false
		for _, t := range list {
			row := PendingRow{Hash: t.Hash.Hash, From: from, To: t.To.Address, HasTo: t.To.Valid,
				Value: bigOrZero(t.Value.Int), Input: []byte(t.Input), Nonce: uint64(t.Nonce), AccountNonce: senders[from],
				Gas: uint64(t.Gas), GasPrice: bigOrZero(t.GasPrice.Int)}
			if row.GasPrice.Sign() == 0 && t.MaxFee.Int != nil {
				row.GasPrice = t.MaxFee.Int
			}
			switch {
			case row.Nonce < expected:
				row.State = "antiga" // nonce ja usado: sera descartada pelo no
			case !gap && row.Nonce == expected:
				row.State = "pronta"
				expected++
			default:
				gap = true
				row.State = "aguardando"
				row.WaitingFor = expected
			}
			if m, ok := added[row.Hash]; ok {
				row.Local = m.Local
				if ts, err := time.Parse(time.RFC3339Nano, m.AddedAt); err == nil {
					row.AddedAt = ts
				}
			}
			if filter != nil && row.From != *filter && !(row.HasTo && row.To == *filter) {
				continue
			}
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].AddedAt.Equal(rows[j].AddedAt) {
			return rows[i].AddedAt.Before(rows[j].AddedAt) // mais antigas primeiro: sao as que chamam atencao
		}
		if rows[i].From != rows[j].From {
			return rows[i].From.Hex() < rows[j].From.Hex()
		}
		return rows[i].Nonce < rows[j].Nonce
	})
	for _, r := range rows {
		if r.State == "pronta" {
			v.Ready++
		} else {
			v.Waiting++
		}
	}
	v.Rows, v.Total = rows, len(rows)
	s.render(w, "pending", v)
}

func sinceStr(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return ago(uint64(t.Unix()))
}
