package httpapi

// Rodadas automáticas do serviço de boletos (issue #48). A cada minuto a API confere se chegou
// a ocorrência do mês — **dia 8** para buscar e mandar os boletos, **dia 14** para cobrar quem
// não pagou — e põe a rodada na fila do worker.
//
// A instrução da issue está no pacote `internal/calendario`: se o dia cair em sábado, domingo ou
// feriado, **antecipa para o dia útil anterior**. Isso vale para as duas rodadas (decisão do
// usuário em 16/10/2026).
//
// A decisão é tomada com a linha da configuração travada (FOR UPDATE) e a ocorrência atendida
// fica gravada: o laço roda a cada minuto, mas cada rodada dispara uma vez por mês, mesmo que a
// API reinicie. Ocorrência muito atrasada não roda (o vigia avisa) — mandar o boleto do dia 8
// no dia 20 não ajuda ninguém.
//
// Criar a rodada **não manda mensagem**: quem decide isso é a montagem da fila
// (boletos_mensagens.go), com a chave do servidor e a opção da tela.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/calendario"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const (
	// Atraso aceito para disparar (API fora do ar, deploy, banco indisponível).
	toleranciaBoletos = 6 * time.Hour

	boletoDisparoCriada         = "criada"
	boletoDisparoSemClientes    = "sem_clientes"
	boletoDisparoSemEnvio       = "sem_envio"
	boletoDisparoSemResponsavel = "sem_responsavel"
	boletoDisparoAtrasada       = "atrasada"
	boletoDisparoAntesDeLigar   = "antes_de_ligar"
)

func (s *Servidor) rodarAgendadorBoletos(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := s.dispararRodadasAgendadas(ctx); err != nil && ctx.Err() == nil {
			slog.Error("agendador de boletos: falha ao criar a rodada automática", "erro", err)
		}
	}
}

// ultimaOcorrenciaBoletos: a ocorrência mais recente que já passou (a deste mês, se a hora
// chegou; senão a do mês anterior), já com a antecipação para dia útil aplicada.
func ultimaOcorrenciaBoletos(agora time.Time, dia, hora int, antecipar bool) (time.Time, error) {
	local := agora.In(calendario.Fuso)
	deste, err := calendario.Ocorrencia(local.Year(), local.Month(), dia, hora, antecipar)
	if err != nil {
		return time.Time{}, err
	}
	if !deste.After(local) {
		return deste, nil
	}
	anterior := local.AddDate(0, -1, 0)
	return calendario.Ocorrencia(anterior.Year(), anterior.Month(), dia, hora, antecipar)
}

// mesDeReferenciaDaOcorrencia: a rodada de novembro é a que roda em novembro, mesmo que a
// antecipação jogue o dia para o fim de outubro (dia 1 em feriado, por exemplo).
func mesDeReferenciaDaOcorrencia(quando time.Time, dia int) time.Time {
	q := quando.In(calendario.Fuso)
	// A antecipação nunca passa de 15 dias; se o dia resultante for muito maior que o dia
	// escolhido, a ocorrência pertence ao mês seguinte.
	if q.Day() > dia+15 {
		q = q.AddDate(0, 1, 0)
	}
	return time.Date(q.Year(), q.Month(), 1, 0, 0, 0, 0, time.UTC)
}

type rodadaAgendada struct {
	tipo      string
	dia, hora int
	ultimoDia *time.Time
	marcar    func(ctx context.Context, qtx *db.Queries, dia time.Time, resultado string) error
}

// dispararRodadasAgendadas confere as duas rodadas do mês. Erro só quando algo falhou de
// verdade (banco): "não era hora" e "não havia o que fazer" não são erro.
func (s *Servidor) dispararRodadasAgendadas(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
	qtx := s.q.WithTx(tx)

	cfg, err := qtx.TravarConfiguracaoBoletos(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // linha ainda não criada; o próximo GET a recria
	}
	if err != nil {
		return err
	}
	if !cfg.AgendamentoAtivo {
		return nil
	}

	rodadas := []rodadaAgendada{
		{
			tipo: "envio", dia: int(cfg.DiaEnvio), hora: int(cfg.HoraEnvio), ultimoDia: cfg.UltimoEnvioDia,
			marcar: func(ctx context.Context, qtx *db.Queries, dia time.Time, resultado string) error {
				return qtx.RegistrarDisparoEnvio(ctx, db.RegistrarDisparoEnvioParams{Dia: &dia, Resultado: &resultado})
			},
		},
		{
			tipo: "cobranca", dia: int(cfg.DiaCobranca), hora: int(cfg.HoraCobranca), ultimoDia: cfg.UltimaCobrancaDia,
			marcar: func(ctx context.Context, qtx *db.Queries, dia time.Time, resultado string) error {
				return qtx.RegistrarDisparoCobranca(ctx, db.RegistrarDisparoCobrancaParams{Dia: &dia, Resultado: &resultado})
			},
		},
	}

	mudou := false
	for _, r := range rodadas {
		feito, err := s.dispararRodada(ctx, qtx, cfg, r)
		if err != nil {
			return err
		}
		mudou = mudou || feito
	}
	if !mudou {
		return nil
	}
	return tx.Commit(ctx)
}

