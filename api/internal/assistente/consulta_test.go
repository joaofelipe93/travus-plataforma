package assistente_test

// Testes de integração: precisam de Postgres (TEST_DATABASE_URL). Rode com `make test`.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joaofelipe93/travus-plataforma/api/internal/assistente"
	"github.com/joaofelipe93/travus-plataforma/api/internal/testedb"
)

// consultorTeste usa o banco de teste com o papel assistente. Em produção o pool conecta com o
// login do papel; aqui é SET ROLE, porque o papel é do cluster inteiro e trocar a senha dele no
// teste mexeria no banco de desenvolvimento. Por isso "voltar de papel" (SET ROLE/set_config) não
// se testa aqui: com SET ROLE, o Postgres confere contra o usuário da sessão (o superusuário do
// teste); com o login próprio do papel, não há papel para onde ir.
func consultorTeste(t *testing.T) (*assistente.Consultor, *pgxpool.Pool) {
	t.Helper()
	admin := testedb.Abrir(t)
	cfg, err := pgxpool.ParseConfig(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET ROLE assistente")
		return err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &assistente.Consultor{Pool: pool}, admin
}

func TestConsultaDevolveLinhasLegiveis(t *testing.T) {
	c, admin := consultorTeste(t)
	ctx := context.Background()
	if _, err := admin.Exec(ctx, `INSERT INTO clientes (nome, nome_normalizado, origem) VALUES ('ANA TESTE', 'ANA TESTE', 'cadastro')`); err != nil {
		t.Fatal(err)
	}

	res, err := c.Consultar(ctx, `SELECT nome, criado_em, DATE '2026-09-23' AS dia, 12.50::numeric AS valor,
		'{"a":1}'::jsonb AS dados, gen_random_uuid() AS id, null AS nada FROM clientes;`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Colunas, ",") != "nome,criado_em,dia,valor,dados,id,nada" || len(res.Linhas) != 1 {
		t.Fatalf("resultado inesperado: %+v", res)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, quer := range []string{`"ANA TESTE"`, `"2026-09-23"`, `"a":1`, `-03:00"`} {
		if !strings.Contains(s, quer) {
			t.Errorf("JSON deveria ter %s: %s", quer, s)
		}
	}
	if !strings.Contains(s, `12.5`) {
		t.Errorf("numeric deveria virar número legível: %s", s)
	}
}

func TestConsultaCortaEmMaximoLinhas(t *testing.T) {
	c, _ := consultorTeste(t)
	res, err := c.Consultar(context.Background(), "SELECT n FROM generate_series(1, 500) n")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Linhas) != assistente.MaximoLinhas || !res.Cortado {
		t.Fatalf("quer %d linhas e cortado, veio %d (cortado=%v)", assistente.MaximoLinhas, len(res.Linhas), res.Cortado)
	}
}

func TestConsultaRecusaOQueNaoELeitura(t *testing.T) {
	c, _ := consultorTeste(t)
	for consulta, quer := range map[string]string{
		"DELETE FROM clientes":                                         "só consultas de leitura",
		"SELECT 1; DELETE FROM clientes":                               "",
		"WITH x AS (DELETE FROM clientes RETURNING 1) SELECT * FROM x": "",
		"SELECT senha_hash FROM usuarios":                              "fora do alcance do assistente",
		"SELECT * FROM sessoes":                                        "fora do alcance do assistente",
		"SELECT pg_read_file('/etc/passwd')":                           "",
		"":                                                             "consulta vazia",
	} {
		_, err := c.Consultar(context.Background(), consulta)
		if err == nil {
			t.Errorf("%q deveria ser recusada", consulta)
			continue
		}
		if quer != "" && !strings.Contains(err.Error(), quer) {
			t.Errorf("%q: erro deveria mencionar %q, veio %v", consulta, quer, err)
		}
	}
}
