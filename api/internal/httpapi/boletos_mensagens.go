package httpapi

// Montagem da fila de mensagens do serviço de boletos (issue #48), etapa 3. Roda quando uma
// rodada termina: 'envio' vira uma mensagem por parcela, com o boleto anexado; 'cobranca' vira
// uma mensagem por cliente que **recebeu o boleto e continua com a parcela em aberto**.
//
// Três travas antes de qualquer mensagem sair, nos moldes do lance real:
//   1. a chave do servidor `BOLETOS_ENVIO_HABILITADO` (ambiente);
//   2. `boletos.configuracao.envio_habilitado`, que só admin liga na tela;
//   3. o teto `max_mensagens_rodada`.
// Desligado, a mensagem **é montada e gravada como 'cancelada'**, com o motivo: dá para ver
// exatamente o que sairia (ensaio) sem mandar nada a ninguém.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
	"github.com/joaofelipe93/travus-plataforma/api/internal/mensagem"
)

// ConfigBoletos é a configuração do serviço, já com os padrões no lugar.
type ConfigBoletos struct {
	AgendamentoAtivo bool
	DiaEnvio         int
	HoraEnvio        int
	DiaCobranca      int
	HoraCobranca     int
	AnteciparDiaUtil bool
	// Envio de verdade: precisa desta opção E da chave do servidor.
	EnvioHabilitado    bool
	EnvioHabilitadoPor *int64
	PausaEnvio         time.Duration
	MaxMensagensRodada int
	// Modelos das mensagens (migração 00019): uma mensagem só para todos, com os campos entre
	// chaves preenchidos no envio. Ver api/internal/mensagem.
	TextoEnvio           string
	TextoCobranca        string
	UltimoEnvioDia       *time.Time
	UltimaCobrancaDia    *time.Time
	AtualizadoPorNome    string
	EnvioHabilitadoPorNm string
	AtualizadoEm         time.Time
}

func padroesBoletos() ConfigBoletos {
	// Os mesmos DEFAULTs da migração 00017 (e da issue: dia 8 e dia 14).
	return ConfigBoletos{
		DiaEnvio: 8, HoraEnvio: 9, DiaCobranca: 14, HoraCobranca: 9,
		AnteciparDiaUtil: true, PausaEnvio: 20 * time.Second, MaxMensagensRodada: 200,
		TextoEnvio: mensagem.PadraoEnvio, TextoCobranca: mensagem.PadraoCobranca,
	}
}

func (s *Servidor) configBoletos(ctx context.Context) ConfigBoletos {
	linha, err := s.q.ConfiguracaoBoletos(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		// A linha se recria sozinha (o TRUNCATE dos testes a leva).
		if e := s.q.GarantirConfiguracaoBoletos(ctx); e == nil {
			if linha, err = s.q.ConfiguracaoBoletos(ctx); err == nil {
				return configBoletosDaLinha(linha)
			}
		}
	}
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("configuração dos boletos: usando os padrões", "erro", err)
		}
		return padroesBoletos()
	}
	return configBoletosDaLinha(linha)
}

func configBoletosDaLinha(l db.ConfiguracaoBoletosRow) ConfigBoletos {
	return ConfigBoletos{
		AgendamentoAtivo:     l.AgendamentoAtivo,
		DiaEnvio:             int(l.DiaEnvio),
		HoraEnvio:            int(l.HoraEnvio),
		DiaCobranca:          int(l.DiaCobranca),
		HoraCobranca:         int(l.HoraCobranca),
		AnteciparDiaUtil:     l.AnteciparDiaUtil,
		EnvioHabilitado:      l.EnvioHabilitado,
		EnvioHabilitadoPor:   l.EnvioHabilitadoPor,
		PausaEnvio:           time.Duration(l.PausaEnvioSegundos) * time.Second,
		MaxMensagensRodada:   int(l.MaxMensagensRodada),
		TextoEnvio:           l.TextoEnvio,
		TextoCobranca:        l.TextoCobranca,
		UltimoEnvioDia:       l.UltimoEnvioDia,
		UltimaCobrancaDia:    l.UltimaCobrancaDia,
		AtualizadoPorNome:    valorOuVazio(l.AtualizadoPorNome),
		EnvioHabilitadoPorNm: valorOuVazio(l.EnvioHabilitadoPorNome),
		AtualizadoEm:         l.AtualizadoEm,
	}
}

// envioLiberado: a chave do servidor e a opção da tela, as duas.
func (s *Servidor) envioLiberado(c ConfigBoletos) (bool, string) {
	if !s.cfg.BoletosEnvioHabilitado {
		return false, "ensaio: a chave BOLETOS_ENVIO_HABILITADO do servidor está desligada"
	}
	if !c.EnvioHabilitado {
		return false, "ensaio: o envio está desligado na configuração do serviço"
	}
	return true, ""
}

type resumoMensagens struct {
	Pendentes  int
	Canceladas int
}

