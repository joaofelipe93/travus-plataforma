// Package testedb prepara o Postgres dos testes de integração.
package testedb

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/joaofelipe93/travus-plataforma/api/migrations"
)

var (
	prepararOnce sync.Once
	prepararErr  error
)

// Abrir devolve um pool para o banco de TEST_DATABASE_URL com as tabelas vazias.
// Na primeira chamada do processo, recria o esquema do zero e aplica as migrações.
// Sem a variável, o teste é pulado (`make test` define).
func Abrir(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL não definida: teste de integração pulado (use make test)")
	}
	ctx := context.Background()
	prepararOnce.Do(func() { prepararErr = preparar(ctx, dsn) })
	if prepararErr != nil {
		t.Fatalf("preparando o banco de teste: %v", prepararErr)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, "TRUNCATE auditoria, sessoes, cotas, importacoes, clientes, usuarios RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
	return pool
}

func preparar(ctx context.Context, dsn string) error {
	if err := criarBancoSeFaltar(ctx, dsn); err != nil {
		return err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS checkin CASCADE; DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public"); err != nil {
		return fmt.Errorf("recriando esquema: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

func criarBancoSeFaltar(ctx context.Context, dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return err
	}
	nome := strings.TrimPrefix(u.Path, "/")
	if !strings.Contains(nome, "teste") {
		return fmt.Errorf("TEST_DATABASE_URL precisa apontar para um banco de teste (nome com \"teste\"), recebido %q", nome)
	}
	admin := *u
	admin.Path = "/postgres"
	conn, err := pgx.Connect(ctx, admin.String())
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	var existe bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", nome).Scan(&existe); err != nil {
		return err
	}
	if !existe {
		_, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{nome}.Sanitize())
	}
	return err
}
