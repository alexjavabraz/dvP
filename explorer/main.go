package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "time/tzdata" // fusos horarios embutidos (nao depende do sistema)
)

var version = "1.3.0"

func main() {
	cfgPath := flag.String("config", envOr("EXPLORER_CONFIG", "besu-explorer.json"), "arquivo de configuracao (RPC e BRLN), editavel pela tela /explorer-conf/admin")
	rpcURL := flag.String("rpc", envOr("EXPLORER_RPC", ""), "URL JSON-RPC do no Besu (sobrepoe o arquivo; padrao "+DefaultRPC+")")
	listen := flag.String("listen", envOr("EXPLORER_LISTEN", ":8080"), "endereco:porta da interface web")
	brln := flag.String("brln", envOr("EXPLORER_BRLN", ""), "endereco do contrato ERC-20 BRLN (sobrepoe o arquivo; habilita a pagina BRLN)")
	adminPwd := flag.String("admin-password", envOr("EXPLORER_ADMIN_PASSWORD", ""), "senha do usuario admin (vazio = senha padrao)")
	clientPwd := flag.String("client-password", envOr("EXPLORER_CLIENT_PASSWORD", ""), "senha do usuario client (vazio = senha padrao)")
	live := flag.Int("refresh", envInt("EXPLORER_REFRESH", 5), "intervalo da atualizacao automatica das paginas em segundos (0 desliga)")
	brlnDec := flag.Int("brln-decimals", 18, "casas decimais do BRLN se decimals() nao responder")
	start := flag.Uint64("start-block", 0, "primeiro bloco a indexar")
	batch := flag.Int("batch", 100, "blocos por requisicao em lote (batch JSON-RPC)")
	workers := flag.Int("workers", 4, "requisicoes em lote em paralelo na sincronizacao inicial")
	poll := flag.Duration("poll", time.Second, "intervalo de consulta de novos blocos")
	timeout := flag.Duration("timeout", 60*time.Second, "timeout de cada requisicao ao no")
	abiDir := flag.String("abi-dir", envOr("EXPLORER_ABI_DIR", ""), "diretorio com ABIs .json extras para decodificar funcoes/eventos")
	tz := flag.String("tz", "America/Sao_Paulo", "fuso horario para exibir datas")
	showVer := flag.Bool("version", false, "mostra a versao e sai")
	flag.Parse()

	if *showVer {
		fmt.Println("besu-explorer", version)
		return
	}
	if loc, err := time.LoadLocation(*tz); err == nil {
		displayTZ = loc
	} else {
		log.Printf("fuso %q invalido, usando o do sistema: %v", *tz, err)
	}
	if *batch < 1 || *batch > 1000 {
		log.Fatal("--batch deve estar entre 1 e 1000")
	}
	if *workers < 1 {
		*workers = 1
	}

	cfg, fromFile, err := LoadConfig(*cfgPath)
	if err != nil {
		log.Printf("config: %v (usando padroes)", err)
	}
	var overrides []string
	if *rpcURL != "" {
		cfg.RPC = *rpcURL
		overrides = append(overrides, "--rpc / EXPLORER_RPC")
	}
	if *brln != "" {
		cfg.BRLN = *brln
		overrides = append(overrides, "--brln / EXPLORER_BRLN")
	}
	cfg, err = NormalizeConfig(cfg)
	if err != nil {
		log.Fatalf("configuracao invalida: %v", err)
	}
	if !fromFile {
		if err := SaveConfig(*cfgPath, cfg); err != nil {
			log.Printf("config: nao foi possivel criar %s: %v", *cfgPath, err)
		}
	}

	dec := NewDecoder()
	if *abiDir != "" {
		if err := dec.LoadDir(*abiDir); err != nil {
			log.Fatalf("--abi-dir: %v", err)
		}
		log.Printf("ABIs carregadas: %v", dec.files)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("besu-explorer %s | web: %s | config: %s", version, *listen, *cfgPath)
	app := NewApp(ctx, Options{StartBlock: *start, Batch: *batch, Workers: *workers, Poll: *poll,
		Timeout: *timeout, BRLNDecimals: *brlnDec}, *cfgPath)
	app.CLIOverrides = overrides
	app.SetExtras(cfg.Labels, cfg.Logo, cfg.LogoDark)
	app.Start(cfg)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           NewServer(app, dec, version, NewAuth(*adminPwd, *clientPwd), *live).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("servidor web: %v", err)
	}
	log.Print("encerrado")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		var n int
		if _, err := fmt.Sscan(v, &n); err == nil {
			return n
		}
	}
	return def
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
