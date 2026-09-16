package migrations_test

// Testes de integração: precisam de Postgres (TEST_DATABASE_URL). Rode com `make test`.

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/joaofelipe93/travus-plataforma/api/internal/testedb"
	"github.com/joaofelipe93/travus-plataforma/api/migrations"
)

// transacaoTeste abre uma transação que é sempre desfeita: o papel checkin é do cluster
// inteiro (o mesmo do banco de desenvolvimento) e nada aqui pode sobrar.
func transacaoTeste(t *testing.T) *sql.Tx {
	t.Helper()
	testedb.Abrir(t) // pula sem TEST_DATABASE_URL e aplica as migrações
	db, err := sql.Open("pgx", os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func codigoPostgres(err error) string {
	if pgErr, ok := err.(*pgconn.PgError); ok {
		return pgErr.Code
	}
	return ""
}

func TestPapelCheckinSoEnxergaOProprioSchema(t *testing.T) {
	tx := transacaoTeste(t)
	ctx := context.Background()

	if _, err := tx.ExecContext(ctx, "SET LOCAL ROLE checkin"); err != nil {
		t.Fatal(err)
	}

	var eventoID int64
	err := tx.QueryRowContext(ctx,
		`INSERT INTO checkin.eventos (chave_dedup, origem, payload) VALUES ('teste:1', 'teste', '{}') RETURNING id`,
	).Scan(&eventoID)
	if err != nil {
		t.Fatalf("checkin deveria gravar evento: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO checkin.mensagens (evento_id, destino_jid, texto) VALUES ($1, '1-1@g.us', 'x')`, eventoID,
	); err != nil {
		t.Fatalf("checkin deveria enfileirar mensagem: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE checkin.mensagens SET status = 'enviada' WHERE evento_id = $1`, eventoID); err != nil {
		t.Fatalf("checkin deveria atualizar mensagem: %v", err)
	}

	// Um SAVEPOINT por tentativa recusada: o erro aborta a transação até o ROLLBACK TO.
	for _, consulta := range []string{
		"SELECT count(*) FROM public.usuarios",
		"SELECT count(*) FROM public.clientes",
		"SELECT count(*) FROM public.integracoes",
		"DELETE FROM checkin.eventos",
	} {
		if _, err := tx.ExecContext(ctx, "SAVEPOINT tentativa"); err != nil {
			t.Fatal(err)
		}
		_, err := tx.ExecContext(ctx, consulta)
		if codigoPostgres(err) != "42501" { // insufficient_privilege
			t.Errorf("%q: esperado permissão negada, veio %v", consulta, err)
		}
		if _, err := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT tentativa"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDefinirSenhaCheckin(t *testing.T) {
	tx := transacaoTeste(t)
	ctx := context.Background()

	if err := migrations.DefinirSenhaCheckin(ctx, tx, "curta"); err == nil ||
		!strings.Contains(err.Error(), "CHECKIN_DB_SENHA") {
		t.Fatalf("senha curta deveria ser recusada, veio %v", err)
	}

	// Aspas na senha: a citação é do Postgres (format %L), não concatenação.
	senha := "senha-de-teste-com-'aspas'-0123456789abcdef"
	if err := migrations.DefinirSenhaCheckin(ctx, tx, senha); err != nil {
		t.Fatal(err)
	}
	var podeLogar bool
	if err := tx.QueryRowContext(ctx, "SELECT rolcanlogin FROM pg_roles WHERE rolname = 'checkin'").Scan(&podeLogar); err != nil {
		t.Fatal(err)
	}
	if !podeLogar {
		t.Error("o papel checkin deveria poder logar depois de DefinirSenhaCheckin")
	}
}
