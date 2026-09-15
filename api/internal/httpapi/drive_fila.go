package httpapi

// Fila de envio dos comprovantes (PDF) ao Google Drive. O lance fica registrado mesmo que o
// envio falhe: a fila tenta de novo com espera crescente, e dá para pedir reenvio na tela.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/joaofelipe93/travus-plataforma/api/internal/cripto"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
	"github.com/joaofelipe93/travus-plataforma/api/internal/drive"
)

const (
	IntegracaoGoogleDrive = "google_drive"
	contextoCofreDrive    = "integracao:google_drive"
)

func (s *Servidor) avisarDrive() {
	select {
	case s.acordarDrive <- struct{}{}:
	default:
	}
}

// ImportarTokenDrive grava cifrado o token no formato do token.json do googleapis.
func ImportarTokenDrive(ctx context.Context, q *db.Queries, cofre *cripto.Cofre, bruto []byte) (drive.TokenNode, error) {
	var tn drive.TokenNode
	if err := json.Unmarshal(bruto, &tn); err != nil {
		return tn, fmt.Errorf("token.json inválido: %w", err)
	}
	if _, err := tn.OAuth2(); err != nil {
		return tn, err
	}
	return tn, salvarTokenDrive(ctx, q, cofre, tn)
}

func salvarTokenDrive(ctx context.Context, q *db.Queries, cofre *cripto.Cofre, tn drive.TokenNode) error {
	limpo, _ := json.Marshal(tn)
	cifrado, err := cofre.Cifrar(limpo, contextoCofreDrive)
	if err != nil {
		return err
	}
	return q.SalvarIntegracao(ctx, db.SalvarIntegracaoParams{Nome: IntegracaoGoogleDrive, DadosCifrados: cifrado})
}

// clienteDrive devolve o enviador, ou nil com o motivo quando o Drive não está configurado.
func (s *Servidor) clienteDrive(ctx context.Context) (drive.Enviador, string, error) {
	if s.enviadorDrive != nil {
		return s.enviadorDrive, "", nil
	}
	g := s.cfg.Google
	switch {
	case s.cfg.Cofre == nil:
		return nil, "CHAVE_CRIPTOGRAFIA não configurada", nil
	case g.ClientID == "" || g.ClientSecret == "" || g.PastaDrive == "":
		return nil, "Google Drive não configurado (GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET e GOOGLE_DRIVE_PASTA_ID)", nil
	}
	integ, err := s.q.BuscarIntegracao(ctx, IntegracaoGoogleDrive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "token do Google não importado (make google-token)", nil
	}
	if err != nil {
		return nil, "", err
	}

	s.driveMu.Lock()
	defer s.driveMu.Unlock()
	if s.driveCliente != nil && s.driveVersao.Equal(integ.AtualizadoEm) {
		return s.driveCliente, "", nil
	}
	bruto, err := s.cfg.Cofre.Decifrar(integ.DadosCifrados, contextoCofreDrive)
	if err != nil {
		return nil, "", fmt.Errorf("o token do Google não decifra (CHAVE_CRIPTOGRAFIA trocada?): %w", err)
	}
	var tn drive.TokenNode
	if err := json.Unmarshal(bruto, &tn); err != nil {
		return nil, "", err
	}
	tok, err := tn.OAuth2()
	if err != nil {
		return nil, "", err
	}
	s.driveCliente = drive.NovoCliente(g.ClientID, g.ClientSecret, tok, g.PastaDrive, func(novo *oauth2.Token) {
		atualizado := drive.TokenNode{
			AccessToken: novo.AccessToken, RefreshToken: tn.RefreshToken, Scope: tn.Scope,
			TokenType: novo.TokenType, ExpiryDate: novo.Expiry.UnixMilli(),
		}
		if novo.RefreshToken != "" {
			atualizado.RefreshToken = novo.RefreshToken
		}
		if err := salvarTokenDrive(context.Background(), s.q, s.cfg.Cofre, atualizado); err != nil {
			slog.Warn("drive: falha ao salvar o token renovado", "erro", err)
		}
	})
	s.driveVersao = integ.AtualizadoEm
	return s.driveCliente, "", nil
}

func (s *Servidor) rodarFilaDrive(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.processarFilaDrive(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.acordarDrive:
		}
	}
}

// processarFilaDrive envia até 5 comprovantes pendentes. Devolve quantos foram enviados.
func (s *Servidor) processarFilaDrive(ctx context.Context) int {
	enviador, motivo, err := s.clienteDrive(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("drive: não foi possível preparar o envio", "erro", err)
		}
		return 0
	}
	if enviador == nil {
		if motivo != s.avisouDrive {
			slog.Info("drive: envio de comprovantes parado", "motivo", motivo)
			s.avisouDrive = motivo
		}
		return 0
	}
	s.avisouDrive = ""

	if _, err := s.q.RecuperarEnviosTravados(ctx); err != nil {
		slog.Warn("drive: falha ao recuperar envios travados", "erro", err)
	}
	ids, err := s.q.ReservarLancesParaDrive(ctx, 5)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("drive: falha ao reservar lances", "erro", err)
		}
		return 0
	}
	enviados := 0
	for _, id := range ids {
		d, err := s.q.DadosParaDrive(ctx, id)
		if err != nil {
			s.marcarErroDrive(ctx, id, fmt.Errorf("lendo o comprovante: %w", err))
			continue
		}
		nome := drive.NomeArquivo(d.ClienteNome, d.Grupo, d.Cota, d.Versao, d.DataLance.In(time.Local))
		arquivo, err := enviador.Enviar(ctx, nome, d.Pdf)
		if err != nil {
			s.marcarErroDrive(ctx, id, err)
			continue
		}
		if err := s.q.MarcarDriveEnviado(ctx, db.MarcarDriveEnviadoParams{ID: id, DriveArquivoID: &arquivo.ID, DriveLink: textoOuNil(arquivo.Link)}); err != nil {
			slog.Error("drive: enviado, mas não foi possível registrar", "lance", id, "arquivo", arquivo.ID, "erro", err)
			continue
		}
		slog.Info("drive: comprovante enviado", "lance", id, "protocolo", d.Protocolo)
		enviados++
	}
	return enviados
}

func (s *Servidor) marcarErroDrive(ctx context.Context, id int64, err error) {
	mensagem := strings.ToValidUTF8(err.Error(), "")
	if utf8.RuneCountInString(mensagem) > 500 {
		mensagem = string([]rune(mensagem)[:500])
	}
	slog.Warn("drive: falha ao enviar comprovante", "lance", id, "erro", mensagem)
	if e := s.q.MarcarDriveErro(ctx, db.MarcarDriveErroParams{ID: id, DriveErro: &mensagem}); e != nil {
		slog.Error("drive: falha ao registrar o erro", "lance", id, "erro", e)
	}
}
