// Comando da API da Travus Plataforma.
//
//	api serve                  sobe a API: pública em API_ADDR (:8080), interna em API_ADDR_INTERNO (:8081)
//	api migrate up|down|status aplica, desfaz (a última) ou lista as migrações
//	api usuario ...            cria, lista e desativa usuários (api usuario para ajuda)
//	api google ...             token do Google Drive cifrado no banco (api google para ajuda)
//	api healthcheck            consulta o /health local (healthcheck do Docker)
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	// Fusos horários embutidos: a imagem distroless não tem /usr/share/zoneinfo (TZ no compose).
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/joaofelipe93/travus-plataforma/api/internal/checkin"
	"github.com/joaofelipe93/travus-plataforma/api/internal/cripto"
	"github.com/joaofelipe93/travus-plataforma/api/internal/httpapi"
	"github.com/joaofelipe93/travus-plataforma/api/migrations"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "migrate":
		err = migrate(os.Args[2:])
	case "usuario":
		if err = usuario(os.Args[2:]); err != nil {
			// Comando de terminal: mensagem simples em vez de log JSON.
			fmt.Fprintln(os.Stderr, "erro:", err)
			os.Exit(1)
		}
	case "google":
		if err = google(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "erro:", err)
			os.Exit(1)
		}
	case "alerta":
		if err = alertaCLI(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "erro:", err)
			os.Exit(1)
		}
	case "healthcheck":
		err = healthcheck()
	default:
		err = fmt.Errorf("comando desconhecido %q (use serve, migrate, usuario, google, alerta ou healthcheck)", cmd)
	}
	if err != nil {
		slog.Error("encerrando com erro", "comando", cmd, "erro", err)
		os.Exit(1)
	}
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbURL, err := requiredEnv("DATABASE_URL")
	if err != nil {
		return err
	}
	tokenWorker := os.Getenv("WORKER_TOKEN")
	if len(tokenWorker) < 32 {
		return errors.New("WORKER_TOKEN ausente ou curto (mínimo 32 caracteres; o make up gera um em deploy/.env)")
	}

	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return fmt.Errorf("configuração do banco inválida: %w", err)
	}
	poolCfg.MaxConns = 10
	// pgxpool conecta sob demanda: a API sobe mesmo com o banco fora, e o /health mostra isso.
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("configuração do banco inválida: %w", err)
	}
	defer pool.Close()

	cofre, err := cripto.NovoCofre(os.Getenv("CHAVE_CRIPTOGRAFIA"))
	if err != nil {
		return err
	}
	cfg := httpapi.Config{
		AppOrigin:           strings.TrimRight(envOr("APP_ORIGIN", "http://app.localhost"), "/"),
		CookieSecure:        envOr("COOKIE_SECURE", "true") != "false",
		SessaoInatividade:   12 * time.Hour,
		SessaoMaxima:        7 * 24 * time.Hour,
		TokenWorker:         tokenWorker,
		RetencaoScreenshots: 30 * 24 * time.Hour,
		// Só "true" liga. Desligado, a API recusa aprovar e entregar execuções reais.
		LanceRealHabilitado: os.Getenv("LANCE_REAL_HABILITADO") == "true",
		ValidadeDryRun:      2 * time.Hour,
		Cofre:               cofre,
		Google: httpapi.ConfigGoogle{
			ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
			ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
			PastaDrive:   os.Getenv("GOOGLE_DRIVE_PASTA_ID"),
		},
		Vigia: httpapi.ConfigVigia{
			PingURL:         os.Getenv("VIGIA_PING_URL"),
			BackupDir:       os.Getenv("VIGIA_BACKUP_DIR"),
			CertificadoHost: os.Getenv("VIGIA_CERTIFICADO"),
			Checkin:         os.Getenv("VIGIA_CHECKIN") == "true",
		},
		Checkin: checkin.NovoCliente(envOr("CHECKIN_URL", "http://checkin-whatsapp:3000"), os.Getenv("CHECKIN_ADMIN_TOKEN")),
	}
	if email := smtpDoAmbiente(); email.Configurado() {
		cfg.Vigia.Email = email
	}
	servidor := httpapi.NovoServidor(cfg, pool)
	servidor.RodarTarefasDeFundo(ctx)

	publico := &http.Server{
		Addr:              envOr("API_ADDR", ":8080"),
		Handler:           servidor.Rotas(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	publico.RegisterOnShutdown(servidor.Encerrar)
	// Rotas do worker: porta que o Traefik não roteia.
	interno := &http.Server{
		Addr:              envOr("API_ADDR_INTERNO", ":8081"),
		Handler:           servidor.RotasInternas(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("configuração", "app_origin", cfg.AppOrigin, "cookie_secure", cfg.CookieSecure, "lance_real_habilitado", cfg.LanceRealHabilitado,
		"drive_configurado", cfg.Google.ClientID != "" && cfg.Google.PastaDrive != "",
		"alertas_por_email", cfg.Vigia.Email != nil, "monitor_externo", cfg.Vigia.PingURL != "",
		"vigia_backup", cfg.Vigia.BackupDir != "", "vigia_certificado", cfg.Vigia.CertificadoHost, "vigia_checkin", cfg.Vigia.Checkin,
		"notificador_checkin", cfg.Checkin != nil)

	errc := make(chan error, 2)
	for _, srv := range []*http.Server{publico, interno} {
		go func() {
			slog.Info("api ouvindo", "endereco", srv.Addr)
			errc <- srv.ListenAndServe()
		}()
	}

	var erroServidor error
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			erroServidor = err
		}
	case <-ctx.Done():
		slog.Info("sinal recebido, desligando a api")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(erroServidor, publico.Shutdown(shutdownCtx), interno.Shutdown(shutdownCtx))
}

func migrate(args []string) error {
	if len(args) != 1 {
		return errors.New("uso: api migrate up|down|status")
	}
	dbURL, err := requiredEnv("DATABASE_URL")
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return err
	}
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return fmt.Errorf("carregando migrações: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	switch args[0] {
	case "up":
		results, err := provider.Up(ctx)
		logResults(results)
		if err != nil {
			return err
		}
		if len(results) == 0 {
			slog.Info("migrações: banco já está atualizado")
		}
		// Login do serviço checkin-whatsapp no Postgres (sem a variável, o papel fica sem login).
		if senha := os.Getenv("CHECKIN_DB_SENHA"); senha != "" {
			if err := migrations.DefinirSenhaCheckin(ctx, db, senha); err != nil {
				return err
			}
			slog.Info("migrações: login do papel checkin definido")
		}
		return nil
	case "down":
		result, err := provider.Down(ctx)
		if result != nil {
			logResults([]*goose.MigrationResult{result})
		}
		return err
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return err
		}
		for _, s := range statuses {
			slog.Info("migração", "versao", s.Source.Version, "arquivo", s.Source.Path, "estado", string(s.State), "aplicada_em", s.AppliedAt)
		}
		return nil
	default:
		return fmt.Errorf("subcomando de migrate desconhecido %q (use up, down ou status)", args[0])
	}
}

func logResults(results []*goose.MigrationResult) {
	for _, r := range results {
		if r.Error != nil {
			slog.Error("migração falhou", "versao", r.Source.Version, "arquivo", r.Source.Path, "direcao", r.Direction, "erro", r.Error)
			continue
		}
		slog.Info("migração aplicada", "versao", r.Source.Version, "arquivo", r.Source.Path, "direcao", r.Direction, "duracao", r.Duration.String())
	}
}

func healthcheck() error {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1" + envOr("API_ADDR", ":8080") + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/health respondeu %d", resp.StatusCode)
	}
	return nil
}

func requiredEnv(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("variável de ambiente obrigatória ausente: %s", name)
	}
	return v, nil
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
