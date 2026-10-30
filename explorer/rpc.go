package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// RPC e um cliente JSON-RPC minimo com suporte a lote (batch).
type RPC struct {
	url    string
	client *http.Client
	nextID atomic.Uint64
}

type rpcReq struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      uint64        `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcErr         `json:"error"`
}

// Call representa uma chamada dentro de um lote.
type Call struct {
	Method string
	Params []interface{}
	Result interface{} // ponteiro para decodificar o resultado
	Err    error
	Code   int // codigo de erro JSON-RPC (0 se nao houve erro do servidor)
}

// MethodUnavailable indica que o no nao tem ou nao habilitou o metodo.
func (c *Call) MethodUnavailable() bool { return c.Code == -32601 || c.Code == -32604 }

func NewRPC(url string, timeout time.Duration) *RPC {
	return &RPC{
		url: url,
		client: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{MaxIdleConns: 16, MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second},
		},
	}
}

func (r *RPC) post(body []byte) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, r.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rpc http %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	return data, nil
}

// Do executa uma unica chamada.
func (r *RPC) Do(method string, result interface{}, params ...interface{}) error {
	if params == nil {
		params = []interface{}{}
	}
	body, _ := json.Marshal(rpcReq{"2.0", r.nextID.Add(1), method, params})
	data, err := r.post(body)
	if err != nil {
		return err
	}
	var resp rpcResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("rpc decode %s: %w", method, err)
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s (code %d)", method, resp.Error.Message, resp.Error.Code)
	}
	if result != nil {
		return json.Unmarshal(resp.Result, result)
	}
	return nil
}

// Batch executa varias chamadas numa unica requisicao HTTP.
// Erros individuais ficam em calls[i].Err; o retorno e erro de transporte.
func (r *RPC) Batch(calls []*Call) error {
	if len(calls) == 0 {
		return nil
	}
	reqs := make([]rpcReq, len(calls))
	byID := make(map[uint64]*Call, len(calls))
	for i, c := range calls {
		p := c.Params
		if p == nil {
			p = []interface{}{}
		}
		id := r.nextID.Add(1)
		reqs[i] = rpcReq{"2.0", id, c.Method, p}
		byID[id] = c
	}
	body, _ := json.Marshal(reqs)
	data, err := r.post(body)
	if err != nil {
		return err
	}
	var resps []rpcResp
	if err := json.Unmarshal(data, &resps); err != nil {
		// alguns servidores devolvem um objeto de erro unico para o lote inteiro
		var single rpcResp
		if json.Unmarshal(data, &single) == nil && single.Error != nil {
			return fmt.Errorf("batch: %s (code %d)", single.Error.Message, single.Error.Code)
		}
		return fmt.Errorf("batch decode: %w", err)
	}
	for _, resp := range resps {
		c, ok := byID[resp.ID]
		if !ok {
			continue
		}
		delete(byID, resp.ID)
		if resp.Error != nil {
			c.Err = fmt.Errorf("%s: %s (code %d)", c.Method, resp.Error.Message, resp.Error.Code)
			c.Code = resp.Error.Code
			continue
		}
		if c.Result != nil {
			if err := json.Unmarshal(resp.Result, c.Result); err != nil {
				c.Err = fmt.Errorf("%s decode: %w", c.Method, err)
			}
		}
	}
	for _, c := range byID {
		c.Err = fmt.Errorf("%s: sem resposta no lote", c.Method)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
