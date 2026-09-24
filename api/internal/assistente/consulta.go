package assistente

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// Linhas devolvidas ao modelo: para listar muito, ele deve agregar.
	MaximoLinhas = 200
	// Tempo total da consulta (o papel já tem statement_timeout de 5 s; este é o do lado da API).
	tempoConsulta = 8 * time.Second
	maximoSQL     = 8_000
)

// reComeco: só SELECT e WITH. Não é a barreira de segurança (essa é o papel só leitura, a
// transação READ ONLY e o protocolo, que não aceita dois comandos); é para errar cedo e com
// mensagem clara quando o modelo tenta outra coisa.
var reComeco = regexp.MustCompile(`(?is)^\s*(\(\s*)*(select|with|values|table)\b`)

// ResultadoConsulta vai para o modelo em JSON.
type ResultadoConsulta struct {
	Colunas []string `json:"colunas"`
	Linhas  [][]any  `json:"linhas"`
	// true quando havia mais que MaximoLinhas linhas.
	Cortado bool `json:"cortado,omitempty"`
}

// Consultor roda o SELECT do modelo com o papel `assistente` (pool próprio).
type Consultor struct {
	Pool *pgxpool.Pool
}

// Consultar roda uma consulta só de leitura e devolve até MaximoLinhas linhas.
func (c *Consultor) Consultar(ctx context.Context, sql string) (*ResultadoConsulta, error) {
	sql = strings.TrimSpace(sql)
	sql = strings.TrimSuffix(sql, ";")
	if sql == "" {
		return nil, errors.New("consulta vazia")
	}
	if len(sql) > maximoSQL {
		return nil, fmt.Errorf("consulta longa demais (máximo %d caracteres)", maximoSQL)
	}
	if !reComeco.MatchString(sql) {
		return nil, errors.New("só consultas de leitura (SELECT ou WITH)")
	}

	ctx, cancel := context.WithTimeout(ctx, tempoConsulta)
	defer cancel()
	tx, err := c.Pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, errors.New("banco indisponível para consulta")
	}
	// Só leitura: nada a gravar, sempre desfaz.
	defer func() { _ = tx.Rollback(context.Background()) }()

	linhas, err := tx.Query(ctx, sql)
	if err != nil {
		return nil, erroDoBanco(err)
	}
	defer linhas.Close()

	res := &ResultadoConsulta{Linhas: [][]any{}}
	for _, f := range linhas.FieldDescriptions() {
		res.Colunas = append(res.Colunas, f.Name)
	}
	for linhas.Next() {
		if len(res.Linhas) == MaximoLinhas {
			res.Cortado = true
			break
		}
		valores, err := linhas.Values()
		if err != nil {
			return nil, erroDoBanco(err)
		}
		for i, v := range valores {
			valores[i] = paraJSON(v)
		}
		res.Linhas = append(res.Linhas, valores)
	}
	if err := linhas.Err(); err != nil {
		return nil, erroDoBanco(err)
	}
	return res, nil
}

// erroDoBanco devolve ao modelo a mensagem do Postgres (ele usa para corrigir o SQL). O papel
// não enxerga segredo, então a mensagem também não tem.
func erroDoBanco(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		msg := pgErr.Message
		if pgErr.Code == "42501" {
			msg += " (tabela ou coluna fora do alcance do assistente)"
		}
		if pgErr.Code == "57014" {
			msg = "a consulta passou do tempo limite: simplifique ou filtre mais"
		}
		if pgErr.Hint != "" {
			msg += ". Dica: " + pgErr.Hint
		}
		return fmt.Errorf("erro do Postgres (%s): %s", pgErr.Code, msg)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("a consulta passou do tempo limite: simplifique ou filtre mais")
	}
	return errors.New("falha ao consultar o banco")
}

// paraJSON converte o que o pgx devolve em algo que o encoding/json escreve de forma legível.
func paraJSON(v any) any {
	switch x := v.(type) {
	case nil, string, bool, int16, int32, int64, float32, float64:
		return x
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 && x.Nanosecond() == 0 && x.Location() == time.UTC {
			return x.Format("2006-01-02")
		}
		return x.In(Fuso).Format(time.RFC3339)
	case [16]byte:
		return fmt.Sprintf("%x-%x-%x-%x-%x", x[0:4], x[4:6], x[6:8], x[8:10], x[10:16])
	case []byte:
		if len(x) > 64 {
			return fmt.Sprintf("<%d bytes>", len(x))
		}
		return "\\x" + hex.EncodeToString(x)
	case map[string]any, []any:
		return x
	case fmt.Stringer:
		return x.String()
	}
	if b, err := json.Marshal(v); err == nil {
		var bruto json.RawMessage = b
		return bruto
	}
	return fmt.Sprint(v)
}

// Fuso de Brasília: datas das perguntas e das respostas.
var Fuso = func() *time.Location {
	if l, err := time.LoadLocation("America/Sao_Paulo"); err == nil {
		return l
	}
	return time.FixedZone("BRT", -3*60*60)
}()
