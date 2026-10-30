package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Um recibo com dados invalidos NAO pode desligar eth_getBlockReceipts; so "metodo inexistente" pode.
func TestReceiptsFallbackOnlyOnMethodNotFound(t *testing.T) {
	var receiptsMode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(body, "eth_getBlockByNumber"):
			io.WriteString(w, `[{"jsonrpc":"2.0","id":`+idOf(body)+`,"result":{"number":"0x1","hash":"0x`+strings.Repeat("1", 64)+`","parentHash":"0x`+strings.Repeat("0", 64)+`","timestamp":"0x1","miner":"0x`+strings.Repeat("2", 40)+`","gasUsed":"0x0","size":"0x1","transactions":[{"hash":"0x`+strings.Repeat("3", 64)+`","from":"0x`+strings.Repeat("4", 40)+`","to":null,"value":"0x0","input":"0x","nonce":"0x0","gas":"0x1","gasPrice":"0x0","transactionIndex":"0x0","type":"0x0"}]}}]`)
		case strings.Contains(body, "eth_getBlockReceipts") && receiptsMode == "bad":
			io.WriteString(w, `[{"jsonrpc":"2.0","id":`+idOf(body)+`,"result":[{"transactionHash":"0xzz"}]}]`)
		case strings.Contains(body, "eth_getBlockReceipts") && receiptsMode == "missing":
			io.WriteString(w, `[{"jsonrpc":"2.0","id":`+idOf(body)+`,"error":{"code":-32601,"message":"Method not found"}}]`)
		case strings.Contains(body, "eth_getTransactionReceipt"):
			io.WriteString(w, `[{"jsonrpc":"2.0","id":`+idOf(body)+`,"result":{"transactionHash":"0x`+strings.Repeat("3", 64)+`","status":"0x1","gasUsed":"0x1","logs":[]}}]`)
		}
	}))
	defer srv.Close()
	ix := NewIndexer(NewRPC(srv.URL, 5*time.Second), NewStore(1, Address{}, false), 10, 1, time.Second, nil)

	receiptsMode = "bad"
	if _, _, err := ix.fetchChunk(1, 1); err == nil {
		t.Fatal("esperava erro com recibo invalido")
	}
	if !ix.receiptsByBlock {
		t.Fatal("recibo invalido desligou eth_getBlockReceipts indevidamente")
	}
	receiptsMode = "missing"
	if _, recs, err := ix.fetchChunk(1, 1); err != nil || len(recs[1]) != 1 {
		t.Fatalf("fallback falhou: %v %v", err, recs)
	}
	if ix.receiptsByBlock {
		t.Fatal("metodo inexistente deveria ativar o fallback")
	}
}

func idOf(body string) string {
	i := strings.Index(body, `"id":`) + 5
	j := strings.IndexAny(body[i:], ",}")
	return body[i : i+j]
}
