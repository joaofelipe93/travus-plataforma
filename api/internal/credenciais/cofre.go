package credenciais

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/cripto"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

var (
	// ErrSemCofre: a API subiu sem CHAVE_CRIPTOGRAFIA, então não há onde guardar segredo.
	ErrSemCofre = errors.New("credenciais: a API está sem CHAVE_CRIPTOGRAFIA")
	// ErrNaoCadastrada: é o que um serviço vê quando o operador ainda não cadastrou a
	// credencial. A tela pede o token no lugar de falhar.
	ErrNaoCadastrada = errors.New("credenciais: a pessoa ainda não cadastrou esta credencial")
)

// contexto amarra o valor cifrado à pessoa e à credencial: o que está gravado para uma não
// decifra no lugar da outra.
func contexto(usuarioID int64, credencial string) string {
	return fmt.Sprintf("credencial:%d:%s", usuarioID, credencial)
}

// Gravar cifra os valores já validados e substitui o que houver. Devolve a linha para a
// tela (dica e datas), nunca o valor.
func Gravar(ctx context.Context, q *db.Queries, cofre *cripto.Cofre, usuarioID int64, d Definicao, valores map[string]string) (db.GravarCredencialDoUsuarioRow, error) {
	if cofre == nil {
		return db.GravarCredencialDoUsuarioRow{}, ErrSemCofre
	}
	bruto, err := json.Marshal(valores)
	if err != nil {
		return db.GravarCredencialDoUsuarioRow{}, err
	}
	cifrado, err := cofre.Cifrar(bruto, contexto(usuarioID, d.ID))
	if err != nil {
		return db.GravarCredencialDoUsuarioRow{}, err
	}
	return q.GravarCredencialDoUsuario(ctx, db.GravarCredencialDoUsuarioParams{
		UsuarioID: usuarioID, Credencial: d.ID, DadosCifrados: cifrado, Dica: d.Dica(valores),
	})
}

// Ler devolve os valores decifrados: é o que um serviço usa para agir em nome da pessoa.
// Sem cadastro, ErrNaoCadastrada — o serviço então pede o token na tela.
func Ler(ctx context.Context, q *db.Queries, cofre *cripto.Cofre, usuarioID int64, credencial string) (map[string]string, error) {
	if cofre == nil {
		return nil, ErrSemCofre
	}
	if _, err := Buscar(credencial); err != nil {
		return nil, err
	}
	row, err := q.BuscarCredencialDoUsuario(ctx, db.BuscarCredencialDoUsuarioParams{
		UsuarioID: usuarioID, Credencial: credencial,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNaoCadastrada
	}
	if err != nil {
		return nil, err
	}
	bruto, err := cofre.Decifrar(row.DadosCifrados, contexto(usuarioID, credencial))
	if err != nil {
		return nil, fmt.Errorf("credencial %q de %d: %w", credencial, usuarioID, err)
	}
	var valores map[string]string
	if err := json.Unmarshal(bruto, &valores); err != nil {
		return nil, err
	}
	return valores, nil
}

// Apagar remove a credencial da pessoa. false: não havia nada gravado.
func Apagar(ctx context.Context, q *db.Queries, usuarioID int64, credencial string) (bool, error) {
	n, err := q.ApagarCredencialDoUsuario(ctx, db.ApagarCredencialDoUsuarioParams{
		UsuarioID: usuarioID, Credencial: credencial,
	})
	return n > 0, err
}
