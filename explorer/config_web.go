package main

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

type configView struct {
	base
	Current    Config
	Form       Config
	Defaults   Config
	Path       string
	Saved      bool
	Notice     string
	Err        string
	StartedAgo string
	Overrides  []string
	Labels     []LabelEntry
	LabelForm  LabelEntryForm
	LogoErr    string
	LogoSlots  []logoSlot
	LabelErr   string
}

type LabelEntryForm struct{ Address, Label string }

// logoSlot descreve uma variante do logo na tela de configuracao.
type logoSlot struct {
	Variant, Title, Hint, URL string
	Uploaded, Dark            bool
}

var notices = map[string]string{
	"1":         "Configuração salva. O explorer recomeçou a indexação do zero com os novos valores.",
	"label":     "Rótulo salvo. Já aparece em todas as páginas.",
	"label-del": "Rótulo removido.",
	"logo":      "Logo atualizado. Ele aparece no cabeçalho e na tela de carregamento.",
	"logo-del":  "Logo removido. Volta a valer o logo padrão.",
}

// sameOrigin bloqueia envio do formulario a partir de outro site (CSRF).
func sameOrigin(r *http.Request) bool {
	for _, h := range []string{r.Header.Get("Origin"), r.Header.Get("Referer")} {
		if h == "" || h == "null" {
			continue
		}
		u, err := url.Parse(h)
		if err != nil || u.Host != r.Host {
			return false
		}
		return true
	}
	return true // sem Origin/Referer (ex.: curl): permitido
}

func (s *Server) configView(r *http.Request, form Config) configView {
	rt := s.app.RT()
	return configView{
		base:       s.base(r, "Configuração", "config"),
		Current:    rt.Cfg,
		Form:       form,
		Defaults:   DefaultConfig(),
		Path:       s.app.ConfigPath(),
		StartedAgo: ago(uint64(rt.Started.Unix())),
		Overrides:  s.app.CLIOverrides,
		Labels:     s.app.Labels(),
		LogoSlots: []logoSlot{
			{Variant: "", Title: "Logo principal", Hint: "Usado no tema claro, e também no escuro se não houver logo próprio para ele.",
				URL: s.app.LogoURL(false), Uploaded: s.app.LogoFile(false) != ""},
			{Variant: "dark", Title: "Logo para o tema escuro (opcional)", Hint: "Versão clara do logo, para aparecer sobre o fundo preto.",
				URL: s.app.LogoURL(true), Uploaded: s.app.LogoFile(true) != "", Dark: true},
		},
	}
}

func (s *Server) configPage(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	v := s.configView(r, s.app.RT().Cfg)
	if n, ok := notices[r.URL.Query().Get("ok")]; ok {
		v.Saved, v.Notice = true, n
	}
	if a := r.URL.Query().Get("edit"); a != "" {
		if ad, ok := ParseAddress(a); ok {
			v.LabelForm = LabelEntryForm{ad.Hex(), s.app.Label(ad)}
		}
	}
	s.render(w, "config", v)
}

func (s *Server) configSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "origem inválida", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxLogoSize+64<<10)
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		err = r.ParseMultipartForm(maxLogoSize + 64<<10)
	} else {
		err = r.ParseForm()
	}
	if err != nil {
		v := s.configView(r, s.app.RT().Cfg)
		v.LogoErr = "Envio inválido ou arquivo grande demais (máximo 512 KB)."
		s.render(w, "config", v)
		return
	}
	done := func(code string) { http.Redirect(w, r, AdminPath+"?ok="+code, http.StatusSeeOther) }

	switch r.PostFormValue("action") {
	case "label-save":
		f := LabelEntryForm{strings.TrimSpace(r.PostFormValue("address")), r.PostFormValue("label")}
		ad, ok := ParseAddress(f.Address)
		if !ok {
			v := s.configView(r, s.app.RT().Cfg)
			v.LabelForm, v.LabelErr = f, "Endereço inválido: deve ter 0x + 40 caracteres hexadecimais."
			s.render(w, "config", v)
			return
		}
		if err := s.app.SetLabel(ad, f.Label); err != nil {
			v := s.configView(r, s.app.RT().Cfg)
			v.LabelForm, v.LabelErr = f, err.Error()
			s.render(w, "config", v)
			return
		}
		done("label")
		return
	case "label-delete":
		if ad, ok := ParseAddress(r.PostFormValue("address")); ok {
			if err := s.app.DeleteLabel(ad); err != nil {
				log.Printf("rotulos: %v", err)
			}
		}
		done("label-del")
		return
	case "logo-upload":
		file, _, ferr := r.FormFile("logo")
		if ferr != nil {
			v := s.configView(r, s.app.RT().Cfg)
			v.LogoErr = "Selecione um arquivo de imagem."
			s.render(w, "config", v)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(io.LimitReader(file, maxLogoSize+1))
		if err := s.app.SetLogo(data, r.PostFormValue("variant") == "dark"); err != nil {
			v := s.configView(r, s.app.RT().Cfg)
			v.LogoErr = err.Error()
			s.render(w, "config", v)
			return
		}
		done("logo")
		return
	case "logo-remove":
		if err := s.app.RemoveLogo(r.PostFormValue("variant") == "dark"); err != nil {
			log.Printf("logo: %v", err)
		}
		done("logo-del")
		return
	}

	// conexao: salvar ou restaurar padroes (reindexa)
	var cfg Config
	if r.PostFormValue("action") == "defaults" {
		cfg = DefaultConfig()
	} else {
		cfg = Config{RPC: r.PostFormValue("rpc"), BRLN: r.PostFormValue("brln")}
	}
	cfg, err = NormalizeConfig(cfg)
	if err != nil {
		v := s.configView(r, cfg)
		v.Err = err.Error()
		s.render(w, "config", v)
		return
	}
	if _, err := s.app.CheckConfig(cfg); err != nil {
		v := s.configView(r, cfg)
		v.Err = err.Error() + " — a configuração não foi alterada."
		s.render(w, "config", v)
		return
	}
	s.app.Start(cfg)
	if err := s.app.Save(); err != nil {
		log.Printf("config: nao foi possivel gravar %s: %v (aplicada so em memoria)", s.app.ConfigPath(), err)
	}
	done("1")
}
