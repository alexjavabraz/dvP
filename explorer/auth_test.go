package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAuthDefaultUsers(t *testing.T) {
	a := NewAuth("", "")
	if u, ok := a.Check("admin", "Deltasp+=ks3"); !ok || u.Role != RoleAdmin {
		t.Fatalf("admin padrao: %v %v", u, ok)
	}
	if u, ok := a.Check("Client ", "vErt1Ce@2026"); !ok || u.Role != RoleClient {
		t.Fatalf("client padrao: %v %v", u, ok)
	}
	for _, c := range [][2]string{{"admin", "vErt1Ce@2026"}, {"client", "Deltasp+=ks3"}, {"root", "Deltasp+=ks3"}, {"admin", ""}} {
		if _, ok := a.Check(c[0], c[1]); ok {
			t.Fatalf("aceitou %q/%q", c[0], c[1])
		}
	}
}

func TestAuthOverride(t *testing.T) {
	a := NewAuth("nova-admin", "nova-client")
	if _, ok := a.Check("admin", "Deltasp+=ks3"); ok {
		t.Fatal("senha padrao do admin continuou valendo")
	}
	if _, ok := a.Check("admin", "nova-admin"); !ok {
		t.Fatal("senha nova do admin recusada")
	}
	if _, ok := a.Check("client", "nova-client"); !ok {
		t.Fatal("senha nova do client recusada")
	}
}

func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"":                     "/",
		"/blocks?page=2":       "/blocks?page=2",
		"//evil.example":       "/",
		"/\\evil.example":      "/",
		"https://evil.example": "/",
		"/login":               "/",
	}
	for in, want := range cases {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestLoginBlocking(t *testing.T) {
	a := NewAuth("", "")
	for i := 0; i < maxLoginFailures; i++ {
		a.fail("10.0.0.1")
	}
	if !a.blocked("10.0.0.1") || a.blocked("10.0.0.2") {
		t.Fatal("bloqueio por IP incorreto")
	}
	a.clearFailures("10.0.0.1")
	if a.blocked("10.0.0.1") {
		t.Fatal("bloqueio nao foi limpo")
	}
}

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	app := NewApp(ctx, Options{Batch: 10, Workers: 1, Poll: time.Hour, Timeout: 50 * time.Millisecond, BRLNDecimals: 6}, t.TempDir()+"/cfg.json")
	app.Start(Config{RPC: "http://127.0.0.1:1"}) // no inexistente: as paginas funcionam com o banco vazio
	return NewServer(app, NewDecoder(), "test", NewAuth("", ""), 5).Routes()
}

func login(t *testing.T, h http.Handler, user, pass string) *http.Cookie {
	t.Helper()
	form := url.Values{"user": {user}, "password": {pass}, "next": {"/blocks"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/blocks" {
		t.Fatalf("login %s: status %d location %q", user, rec.Code, rec.Header().Get("Location"))
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.HttpOnly {
			return c
		}
	}
	t.Fatal("login sem cookie de sessao")
	return nil
}

func get(h http.Handler, path string, mod func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if mod != nil {
		mod(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoutesRequireLogin(t *testing.T) {
	h := newTestServer(t)

	if rec := get(h, "/blocks?page=2", nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login?next=%2Fblocks%3Fpage%3D2" {
		t.Fatalf("sem login: status %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := get(h, "/", func(r *http.Request) { r.Header.Set("X-Live-Refresh", "1") }); rec.Code != http.StatusUnauthorized {
		t.Fatalf("atualizacao automatica sem login: status %d", rec.Code)
	}
	if rec := get(h, "/api/status", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("api sem login: status %d", rec.Code)
	}
	for _, p := range []string{"/login", "/static/style.css", "/static/theme.js", "/static/fonts/dm-sans.woff2"} {
		if rec := get(h, p, nil); rec.Code != http.StatusOK {
			t.Fatalf("%s publico: status %d", p, rec.Code)
		}
	}
	if rec := get(h, "/api/status", func(r *http.Request) { r.SetBasicAuth("client", "vErt1Ce@2026") }); rec.Code != http.StatusOK {
		t.Fatalf("api com basic auth: status %d", rec.Code)
	}
	if rec := get(h, "/api/status", func(r *http.Request) { r.SetBasicAuth("client", "errada") }); rec.Code != http.StatusUnauthorized {
		t.Fatalf("api com senha errada: status %d", rec.Code)
	}
}

func TestRoles(t *testing.T) {
	h := newTestServer(t)
	client := login(t, h, "client", "vErt1Ce@2026")
	admin := login(t, h, "admin", "Deltasp+=ks3")

	rec := get(h, "/blocks", func(r *http.Request) { r.AddCookie(client) })
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), AdminPath) {
		t.Fatalf("client em /blocks: status %d, link de config visivel: %v", rec.Code, strings.Contains(rec.Body.String(), AdminPath))
	}
	if rec := get(h, AdminPath, func(r *http.Request) { r.AddCookie(client) }); rec.Code != http.StatusForbidden {
		t.Fatalf("client na configuracao: status %d", rec.Code)
	}
	rec = get(h, AdminPath, func(r *http.Request) { r.AddCookie(admin) })
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Conexão") {
		t.Fatalf("admin na configuracao: status %d", rec.Code)
	}

	// logout invalida a sessao
	req := httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(client)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if rec := get(h, "/blocks", func(r *http.Request) { r.AddCookie(client) }); rec.Code != http.StatusSeeOther {
		t.Fatalf("sessao continuou valida apos sair: status %d", rec.Code)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	h := newTestServer(t)
	form := url.Values{"user": {"admin"}, "password": {"x"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "incorretos") || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("senha errada: status %d", rec.Code)
	}
}
