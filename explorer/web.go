package main

import (
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed web/templates/*.html web/static/*
var webFS embed.FS

type Server struct {
	app         *App
	dec         *Decoder
	tpl         map[string]*template.Template
	version     string
	auth        *Auth
	liveSeconds int
}

// AdminPath e a URL da tela de configuracao (fora do menu).
const AdminPath = "/explorer-conf/admin"

const pageSize = 25

func NewServer(app *App, dec *Decoder, version string, auth *Auth, liveSeconds int) *Server {
	s := &Server{app: app, dec: dec, version: version, auth: auth, liveSeconds: liveSeconds, tpl: map[string]*template.Template{}}
	funcs := template.FuncMap{
		"short":  shortHex,
		"time":   fmtTime,
		"ago":    ago,
		"int":    fmtInt,
		"dur":    fmtDuration,
		"hexb":   hexBytes,
		"units":  formatUnits,
		"native": func(v *big.Int) string { return formatUnits(v, 18) },
		"tok":    func(v *big.Int) string { return formatUnits(v, s.app.RT().Token.Info().Decimals) },
		"pct":    func(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) },
		"method": func(in []byte) string {
			d, ok := s.dec.Method(in)
			if ok {
				return d.Name
			}
			if len(in) >= 4 {
				return hexBytes(in[:4])
			}
			return ""
		},
		"isToken": func(a Address) bool { ti := s.app.RT().Token.Info(); return ti.Enabled && a == ti.Address },
		"add":     func(a, b int) int { return a + b },
		"uadd":    func(a, b uint64) uint64 { return a + b },
		"usub": func(a, b uint64) uint64 {
			if a < b {
				return 0
			}
			return a - b
		},
		"isZero": func(a Address) bool { return a.IsZero() },
		"label":  s.app.Label,
		"since":  sinceStr,
		"unix":   func(t time.Time) uint64 { return uint64(t.Unix()) },
	}
	pages := []string{"home", "blocks", "block", "txs", "tx", "address", "brln", "notfound", "config", "pending", "login"}
	for _, p := range pages {
		t := template.Must(template.New("layout.html").Funcs(funcs).ParseFS(webFS, "web/templates/layout.html", "web/templates/"+p+".html"))
		s.tpl[p] = t
	}
	return s
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("GET /branding/logo", s.serveLogo)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.loginSubmit)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /blocks", s.blocks)
	mux.HandleFunc("GET /block/{id}", s.block)
	mux.HandleFunc("GET /txs", s.txs)
	mux.HandleFunc("GET /tx/{hash}", s.tx)
	mux.HandleFunc("GET /address/{addr}", s.address)
	mux.HandleFunc("GET /brln", s.brln)
	mux.HandleFunc("GET /search", s.search)
	mux.HandleFunc("GET /api/status", s.apiStatus)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /pending", s.pending)
	mux.HandleFunc("GET "+AdminPath, s.configPage)
	mux.HandleFunc("POST "+AdminPath, s.configSave)
	return logRequests(s.requireLogin(mux))
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		h.ServeHTTP(w, r)
	})
}

type base struct {
	Title    string
	Nav      string
	Status   IndexStatus
	Token    TokenInfo
	Version  string
	Query    string
	Live     int   // intervalo da atualizacao automatica em segundos (0 = desligada)
	Now      int64 // relogio do servidor (unix), para os "ha N s" calculados no navegador
	Admin    string
	UITheme  string // cor-tema da pagina (design system Nuclea): lilas, lima ou verde
	Logo     string // URL do logo (cabecalho e carregamento); vazio = marca padrao
	LogoDark string // URL do logo para o tema escuro (vazio = usa Logo)
	User     *User  // usuario logado (nil na tela de login)
}

func (s *Server) base(r *http.Request, title, nav string) base {
	rt := s.app.RT()
	live := s.liveSeconds
	if nav == "config" || nav == "login" {
		live = 0 // configuracao e login nao se atualizam sozinhos
	}
	return base{Title: title, Nav: nav, UITheme: uiTheme(nav), Now: time.Now().Unix(), Status: rt.Ix.Status(), Token: rt.Token.Info(), Version: s.version, Live: live, Admin: AdminPath,
		Logo: s.app.LogoURL(false), LogoDark: s.app.LogoURL(true), User: userFrom(r)}
}

// uiTheme: uma cor-tema por area (o guia proibe misturar cores-tema numa mesma tela).
func uiTheme(nav string) string {
	switch nav {
	case "brln":
		return "verde"
	case "config":
		return "lima"
	}
	return "lilas"
}

func (s *Server) render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl[name].ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("template %s: %v", name, err)
	}
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request, msg string, q string) {
	w.WriteHeader(http.StatusNotFound)
	s.render(w, "notfound", struct {
		base
		Msg string
	}{s.base(r, "Não encontrado", ""), msg})
}

func pageParam(r *http.Request) int {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		p = 1
	}
	return p
}

// ------------------------------------------------------------------ paginas

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	rt := s.app.RT()
	blocks, _ := rt.Store.Blocks(0, 10, false)
	txs, _ := rt.Store.Txs(0, 10)
	s.render(w, "home", struct {
		base
		Stats  Stats
		Blocks []BlockView
		Txs    []*Tx
	}{s.base(r, "Início", "home"), rt.Store.Stats(), blocks, txs})
}

func (s *Server) blocks(w http.ResponseWriter, r *http.Request) {
	rt := s.app.RT()
	only := r.URL.Query().Get("comtx") == "1"
	_, total := rt.Store.Blocks(0, 0, only)
	baseURL := "/blocks?"
	if only {
		baseURL = "/blocks?comtx=1&"
	}
	pg := newPager(pageParam(r), pageSize, total, baseURL)
	list, _ := rt.Store.Blocks(pg.Offset(), pageSize, only)
	s.render(w, "blocks", struct {
		base
		Blocks []BlockView
		Pager  pager
		Only   bool
	}{s.base(r, "Blocos", "blocks"), list, pg, only})
}

func (s *Server) block(w http.ResponseWriter, r *http.Request) {
	rt := s.app.RT()
	id := r.PathValue("id")
	var num uint64
	if h, ok := ParseHash(id); ok {
		n, found := rt.Store.BlockByHash(h)
		if !found {
			s.notFound(w, r, "Bloco não encontrado: "+id, id)
			return
		}
		num = n
	} else {
		n, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			s.notFound(w, r, "Número de bloco inválido: "+id, id)
			return
		}
		num = n
	}
	b, txs, ok := rt.Store.Block(num)
	if !ok {
		s.notFound(w, r, "Bloco "+id+" ainda não indexado ou inexistente.", id)
		return
	}
	st := rt.Store.Stats()
	s.render(w, "block", struct {
		base
		Block   BlockView
		Txs     []*Tx
		HasPrev bool
		HasNext bool
	}{s.base(r, "Bloco "+fmtInt(num), "blocks"), b, txs, num > st.FirstBlock, num < st.LastBlock})
}

func (s *Server) txs(w http.ResponseWriter, r *http.Request) {
	rt := s.app.RT()
	_, total := rt.Store.Txs(0, 0)
	pg := newPager(pageParam(r), pageSize, total, "/txs?")
	list, _ := rt.Store.Txs(pg.Offset(), pageSize)
	s.render(w, "txs", struct {
		base
		Txs   []*Tx
		Pager pager
	}{s.base(r, "Transações", "txs"), list, pg})
}

type logView struct {
	Log
	Decoded    Decoded
	HasDecoded bool
}

func (s *Server) tx(w http.ResponseWriter, r *http.Request) {
	rt := s.app.RT()
	id := r.PathValue("hash")
	h, ok := ParseHash(id)
	if !ok {
		s.notFound(w, r, "Hash de transação inválido: "+id, id)
		return
	}
	t, ok := rt.Store.Tx(h)
	if !ok {
		// pode existir no no e ainda nao ter sido indexada (pendente ou sincronizacao em andamento)
		var raw map[string]interface{}
		msg := "Transação não encontrada: " + id
		if err := rt.RPC.Do("eth_getTransactionByHash", &raw, h.Hex()); err == nil && raw != nil {
			if raw["blockNumber"] == nil {
				msg = "Esta transação está PENDENTE no mempool do nó: ainda não entrou em nenhum bloco. Veja a página Pendentes para saber se ela está pronta ou aguardando um nonce anterior."
			} else {
				msg = "A transação já está em um bloco, mas ainda não foi indexada (sincronização em andamento). Tente novamente em instantes."
			}
		}
		s.notFound(w, r, msg, id)
		return
	}
	var logs []logView
	for _, l := range t.Logs {
		d, ok := s.dec.Event(l)
		logs = append(logs, logView{l, d, ok})
	}
	m, hasMethod := s.dec.Method(t.Input)
	s.render(w, "tx", struct {
		base
		Tx        *Tx
		Logs      []logView
		Method    Decoded
		HasMethod bool
		Transfers []TokenTransfer
	}{s.base(r, "Transação "+shortHex(t.Hash.Hex()), "txs"), t, logs, m, hasMethod, rt.Store.TxTransfers(h)})
}

func (s *Server) address(w http.ResponseWriter, r *http.Request) {
	rt := s.app.RT()
	id := r.PathValue("addr")
	a, ok := ParseAddress(id)
	if !ok {
		s.notFound(w, r, "Endereço inválido: "+id, id)
		return
	}
	var bal HexBig
	var code HexBytes
	calls := []*Call{
		{Method: "eth_getBalance", Params: []interface{}{a.Hex(), "latest"}, Result: &bal},
		{Method: "eth_getCode", Params: []interface{}{a.Hex(), "latest"}, Result: &code},
	}
	_ = rt.RPC.Batch(calls)
	_, total := rt.Store.AddressTxs(a, 0, 0)
	pg := newPager(pageParam(r), pageSize, total, "/address/"+a.Hex()+"?")
	list, _ := rt.Store.AddressTxs(a, pg.Offset(), pageSize)
	tb, tcount := rt.Store.TokenBalance(a)
	var live *big.Int
	ti := rt.Token.Info()
	if ti.Enabled {
		if v, err := rt.Token.BalanceOf(a); err == nil {
			live = v
		}
	}
	trs, _ := rt.Store.Transfers(&a, 0, 10)
	s.render(w, "address", struct {
		base
		Addr       Address
		Balance    *big.Int
		BalanceOK  bool
		IsContract bool
		CodeSize   int
		Txs        []*Tx
		Pager      pager
		TokBal     *big.Int
		TokLive    *big.Int
		TokCount   int
		Transfers  []TokenTransfer
	}{s.base(r, "Endereço "+shortHex(a.Hex()), ""), a, bigOrZero(bal.Int), calls[0].Err == nil, len(code) > 0, len(code),
		list, pg, tb, live, tcount, trs})
}

func (s *Server) brln(w http.ResponseWriter, r *http.Request) {
	rt := s.app.RT()
	ti := rt.Token.Info()
	var filter *Address
	q := strings.TrimSpace(r.URL.Query().Get("addr"))
	if q != "" {
		if a, ok := s.resolveAddr(q); ok {
			filter = &a
		}
	}
	_, total := rt.Store.Transfers(filter, 0, 0)
	baseURL := "/brln?"
	if filter != nil {
		baseURL = "/brln?addr=" + filter.Hex() + "&"
	}
	pg := newPager(pageParam(r), pageSize, total, baseURL)
	list, _ := rt.Store.Transfers(filter, pg.Offset(), pageSize)
	s.render(w, "brln", struct {
		base
		Transfers []TokenTransfer
		Pager     pager
		Holders   []Holder
		Filter    string
		BadFilter bool
		Supply    SupplyCheck
		Stats     Stats
	}{s.base(r, ti.Symbol, "brln"), list, pg, rt.Store.Holders(50), q, q != "" && filter == nil, checkSupply(rt), rt.Store.Stats()})
}

// SupplyCheck compara o totalSupply do contrato com a soma dos eventos (emissoes - queimas).
type SupplyCheck struct {
	State     string   // "ok", "divergente", "conferindo" ou "aguardando"
	Sum       *big.Int // soma dos eventos ate o ultimo bloco indexado
	Block     uint64   // bloco da leitura do totalSupply
	LastBlock uint64   // ultimo bloco indexado
}

// checkSupply so compara quando a leitura do contrato e de um bloco igual ou posterior a ultima
// emissao/queima indexada: transferencias comuns nao mudam o total, entao o resultado vale ate o
// ultimo bloco. Antes disso o estado e "conferindo" (o totalSupply do bloco novo ainda vai ser lido).
func checkSupply(rt *Runtime) SupplyCheck {
	ti := rt.Token.Info()
	es := rt.Store.SupplyFromEvents()
	c := SupplyCheck{Sum: es.Sum, Block: ti.SupplyBlock, LastBlock: es.LastBlock}
	switch {
	case !ti.Enabled || ti.TotalSupply == nil || ti.SupplyBlock == 0 || !rt.Ix.Status().InitialDone:
		c.State = "aguardando"
	case es.HasChange && ti.SupplyBlock < es.LastChange:
		c.State = "conferindo"
	case ti.TotalSupply.Cmp(es.Sum) == 0:
		c.State = "ok"
	default:
		c.State = "divergente"
	}
	return c
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	rt := s.app.RT()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	switch {
	case q == "":
		http.Redirect(w, r, "/", http.StatusSeeOther)
	case len(strings.TrimPrefix(q, "0x")) == 64:
		h, ok := ParseHash(q)
		if !ok {
			break
		}
		if _, ok := rt.Store.Tx(h); ok {
			http.Redirect(w, r, "/tx/"+h.Hex(), http.StatusSeeOther)
			return
		}
		if n, ok := rt.Store.BlockByHash(h); ok {
			http.Redirect(w, r, "/block/"+strconv.FormatUint(n, 10), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/tx/"+h.Hex(), http.StatusSeeOther) // mostra "nao encontrada / nao indexada"
		return
	case len(strings.TrimPrefix(q, "0x")) == 40:
		if a, ok := ParseAddress(q); ok {
			http.Redirect(w, r, "/address/"+a.Hex(), http.StatusSeeOther)
			return
		}
	default:
		if a, ok := s.app.FindByLabel(q); ok {
			http.Redirect(w, r, "/address/"+a.Hex(), http.StatusSeeOther)
			return
		}
		if n, err := strconv.ParseUint(strings.ReplaceAll(q, ".", ""), 10, 64); err == nil {
			http.Redirect(w, r, "/block/"+strconv.FormatUint(n, 10), http.StatusSeeOther)
			return
		}
	}
	s.notFound(w, r, "Nada encontrado para \""+q+"\". Busque por hash de transação (txId), hash ou número de bloco, endereço ou rótulo.", q)
}

func bigStr(v *big.Int) string {
	if v == nil {
		return ""
	}
	return v.String()
}

// resolveAddr aceita endereco 0x... ou rotulo cadastrado.
func (s *Server) resolveAddr(q string) (Address, bool) {
	if a, ok := ParseAddress(q); ok {
		return a, true
	}
	return s.app.FindByLabel(q)
}

func (s *Server) apiStatus(w http.ResponseWriter, r *http.Request) {
	rt := s.app.RT()
	st := rt.Ix.Status()
	ss := rt.Store.Stats()
	ti := rt.Token.Info()
	sc := checkSupply(rt)
	var vals []string
	for _, v := range st.Validators {
		vals = append(vals, v.Hex())
	}
	out := map[string]interface{}{
		"version": s.version, "chainId": st.ChainID, "head": st.Head, "nextBlock": st.Next,
		"syncing": st.Syncing, "initialSyncDone": st.InitialDone, "percent": st.Percent,
		"blocksIndexed": ss.Indexed, "blocksWithTx": ss.NonEmpty, "transactions": ss.TxCount,
		"tokenTransfers": ss.Transfers, "tokenHolders": ss.Holders, "validators": vals,
		"lastError": st.LastErr, "token": map[string]interface{}{"enabled": ti.Enabled, "address": ti.Address.Hex(), "symbol": ti.Symbol, "decimals": ti.Decimals,
			"supplyCheck": sc.State, "totalSupply": bigStr(ti.TotalSupply), "supplyBlock": sc.Block, "supplyFromEvents": sc.Sum.String()},
		"time": time.Now().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
