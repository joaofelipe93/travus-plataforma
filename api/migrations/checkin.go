package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// TamanhoMinimoSenhaCheckin: o make e o publicar.sh geram 64 caracteres hexadecimais (vale
// também para ASSISTENTE_DB_SENHA).
const TamanhoMinimoSenhaCheckin = 32

// Executor é o que DefinirSenhaCheckin usa: *sql.DB ou *sql.Tx (o teste roda numa transação
// desfeita no fim, porque o papel é do cluster inteiro).
type Executor interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// DefinirSenhaCheckin liga o login do papel `checkin` (criado sem login pela migração
// 00006) com a senha de CHECKIN_DB_SENHA. Roda a cada `api migrate up`: trocar a senha no
// .env e publicar de novo basta.
func DefinirSenhaCheckin(ctx context.Context, db Executor, senha string) error {
	return definirSenhaPapel(ctx, db, "checkin", "CHECKIN_DB_SENHA", "00006", senha)
}

// DefinirSenhaAssistente liga o login do papel `assistente` (migração 00011, só leitura) com a
// senha de ASSISTENTE_DB_SENHA. A API usa esse papel no SELECT livre do assistente.
func DefinirSenhaAssistente(ctx context.Context, db Executor, senha string) error {
	return definirSenhaPapel(ctx, db, "assistente", "ASSISTENTE_DB_SENHA", "00011", senha)
}

func definirSenhaPapel(ctx context.Context, db Executor, papel, variavel, migracao, senha string) error {
	if len(senha) < TamanhoMinimoSenhaCheckin {
		return fmt.Errorf("%s precisa ter ao menos %d caracteres", variavel, TamanhoMinimoSenhaCheckin)
	}
	// ALTER ROLE não aceita parâmetro: o format(%I, %L) do próprio Postgres cita o papel e a senha.
	var comando string
	if err := db.QueryRowContext(ctx, "SELECT format('ALTER ROLE %I WITH LOGIN PASSWORD %L', $1::text, $2::text)", papel, senha).Scan(&comando); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, comando); err != nil {
		// Não devolve o erro cru: a mensagem do Postgres pode repetir o comando com a senha.
		return fmt.Errorf("não foi possível definir a senha do papel %s (a migração %s foi aplicada?)", papel, migracao)
	}
	return nil
}
