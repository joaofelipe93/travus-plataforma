package migrations_test

// Testes de integração: precisam de Postgres (TEST_DATABASE_URL). Rode com `make test`.

import (
	"context"
	"strings"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/migrations"
)

func TestPapelAssistenteSoLeOPermitido(t *testing.T) {
	tx := transacaoTeste(t)
	ctx := context.Background()

	if _, err := tx.ExecContext(ctx, "SET LOCAL ROLE assistente"); err != nil {
		t.Fatal(err)
	}

	for _, consulta := range []string{
		"SELECT count(*) FROM clientes",
		"SELECT count(*) FROM cotas",
		"SELECT count(*) FROM execucoes",
		"SELECT count(*) FROM execucao_cotas",
		"SELECT count(*) FROM lances",
		"SELECT id, nome, email, perfil FROM usuarios",
		"SELECT id, tipo, tamanho FROM arquivos",
		"SELECT nome, atualizado_em FROM integracoes",
		"SELECT usuario_id, credencial FROM credenciais_usuario",
		"SELECT acao, criado_em FROM auditoria",
		"SELECT count(*) FROM checkin.eventos",
		"SELECT count(*) FROM checkin.reservas",
		"SELECT count(*) FROM checkin.mensagens",
		"SELECT count(*) FROM checkin.resumos",
		"SELECT count(*) FROM checkin.configuracao",
	} {
		if _, err := tx.ExecContext(ctx, consulta); err != nil {
			t.Fatalf("assistente deveria ler %q: %v", consulta, err)
		}
	}

	// Um SAVEPOINT por tentativa recusada: o erro aborta a transação até o ROLLBACK TO.
	for _, consulta := range []string{
		"SELECT senha_hash FROM usuarios",
		"SELECT * FROM usuarios",
		"SELECT count(*) FROM sessoes",
		"SELECT dados_cifrados FROM integracoes",
		"SELECT dados_cifrados FROM credenciais_usuario",
		"SELECT dica FROM credenciais_usuario",
		"SELECT conteudo FROM arquivos",
		"SELECT ip FROM auditoria",
		"SELECT linhas FROM importacoes",
		"SELECT count(*) FROM goose_db_version",
		"INSERT INTO checkin.resumos (tipo, data, reservas) VALUES ('hoje', '2026-10-26', 0)",
		"UPDATE clientes SET nome = nome",
		"DELETE FROM lances",
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

func TestPapelAssistenteNasceSoLeitura(t *testing.T) {
	tx := transacaoTeste(t)
	ctx := context.Background()

	var config string
	if err := tx.QueryRowContext(ctx,
		`SELECT array_to_string(s.setconfig, ',') FROM pg_db_role_setting s
		   JOIN pg_roles r ON r.oid = s.setrole WHERE r.rolname = 'assistente' AND s.setdatabase = 0`,
	).Scan(&config); err != nil {
		t.Fatal(err)
	}
	for _, quer := range []string{"default_transaction_read_only=on", "statement_timeout=5s"} {
		if !strings.Contains(config, quer) {
			t.Errorf("o papel assistente deveria ter %s, tem %q", quer, config)
		}
	}
	var super, herda bool
	if err := tx.QueryRowContext(ctx, "SELECT rolsuper, rolinherit FROM pg_roles WHERE rolname = 'assistente'").Scan(&super, &herda); err != nil {
		t.Fatal(err)
	}
	if super || herda {
		t.Errorf("assistente não pode ser superusuário nem herdar papéis (super=%v, herda=%v)", super, herda)
	}
}

func TestDefinirSenhaAssistente(t *testing.T) {
	tx := transacaoTeste(t)
	ctx := context.Background()

	if err := migrations.DefinirSenhaAssistente(ctx, tx, "curta"); err == nil ||
		!strings.Contains(err.Error(), "ASSISTENTE_DB_SENHA") {
		t.Fatalf("senha curta deveria ser recusada, veio %v", err)
	}
	if err := migrations.DefinirSenhaAssistente(ctx, tx, "senha-de-teste-com-'aspas'-0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	var podeLogar bool
	if err := tx.QueryRowContext(ctx, "SELECT rolcanlogin FROM pg_roles WHERE rolname = 'assistente'").Scan(&podeLogar); err != nil {
		t.Fatal(err)
	}
	if !podeLogar {
		t.Error("o papel assistente deveria poder logar depois de DefinirSenhaAssistente")
	}
}
