package main

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// /api/events manda o estado ao conectar e um aviso a cada Publish (bloco gravado).
func TestEventsStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := NewApp(ctx, Options{Batch: 10, Workers: 1, Poll: time.Hour, Timeout: 50 * time.Millisecond}, t.TempDir()+"/cfg.json")
	app.Start(Config{RPC: "http://127.0.0.1:1"})
	srv := httptest.NewServer(NewServer(app, NewDecoder(), "test", NewAuth("", ""), 5).Routes())
	defer srv.Close()

	// sem login: 401
	if r, err := http.Get(srv.URL + "/api/events"); err != nil || r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sem login: %v %v", r, err)
	}

	req, _ := http.NewRequest("GET", srv.URL+"/api/events", nil)
	req.SetBasicAuth("client", "vErt1Ce@2026")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type %q", ct)
	}
	events := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				events <- sc.Text()
			}
		}
		close(events)
	}()
	wait := func(what string) string {
		select {
		case e := <-events:
			return e
		case <-time.After(3 * time.Second):
			t.Fatalf("sem evento: %s", what)
		}
		return ""
	}
	if e := wait("inicial"); !strings.Contains(e, `"indexed":-1`) {
		t.Fatalf("evento inicial: %s", e)
	}
	time.Sleep(50 * time.Millisecond) // garante a inscricao antes do aviso
	app.hub.Publish()
	if e := wait("apos Publish"); !strings.Contains(e, `"now":`) {
		t.Fatalf("evento: %s", e)
	}
}

// Publish nao bloqueia com inscrito lento e acumula avisos num so.
func TestHubCoalesces(t *testing.T) {
	h := NewHub()
	c := h.subscribe()
	for i := 0; i < 100; i++ {
		h.Publish()
	}
	if len(c) != 1 {
		t.Fatalf("avisos pendentes: %d", len(c))
	}
	h.unsubscribe(c)
	h.Publish()
}