func (s *Servidor) dispararRodada(ctx context.Context, qtx *db.Queries, cfg db.TravarConfiguracaoBoletosRow, r rodadaAgendada) (bool, error) {
	agora := s.agora()
	devida, err := ultimaOcorrenciaBoletos(agora, r.dia, r.hora, cfg.AnteciparDiaUtil)
	if err != nil {
		return false, nil // dia inválido na configuração: o PUT já recusa, mas não derruba o laço
	}
	dia := time.Date(devida.Year(), devida.Month(), devida.Day(), 0, 0, 0, 0, time.UTC)
	if r.ultimoDia != nil && r.ultimoDia.UTC().Format(time.DateOnly) == dia.Format(time.DateOnly) {
		return false, nil // esta ocorrência já foi atendida
	}

	marcar := func(resultado string) (bool, error) {
		return true, r.marcar(ctx, qtx, dia, resultado)
	}

	if agora.Sub(devida) > toleranciaBoletos {
		slog.Warn("agendador de boletos: ocorrência perdida, rodada não será criada atrasada",
			"tipo", r.tipo, "ocorrencia", devida.Format(time.RFC3339), "atraso", agora.Sub(devida).String())
		return marcar(boletoDisparoAtrasada)
	}
	responsavel := cfg.AtualizadoPor
	if responsavel == nil {
		responsavel = cfg.EnvioHabilitadoPor
	}
	if responsavel == nil {
		slog.Error("agendador de boletos: configuração sem responsável, rodada não criada", "tipo", r.tipo)
		return marcar(boletoDisparoSemResponsavel)
	}

	referencia := mesDeReferenciaDaOcorrencia(devida, r.dia)

	// A cobrança precisa da rodada de envio do mês para saber quem recebeu boleto.
	if r.tipo == "cobranca" {
		base, err := qtx.ParcelasDoEnvio(ctx, referencia)
		if err != nil {
			return false, err
		}
		if len(base) == 0 {
			slog.Warn("agendador de boletos: sem rodada de envio concluída no mês, cobrança não criada",
				"referencia", referencia.Format("2006-01"))
			return marcar(boletoDisparoSemEnvio)
		}
	}

	clientes, err := qtx.ClientesParaBoletos(ctx)
	if err != nil {
		return false, err
	}
	if len(clientes) == 0 {
		slog.Warn("agendador de boletos: nenhum cliente com CPF/CNPJ e cota, rodada não criada", "tipo", r.tipo)
		return marcar(boletoDisparoSemClientes)
	}

	rodada, err := qtx.CriarRodada(ctx, db.CriarRodadaParams{
		Tipo: r.tipo, Referencia: referencia, Origem: origemAgendada, CriadaPor: *responsavel,
	})
	if err != nil {
		return false, err
	}
	for i, c := range clientes {
		if _, err := qtx.CriarRodadaCliente(ctx, db.CriarRodadaClienteParams{
			RodadaID: rodada.ID, ClienteID: c.ID, Ordem: int32(i + 1), ClienteNome: c.Nome,
		}); err != nil {
			return false, err
		}
	}
	if err := qtx.RegistrarEventoBoleto(ctx, db.RegistrarEventoBoletoParams{
		RodadaID: rodada.ID, Nivel: "info", Dados: []byte("{}"),
		Mensagem: fmt.Sprintf("Rodada de %s criada pelo agendamento (dia %d às %02dh) com %d cliente(s).",
			r.tipo, r.dia, r.hora, len(clientes)),
	}); err != nil {
		return false, err
	}
	if err := qtx.RegistrarAuditoria(ctx, paramsAuditoria(nil, responsavel, "boletos_rodada_criada", "boletos_rodada", idTexto(rodada.ID),
		map[string]any{"tipo": r.tipo, "clientes": len(clientes), "origem": origemAgendada,
			"referencia": referencia.Format("2006-01"), "ocorrencia": devida.Format(time.RFC3339)})); err != nil {
		return false, err
	}
	slog.Info("agendador de boletos: rodada criada", "tipo", r.tipo, "rodada", rodada.ID, "clientes", len(clientes))
	return marcar(boletoDisparoCriada)
}

// fecharOcorrenciasAnteriores marca as ocorrências que já passaram como "antes de ligar": quem
// liga o agendamento hoje não quer a rodada do dia 8 do mês passado rodando agora.
func (s *Servidor) fecharOcorrenciasAnteriores(ctx context.Context) {
	c := s.configBoletos(ctx)
	agora := s.agora()
	resultado := boletoDisparoAntesDeLigar

	if devida, err := ultimaOcorrenciaBoletos(agora, c.DiaEnvio, c.HoraEnvio, c.AnteciparDiaUtil); err == nil {
		dia := time.Date(devida.Year(), devida.Month(), devida.Day(), 0, 0, 0, 0, time.UTC)
		if err := s.q.RegistrarDisparoEnvio(ctx, db.RegistrarDisparoEnvioParams{Dia: &dia, Resultado: &resultado}); err != nil {
			slog.Warn("boletos: não foi possível fechar a ocorrência de envio anterior", "erro", err)
		}
	}
	if devida, err := ultimaOcorrenciaBoletos(agora, c.DiaCobranca, c.HoraCobranca, c.AnteciparDiaUtil); err == nil {
		dia := time.Date(devida.Year(), devida.Month(), devida.Day(), 0, 0, 0, 0, time.UTC)
		if err := s.q.RegistrarDisparoCobranca(ctx, db.RegistrarDisparoCobrancaParams{Dia: &dia, Resultado: &resultado}); err != nil {
			slog.Warn("boletos: não foi possível fechar a ocorrência de cobrança anterior", "erro", err)
		}
	}
}