// montarMensagens monta a fila da rodada que acabou. Nunca devolve erro por causa de um
// cliente: telefone ruim e parcela sem PDF viram aviso no log da rodada.
func (s *Servidor) montarMensagens(ctx context.Context, qtx *db.Queries, rodadaID int64, tipo string, referencia time.Time) (resumoMensagens, error) {
	cfg := s.configBoletos(ctx)
	liberado, motivo := s.envioLiberado(cfg)
	var r resumoMensagens

	aviso := func(formato string, args ...any) {
		if err := qtx.RegistrarEventoBoleto(ctx, db.RegistrarEventoBoletoParams{
			RodadaID: rodadaID, Nivel: "aviso", Mensagem: fmt.Sprintf(formato, args...), Dados: []byte("{}"),
		}); err != nil {
			slog.Warn("boletos: aviso não registrado", "rodada", rodadaID, "erro", err)
		}
	}

	// enfileirar grava a mensagem já decidida (pendente ou cancelada em ensaio/teto).
	enfileirar := func(p db.CriarMensagemBoletoParams) error {
		if !liberado {
			p.Status, p.UltimoErro, p.AnexoPdf, p.AnexoNome = "cancelada", &motivo, nil, nil
			r.Canceladas++
		} else if r.Pendentes >= cfg.MaxMensagensRodada {
			teto := fmt.Sprintf("teto de %d mensagens por rodada atingido: ligue o resto à mão depois de conferir", cfg.MaxMensagensRodada)
			p.Status, p.UltimoErro, p.AnexoPdf, p.AnexoNome = "cancelada", &teto, nil, nil
			r.Canceladas++
		} else {
			p.Status = "pendente"
			r.Pendentes++
		}
		_, err := qtx.CriarMensagemBoleto(ctx, p)
		return err
	}

	switch tipo {
	case "envio":
		linhas, err := qtx.ParcelasParaMensagem(ctx, rodadaID)
		if err != nil {
			return r, err
		}
		for _, l := range linhas {
			telefone, err := mensagem.Telefone(valorOuVazio(l.Telefone))
			if err != nil {
				aviso("%s: não dá para mandar o boleto da cota %s-%s — %s.", l.ClienteNome, l.Grupo, l.Cota, err)
				continue
			}
			if l.PdfID == nil || len(l.PdfConteudo) == 0 {
				aviso("%s: a parcela %s da cota %s-%s está sem PDF e não vai por WhatsApp.", l.ClienteNome, l.Parcela, l.Grupo, l.Cota)
				continue
			}
			texto := mensagem.Envio(cfg.TextoEnvio, mensagem.Boleto{
				Nome: l.ClienteNome, Grupo: l.Grupo, Cota: l.Cota, Parcela: l.Parcela,
				Vencimento: l.Vencimento, Centavos: l.ValorCentavos,
			})
			parcelaID := l.ID
			if err := enfileirar(db.CriarMensagemBoletoParams{
				RodadaID: &rodadaID, RodadaClienteID: &l.RodadaClienteID, Tipo: "envio",
				ParcelaID: &parcelaID, DestinoTelefone: telefone, DestinoNome: l.ClienteNome,
				Texto: texto, PdfID: l.PdfID, AnexoPdf: l.PdfConteudo, AnexoNome: l.PdfNome,
			}); err != nil {
				return r, err
			}
		}

	case "cobranca":
		linhas, err := qtx.ClientesParaCobranca(ctx, db.ClientesParaCobrancaParams{RodadaID: rodadaID, Referencia: referencia})
		if err != nil {
			return r, err
		}
		for _, l := range linhas {
			telefone, err := mensagem.Telefone(valorOuVazio(l.Telefone))
			if err != nil {
				aviso("%s: continua com boleto em aberto, mas não dá para avisar — %s.", l.ClienteNome, err)
				continue
			}
			clienteID := l.ID
			if err := enfileirar(db.CriarMensagemBoletoParams{
				RodadaID: &rodadaID, RodadaClienteID: &clienteID, Tipo: "cobranca",
				DestinoTelefone: telefone, DestinoNome: l.ClienteNome,
				Texto: mensagem.Cobranca(cfg.TextoCobranca, l.ClienteNome),
			}); err != nil {
				return r, err
			}
		}
	}

	nivel, resumo := "ok", fmt.Sprintf("%d mensagem(ns) na fila do WhatsApp.", r.Pendentes)
	if r.Canceladas > 0 {
		nivel = "aviso"
		resumo = fmt.Sprintf("%d mensagem(ns) na fila e %d montada(s) sem enviar (%s).", r.Pendentes, r.Canceladas, motivo)
		if liberado {
			resumo = fmt.Sprintf("%d mensagem(ns) na fila e %d acima do teto da rodada.", r.Pendentes, r.Canceladas)
		}
	}
	if r.Pendentes == 0 && r.Canceladas == 0 {
		nivel, resumo = "info", "Nenhuma mensagem para enviar."
	}
	if err := qtx.RegistrarEventoBoleto(ctx, db.RegistrarEventoBoletoParams{
		RodadaID: rodadaID, Nivel: nivel, Mensagem: resumo, Dados: []byte("{}"),
	}); err != nil {
		return r, err
	}
	return r, nil
}
