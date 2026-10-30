package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectLogo(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32))
	if ext, err := detectLogo(png); err != nil || ext != ".png" {
		t.Fatalf("png: %v %v", ext, err)
	}
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	if ext, err := detectLogo(svg); err != nil || ext != ".svg" {
		t.Fatalf("svg: %v %v", ext, err)
	}
	if _, err := detectLogo([]byte("<html><script>alert(1)</script></html>")); err == nil {
		t.Fatal("html deveria ser rejeitado")
	}
	if _, err := detectLogo(make([]byte, maxLogoSize+1)); err == nil {
		t.Fatal("arquivo grande deveria ser rejeitado")
	}
}

func TestNormalizeLabel(t *testing.T) {
	if l, err := normalizeLabel("  Núclea   operador \n"); err != nil || l != "Núclea operador" {
		t.Fatalf("%q %v", l, err)
	}
	if _, err := normalizeLabel("   "); err == nil {
		t.Fatal("vazio deveria falhar")
	}
	if _, err := normalizeLabel(strings.Repeat("á", maxLabelLen+1)); err == nil {
		t.Fatal("longo deveria falhar")
	}
}

// Salvar rotulos/logo nao pode perder RPC/BRLN e salvar a conexao nao pode perder rotulos/logo.
func TestConfigRoundTripKeepsExtras(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := NewApp(ctx, Options{Batch: 10, Workers: 1, Poll: 1e9, Timeout: 1e9}, path)
	app.SetExtras(nil, "", "")
	app.Start(Config{RPC: "http://127.0.0.1:1", BRLN: ""})

	a, _ := ParseAddress("0x00000000000000000000000000000000000000aa")
	if err := app.SetLabel(a, "Operador"); err != nil {
		t.Fatal(err)
	}
	if err := app.SetLogo([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), false); err != nil {
		t.Fatal(err)
	}
	app.Start(Config{RPC: "http://127.0.0.1:2", BRLN: ""}) // como no "Salvar e reindexar"
	if err := app.Save(); err != nil {
		t.Fatal(err)
	}

	c, ok, err := LoadConfig(path)
	if err != nil || !ok {
		t.Fatalf("load: %v %v", ok, err)
	}
	if c.RPC != "http://127.0.0.1:2" || c.Labels[a.Hex()] != "Operador" || c.Logo != "explorer-logo.svg" {
		t.Fatalf("config gravada errada: %+v", c)
	}
	if _, err := os.Stat(filepath.Join(dir, "explorer-logo.svg")); err != nil {
		t.Fatal("arquivo do logo ausente")
	}
	app2 := NewApp(ctx, Options{}, path)
	app2.SetExtras(c.Labels, c.Logo, c.LogoDark)
	if app2.Label(a) != "Operador" || app2.LogoFile(false) == "" {
		t.Fatal("extras nao recarregados")
	}
	if got, ok := app2.FindByLabel("operador"); !ok || got != a {
		t.Fatal("busca por rotulo falhou")
	}
	if err := app.RemoveLogo(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "explorer-logo.svg")); !os.IsNotExist(err) {
		t.Fatal("logo removido deveria apagar o arquivo")
	}
}

// O logo do tema escuro e independente do principal e sobrevive a recarga da configuracao.
func TestDarkLogoVariant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := NewApp(ctx, Options{Batch: 10, Workers: 1, Poll: 1e9, Timeout: 1e9}, path)
	app.Start(Config{RPC: "http://127.0.0.1:1"})
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32))
	if err := app.SetLogo([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), false); err != nil {
		t.Fatal(err)
	}
	if err := app.SetLogo(png, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(app.LogoURL(true), "dark=1") || strings.Contains(app.LogoURL(false), "dark=1") {
		t.Fatalf("urls: %q %q", app.LogoURL(false), app.LogoURL(true))
	}
	c, _, _ := LoadConfig(path)
	if c.Logo != "explorer-logo.svg" || c.LogoDark != "explorer-logo-dark.png" {
		t.Fatalf("config: %+v", c)
	}
	if err := app.RemoveLogo(true); err != nil {
		t.Fatal(err)
	}
	if app.LogoFile(true) != "" || app.LogoFile(false) == "" {
		t.Fatal("remover o logo escuro afetou o principal")
	}
}
