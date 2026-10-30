package main

// Rotulos de enderecos e logo da tela de carregamento.
// Ficam no mesmo arquivo de configuracao, mas mudam sem reindexar a rede.

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxLabelLen = 40
	maxLabels   = 2000
	maxLogoSize = 512 << 10 // 512 KB
	logoBase    = "explorer-logo"
)

var logoTypes = map[string]string{ // extensao -> content-type
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

// ------------------------------------------------------------- rotulos

// SetExtras carrega rotulos e logo lidos do arquivo (na inicializacao).
func (a *App) SetExtras(labels map[string]string, logo, logoDark string) {
	m := map[Address]string{}
	for k, v := range labels {
		if ad, ok := ParseAddress(k); ok {
			if l, err := normalizeLabel(v); err == nil {
				m[ad] = l
			}
		}
	}
	a.mu.Lock()
	a.labels, a.logo, a.logoDark = m, "", ""
	if logo != "" {
		a.logo = filepath.Base(logo)
	}
	if logoDark != "" {
		a.logoDark = filepath.Base(logoDark)
	}
	a.mu.Unlock()
}

func (a *App) Label(ad Address) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.labels[ad]
}

type LabelEntry struct {
	Address Address
	Label   string
}

func (a *App) Labels() []LabelEntry {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]LabelEntry, 0, len(a.labels))
	for ad, l := range a.labels {
		out = append(out, LabelEntry{ad, l})
	}
	sort.Slice(out, func(i, j int) bool {
		if li, lj := strings.ToLower(out[i].Label), strings.ToLower(out[j].Label); li != lj {
			return li < lj
		}
		return out[i].Address.Hex() < out[j].Address.Hex()
	})
	return out
}

// FindByLabel procura um endereco pelo rotulo (exato, sem diferenciar maiusculas).
func (a *App) FindByLabel(q string) (Address, bool) {
	q = strings.ToLower(strings.TrimSpace(q))
	a.mu.RLock()
	defer a.mu.RUnlock()
	for ad, l := range a.labels {
		if strings.ToLower(l) == q {
			return ad, true
		}
	}
	return Address{}, false
}

func normalizeLabel(s string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "", errors.New("informe o rótulo")
	}
	if utf8.RuneCountInString(s) > maxLabelLen {
		return "", fmt.Errorf("rótulo com mais de %d caracteres", maxLabelLen)
	}
	return s, nil
}

func (a *App) SetLabel(ad Address, label string) error {
	l, err := normalizeLabel(label)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if _, exists := a.labels[ad]; !exists && len(a.labels) >= maxLabels {
		a.mu.Unlock()
		return fmt.Errorf("limite de %d rótulos atingido", maxLabels)
	}
	a.labels[ad] = l
	a.mu.Unlock()
	return a.Save()
}

func (a *App) DeleteLabel(ad Address) error {
	a.mu.Lock()
	delete(a.labels, ad)
	a.mu.Unlock()
	return a.Save()
}

// Save grava RPC/BRLN em uso + rotulos + logo.
func (a *App) Save() error {
	a.mu.RLock()
	c := a.rt.Cfg
	c.Labels = map[string]string{}
	for ad, l := range a.labels {
		c.Labels[ad.Hex()] = l
	}
	c.Logo, c.LogoDark = a.logo, a.logoDark
	a.mu.RUnlock()
	return SaveConfig(a.cfgPath, c)
}

// --------------------------------------------------------------- logo
// Ha duas variantes: a normal (tema claro, e tambem o escuro se nao houver outra)
// e a opcional do tema escuro. Sem logo enviado, vale o logo padrao embutido em
// web/static/brand/ (logo.* e logo-dark.*), se existir.

func logoName(dark bool) string {
	if dark {
		return logoBase + "-dark"
	}
	return logoBase
}

func (a *App) LogoFile(dark bool) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	name := a.logo
	if dark {
		name = a.logoDark
	}
	if name == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(a.cfgPath), name)
}

// builtinLogo procura o logo padrao embutido no executavel.
func builtinLogo(dark bool) string {
	for ext := range logoTypes {
		p := "web/static/brand/" + strings.TrimPrefix(logoName(dark), "explorer-") + ext
		if _, err := fs.Stat(webFS, p); err == nil {
			return "/static/" + strings.TrimPrefix(p, "web/static/")
		}
	}
	return ""
}

// LogoURL devolve a URL do logo com versao (data do arquivo) para nao usar copia antiga do cache.
func (a *App) LogoURL(dark bool) string {
	f := a.LogoFile(dark)
	if f == "" {
		return builtinLogo(dark)
	}
	st, err := os.Stat(f)
	if err != nil {
		return builtinLogo(dark)
	}
	u := fmt.Sprintf("/branding/logo?v=%d", st.ModTime().UnixNano())
	if dark {
		u += "&dark=1"
	}
	return u
}

// detectLogo valida o arquivo enviado e devolve a extensao.
func detectLogo(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("arquivo vazio")
	}
	if len(data) > maxLogoSize {
		return "", fmt.Errorf("arquivo maior que %d KB", maxLogoSize>>10)
	}
	switch http.DetectContentType(data) {
	case "image/png":
		return ".png", nil
	case "image/jpeg":
		return ".jpg", nil
	case "image/webp":
		return ".webp", nil
	}
	head := bytes.ToLower(data[:min(len(data), 1024)])
	if bytes.Contains(head, []byte("<svg")) {
		return ".svg", nil
	}
	return "", errors.New("formato não suportado: envie PNG, JPG, WebP ou SVG")
}

func (a *App) SetLogo(data []byte, dark bool) error {
	ext, err := detectLogo(data)
	if err != nil {
		return err
	}
	dir := filepath.Dir(a.cfgPath)
	name := logoName(dark) + ext
	tmp := filepath.Join(dir, name+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("não foi possível gravar o logo: %v", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("não foi possível gravar o logo: %v", err)
	}
	a.mu.Lock()
	slot := &a.logo
	if dark {
		slot = &a.logoDark
	}
	old := *slot
	*slot = name
	a.mu.Unlock()
	if old != "" && old != name {
		_ = os.Remove(filepath.Join(dir, old))
	}
	return a.Save()
}

func (a *App) RemoveLogo(dark bool) error {
	a.mu.Lock()
	slot := &a.logo
	if dark {
		slot = &a.logoDark
	}
	old := *slot
	*slot = ""
	a.mu.Unlock()
	if old != "" {
		_ = os.Remove(filepath.Join(filepath.Dir(a.cfgPath), old))
	}
	return a.Save()
}

// serveLogo entrega o logo; SVG vai com CSP restritiva (nada de script se aberto direto).
func (s *Server) serveLogo(w http.ResponseWriter, r *http.Request) {
	f := s.app.LogoFile(r.URL.Query().Get("dark") == "1")
	if f == "" {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(f)
	if err != nil {
		log.Printf("logo: %v", err)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", logoTypes[filepath.Ext(f)])
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}
