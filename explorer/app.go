package main

// App guarda a configuracao e o "runtime" atual (no RPC + banco em memoria + indexador).
// Ao salvar uma nova configuracao pela tela de configuracao (/explorer-conf/admin), o runtime antigo e descartado
// e um novo comeca a indexar do zero.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	DefaultRPC  = "http://127.0.0.1:8545"
	DefaultBRLN = ""
)

type Config struct {
	RPC    string            `json:"rpc"`
	BRLN   string            `json:"brln"`
	Labels map[string]string `json:"labels,omitempty"` // endereco -> rotulo
	Logo   string            `json:"logo,omitempty"`   // arquivo do logo (mesma pasta da config)
	// arquivo do logo para o tema escuro (opcional)
	LogoDark string `json:"logoDark,omitempty"`
}

func DefaultConfig() Config { return Config{RPC: DefaultRPC, BRLN: DefaultBRLN} }

type Options struct {
	StartBlock   uint64
	Batch        int
	Workers      int
	Poll         time.Duration
	Timeout      time.Duration
	BRLNDecimals int
}

type Runtime struct {
	Cfg     Config
	RPC     *RPC
	Store   *Store
	Ix      *Indexer
	Token   *TokenWatcher
	Started time.Time
	cancel  context.CancelFunc
}

type App struct {
	mu       sync.RWMutex
	rt       *Runtime
	parent   context.Context
	opts     Options
	cfgPath  string
	hub      *Hub
	labels   map[Address]string
	logo     string
	logoDark string
	// opcoes de linha de comando que sobrepoem o arquivo a cada inicio
	CLIOverrides []string
}

func NewApp(parent context.Context, opts Options, cfgPath string) *App {
	return &App{parent: parent, opts: opts, cfgPath: cfgPath, labels: map[Address]string{}, hub: NewHub()}
}

func (a *App) RT() *Runtime {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.rt
}

func (a *App) ConfigPath() string { return a.cfgPath }

// NormalizeConfig valida o formato dos campos.
func NormalizeConfig(c Config) (Config, error) {
	c.RPC = strings.TrimSpace(c.RPC)
	c.BRLN = strings.TrimSpace(c.BRLN)
	if c.RPC == "" {
		return c, errors.New("informe o endereço RPC do nó")
	}
	u, err := url.Parse(c.RPC)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return c, errors.New("endereço RPC inválido: use http://host:porta (ex.: " + DefaultRPC + ")")
	}
	if c.BRLN != "" {
		ad, ok := ParseAddress(c.BRLN)
		if !ok {
			return c, errors.New("endereço do BRLN inválido: deve ter 0x + 40 caracteres hexadecimais")
		}
		c.BRLN = ad.Hex()
	}
	return c, nil
}

// CheckConfig testa a configuracao contra o no antes de aplicar.
func (a *App) CheckConfig(c Config) (chainID uint64, err error) {
	r := NewRPC(c.RPC, 5*time.Second)
	var cid HexU64
	if err := r.Do("eth_chainId", &cid); err != nil {
		return 0, fmt.Errorf("o nó não respondeu em %s: %v", c.RPC, err)
	}
	if c.BRLN != "" {
		var code HexBytes
		if err := r.Do("eth_getCode", &code, c.BRLN, "latest"); err != nil {
			return 0, fmt.Errorf("não foi possível consultar o BRLN: %v", err)
		}
		if len(code) == 0 {
			return 0, fmt.Errorf("não há contrato no endereço %s nesta rede", c.BRLN)
		}
	}
	return uint64(cid), nil
}

// Start descarta o runtime atual (se houver) e inicia outro com a configuracao dada.
func (a *App) Start(c Config) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rt != nil {
		a.rt.cancel()
	}
	var tokenAddr Address
	tokenOn := false
	if c.BRLN != "" {
		tokenAddr, tokenOn = ParseAddress(c.BRLN)
	}
	ctx, cancel := context.WithCancel(a.parent)
	rpc := NewRPC(c.RPC, a.opts.Timeout)
	store := NewStore(a.opts.StartBlock, tokenAddr, tokenOn)
	tok := NewTokenWatcher(rpc, tokenAddr, tokenOn, a.opts.BRLNDecimals)
	hub := a.hub
	ix := NewIndexer(rpc, store, a.opts.Batch, a.opts.Workers, a.opts.Poll, func() { hub.Publish(); tok.Kick() })
	tok.Follow(func() (uint64, bool) {
		if !ix.Status().InitialDone { // blocos antigos podem nao ter estado no no (Bonsai): so confere sincronizado
			return 0, false
		}
		es := store.SupplyFromEvents()
		return es.LastBlock, es.HasBlocks
	}, hub.Publish)
	c.Labels, c.Logo, c.LogoDark = nil, "", "" // ficam no App, nao no runtime
	a.rt = &Runtime{Cfg: c, RPC: rpc, Store: store, Ix: ix, Token: tok, Started: time.Now(), cancel: cancel}
	go ix.Run(ctx)
	a.hub.Publish() // banco novo (reindexacao): as paginas abertas se atualizam
	go tok.Run(ctx)
	log.Printf("indexando | no: %s | BRLN: %s | banco: memoria", c.RPC, orDash(c.BRLN))
}

// LoadConfig le o arquivo de configuracao; se nao existir, retorna os padroes.
func LoadConfig(path string) (Config, bool, error) {
	c := DefaultConfig()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return DefaultConfig(), false, fmt.Errorf("%s: %w", path, err)
	}
	return c, true, nil
}

// SaveConfig grava a configuracao de forma atomica.
func SaveConfig(path string, c Config) error {
	raw, _ := json.MarshalIndent(c, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
