// Comando da API da Travus Plataforma.
//
//	api serve                 sobe o servidor HTTP (padrão)
//	api migrate up|down|status aplica, desfaz (a última) ou lista as migrações
//	api healthcheck           consulta o /health local (healthcheck do Docker)
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
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

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
	case "healthcheck":
		err = healthcheck()
	default:
		err = fmt.Errorf("comando desconhecido %q (use serve, migrate ou healthcheck)", cmd)
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
	// pgxpool conecta sob demanda: a API sobe mesmo com o banco fora, e o /health mostra isso.
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("configuração do banco inválida: %w", err)
	}
	defer pool.Close()

	srv := &http.Server{
		Addr:              envOr("API_ADDR", ":8080"),
		Handler:           httpapi.NewRouter(pool),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("api ouvindo", "endereco", srv.Addr)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	slog.Info("sinal recebido, desligando a api")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
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
