package main

// Aviso de bloco novo para os navegadores (Server-Sent Events em /api/events).
// O indexador chama Hub.Publish a cada lote gravado; cada conexao recebe no maximo
// um aviso pendente (os avisos se acumulam num so), e o navegador busca so o que mudou.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const sseHeartbeat = 15 * time.Second

type Hub struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[chan struct{}]struct{}{}} }

func (h *Hub) subscribe() chan struct{} {
	c := make(chan struct{}, 1)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	h.mu.Unlock()
	return c
}

func (h *Hub) unsubscribe(c chan struct{}) {
	h.mu.Lock()
	delete(h.subs, c)
	h.mu.Unlock()
}

// Publish avisa todos os inscritos sem bloquear: quem ainda tem um aviso pendente nao recebe outro.
func (h *Hub) Publish() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.subs {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}

// liveEvent e o conteudo de cada aviso.
type liveEvent struct {
	Indexed int64  `json:"indexed"` // ultimo bloco indexado (-1 = nenhum)
	Head    uint64 `json:"head"`
	Synced  bool   `json:"synced"`
	Now     int64  `json:"now"` // relogio do servidor (unix), para os "ha N s" no navegador
}

func (s *Server) liveEvent() liveEvent {
	rt := s.app.RT()
	st := rt.Ix.Status()
	ev := liveEvent{Indexed: int64(st.Next) - 1, Head: st.Head, Synced: st.InitialDone, Now: time.Now().Unix()}
	if ss := rt.Store.Stats(); !ss.HasBlocks {
		ev.Indexed = -1
	}
	return ev
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // conexao longa: sem o WriteTimeout do servidor
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // proxies (nginx) nao seguram os avisos

	sub := s.app.hub.subscribe()
	defer s.app.hub.unsubscribe(sub)
	var session string
	if c, err := r.Cookie(sessionCookie); err == nil {
		session = c.Value
	}

	send := func() bool {
		b, _ := json.Marshal(s.liveEvent())
		if _, err := fmt.Fprintf(w, "event: block\ndata: %s\n\n", b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil || !send() {
		return
	}
	beat := time.NewTicker(sseHeartbeat)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.app.parent.Done():
			return
		case <-sub:
			if !send() {
				return
			}
		case <-beat.C:
			// sessao encerrada (Sair) ou expirada: fecha o aviso; o navegador volta para o login
			if session != "" {
				if _, ok := s.auth.lookup(session); !ok {
					return
				}
			}
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}
