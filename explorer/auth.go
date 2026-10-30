package main

// Autenticacao com usuarios fixos e sessoes em memoria.
// - admin: todas as paginas, inclusive a configuracao (/explorer-conf/admin)
// - client: todas as paginas, exceto a configuracao
// As senhas padrao ficam aqui so como hash SHA-256; podem ser trocadas por
// --admin-password / --client-password (ou variaveis de ambiente) sem recompilar.
// As sessoes somem ao reiniciar o processo (como o banco em memoria).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	RoleAdmin  = "admin"
	RoleClient = "client"

	sessionCookie = "besu_explorer_session"
	sessionTTL    = 12 * time.Hour

	maxLoginFailures = 10
	failureWindow    = 10 * time.Minute
)

// hashes SHA-256 das senhas padrao
const (
	defaultAdminHash  = "3fe96bd7edac32373f42ad6b7e32cbb7203ce52d3a3602cbf595451fe8d59323"
	defaultClientHash = "f22d463d401708d665cd13c4b921054334bd267ac392b09351c46b33cf3ca0d3"
)

type User struct {
	Name string
	Role string
}

func (u *User) IsAdmin() bool { return u != nil && u.Role == RoleAdmin }

type session struct {
	user    User
	expires time.Time
}

type failure struct {
	count int
	first time.Time
}

type Auth struct {
	users map[string]userEntry // nome -> hash da senha + papel

	mu       sync.Mutex
	sessions map[string]session
	failures map[string]failure
}

type userEntry struct {
	hash [32]byte
	role string
}

func passwordHash(p string) [32]byte { return sha256.Sum256([]byte(p)) }

func mustHex32(s string) [32]byte {
	var out [32]byte
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		panic("hash invalido: " + s)
	}
	copy(out[:], b)
	return out
}

// NewAuth cria os usuarios fixos; senha vazia mantem a padrao.
func NewAuth(adminPassword, clientPassword string) *Auth {
	a := &Auth{users: map[string]userEntry{}, sessions: map[string]session{}, failures: map[string]failure{}}
	ah, ch := mustHex32(defaultAdminHash), mustHex32(defaultClientHash)
	if adminPassword != "" {
		ah = passwordHash(adminPassword)
	}
	if clientPassword != "" {
		ch = passwordHash(clientPassword)
	}
	a.users["admin"] = userEntry{ah, RoleAdmin}
	a.users["client"] = userEntry{ch, RoleClient}
	return a
}

// Check valida usuario e senha em tempo constante.
func (a *Auth) Check(name, password string) (User, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	h := passwordHash(password)
	e, ok := a.users[name]
	if !ok {
		e = userEntry{hash: mustHex32(defaultAdminHash)} // compara mesmo assim para nao revelar usuarios pelo tempo
	}
	match := subtle.ConstantTimeCompare(h[:], e.hash[:]) == 1
	if !ok || !match {
		return User{}, false
	}
	return User{Name: name, Role: e.role}, true
}

func (a *Auth) newSession(u User) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	tok := hex.EncodeToString(b)
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, s := range a.sessions { // limpeza oportunista
		if now.After(s.expires) {
			delete(a.sessions, k)
		}
	}
	a.sessions[tok] = session{u, now.Add(sessionTTL)}
	return tok
}

func (a *Auth) lookup(tok string) (User, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[tok]
	if !ok {
		return User{}, false
	}
	if time.Now().After(s.expires) {
		delete(a.sessions, tok)
		return User{}, false
	}
	return s.user, true
}

func (a *Auth) drop(tok string) {
	a.mu.Lock()
	delete(a.sessions, tok)
	a.mu.Unlock()
}

// blocked indica muitas tentativas erradas recentes deste IP.
func (a *Auth) blocked(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.failures[ip]
	if !ok {
		return false
	}
	if time.Since(f.first) > failureWindow {
		delete(a.failures, ip)
		return false
	}
	return f.count >= maxLoginFailures
}

func (a *Auth) fail(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := a.failures[ip]
	if f.count == 0 || time.Since(f.first) > failureWindow {
		f = failure{first: time.Now()}
	}
	f.count++
	a.failures[ip] = f
}

func (a *Auth) clearFailures(ip string) {
	a.mu.Lock()
	delete(a.failures, ip)
	a.mu.Unlock()
}

// ------------------------------------------------------------------ http

type ctxKey struct{}

func userFrom(r *http.Request) *User {
	u, _ := r.Context().Value(ctxKey{}).(*User)
	return u
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// publicPath: paginas acessiveis sem login.
func publicPath(p string) bool {
	return p == "/login" || strings.HasPrefix(p, "/static/") || p == "/branding/logo"
}

// requireLogin exige sessao (cookie) ou autenticacao basica (para curl e monitoramento).
func (s *Server) requireLogin(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPath(r.URL.Path) {
			h.ServeHTTP(w, r)
			return
		}
		var u User
		ok := false
		if c, err := r.Cookie(sessionCookie); err == nil {
			u, ok = s.auth.lookup(c.Value)
		}
		if !ok {
			if name, pass, has := r.BasicAuth(); has {
				u, ok = s.auth.Check(name, pass)
			}
		}
		if !ok {
			// atualizacao automatica e API: 401 (o live.js recarrega e cai no login)
			if r.Header.Get("X-Live-Refresh") != "" || strings.HasPrefix(r.URL.Path, "/api/") || r.Method != http.MethodGet {
				w.Header().Set("WWW-Authenticate", `Basic realm="besu-explorer"`)
				http.Error(w, "autenticação necessária", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, &u)))
	})
}

// requireAdmin libera a pagina so para o papel admin.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if userFrom(r).IsAdmin() {
		return true
	}
	w.WriteHeader(http.StatusForbidden)
	s.render(w, "notfound", struct {
		base
		Msg string
	}{s.base(r, "Acesso restrito", ""), "Esta página é restrita ao administrador. Para acessá-la, saia e entre com o usuário admin."})
	return false
}

// safeNext aceita so caminhos locais (evita redirecionar para outro site).
func safeNext(n string) string {
	if n == "" || !strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") || strings.HasPrefix(n, "/\\") || n == "/login" {
		return "/"
	}
	return n
}

type loginView struct {
	base
	Name string
	Next string
	Err  string
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if _, ok := s.auth.lookup(c.Value); ok {
			http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
			return
		}
	}
	s.render(w, "login", loginView{base: s.base(r, "Entrar", "login"), Next: safeNext(r.URL.Query().Get("next"))})
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "origem inválida", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	_ = r.ParseForm()
	v := loginView{base: s.base(r, "Entrar", "login"), Name: strings.TrimSpace(r.PostFormValue("user")), Next: safeNext(r.PostFormValue("next"))}
	ip := clientIP(r)
	if s.auth.blocked(ip) {
		w.WriteHeader(http.StatusTooManyRequests)
		v.Err = "Muitas tentativas erradas. Aguarde alguns minutos e tente novamente."
		s.render(w, "login", v)
		return
	}
	u, ok := s.auth.Check(v.Name, r.PostFormValue("password"))
	if !ok {
		s.auth.fail(ip)
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusUnauthorized)
		v.Err = "Usuário ou senha incorretos. Confira os dados e tente novamente."
		s.render(w, "login", v)
		return
	}
	s.auth.clearFailures(ip)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: s.auth.newSession(u), Path: "/", HttpOnly: true,
		Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds())})
	http.Redirect(w, r, v.Next, http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "origem inválida", http.StatusForbidden)
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.auth.drop(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: isHTTPS(r),
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
