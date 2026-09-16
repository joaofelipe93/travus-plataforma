package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// TamanhoMinimoSenhaCheckin: o make e o publicar.sh geram 64 caracteres hexadecimais.
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
	if len(senha) < TamanhoMinimoSenhaCheckin {
		return fmt.Errorf("CHECKIN_DB_SENHA precisa ter ao menos %d caracteres", TamanhoMinimoSenhaCheckin)
	}
	// ALTER ROLE não aceita parâmetro: o format(%L) do próprio Postgres cita a senha.
	var comando string
	if err := db.QueryRowContext(ctx, "SELECT format('ALTER ROLE checkin WITH LOGIN PASSWORD %L', $1::text)", senha).Scan(&comando); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, comando); err != nil {
		// Não devolve o erro cru: a mensagem do Postgres pode repetir o comando com a senha.
		return errors.New("não foi possível definir a senha do papel checkin (a migração 00006 foi aplicada?)")
	}
	return nil
}
