package httpapi

// Dry-run automático do Canopus: a cada minuto a API confere se chegou a ocorrência marcada na
// configuração (dia, hora e minuto, fuso de Brasília) e, se chegou, põe na fila um **dry-run**
// com todas as cotas ativas. Só dry-run: nada aqui cria execução real nem aprova lance — isso
// continua passando pela revisão e por um admin.
//
// A decisão é tomada com a linha da configuração travada (FOR UPDATE) e a ocorrência atendida
// fica gravada (ultimo_disparo_dia): o laço roda a cada minuto, mas o mês dispara uma vez só,
// mesmo que a API reinicie. Ocorrência que passou da tolerância não é disparada atrasada — o
// vigia avisa que ela não rodou.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/alerta"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const (
	// Atraso aceito para disparar (API fora do ar, deploy, banco indisponível). Passando
	// disso, o dry-run do mês não roda: o Newcon pode já estar perto da assembleia.
	toleranciaAgendamento = 6 * time.Hour
	// Folga antes de considerar que a ocorrência não rodou (o laço acorda a cada minuto).
	folgaAgendamento = 15 * time.Minute

	disparoCriada         = "criada"
	disparoSemCotas       = "sem_cotas"
	disparoSemResponsavel = "sem_responsavel"
	disparoAtrasada       = "atrasada"
	disparoAntesDeLigar   = "antes_de_ligar"
)

func (s *Servidor) rodarAgendador(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.cicloAgendador(ctx)
	}
}

func (s *Servidor) cicloAgendador(ctx context.Context) {
	if err := s.dispararDryRunAgendado(ctx); err != nil && ctx.Err() == nil {
		slog.Error("agendador: falha ao criar o dry-run automático", "erro", err)
	}
	s.avisarExecucoesAgendadas(ctx)
}

// ultimaOcorrencia é a ocorrência mais recente do dia/hora escolhidos que já passou (a deste
// mês, se a hora já chegou; senão a do mês anterior). Mês sem o dia escolhido usa o último dia.
func ultimaOcorrencia(agora time.Time, dia, hora, minuto int) time.Time {
	local := agora.In(fusoCanopus)
	primeiroDoMes := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, fusoCanopus)
	if quando := ocorrenciaNoMes(primeiroDoMes, dia, hora, minuto); !quando.After(local) {
		return quando
	}
	return ocorrenciaNoMes(primeiroDoMes.AddDate(0, -1, 0), dia, hora, minuto)
}

// diaDaOcorrencia: a data (sem hora) que fica gravada em ultimo_disparo_dia. Meia-noite em UTC
// para o tipo date ir e voltar do Postgres sem escorregar de dia.
func diaDaOcorrencia(quando time.Time) time.Time {
	q := quando.In(fusoCanopus)
	return time.Date(q.Year(), q.Month(), q.Day(), 0, 0, 0, 0, time.UTC)
}

func mesmoDia(a, b time.Time) bool {
	return a.UTC().Format("2006-01-02") == b.UTC().Format("2006-01-02")
}

// dispararDryRunAgendado cria o dry-run do mês quando chega a hora. Devolve erro só quando algo
// falhou de verdade (banco); "não era hora" e "não havia o que rodar" não são erro.
func (s *Servidor) dispararDryRunAgendado(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	cfg, err := qtx.TravarConfiguracaoCanopus(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		// Linha ainda não criada (migração nova, banco recém-limpo): o próximo GET a recria.
		return nil
	}
	if err != nil {
		return err
	}
	if !cfg.DryRunAutomatico {
		return nil
	}

	agora := s.agora()
	devida := ultimaOcorrencia(agora, int(cfg.DryRunDia), int(cfg.DryRunHora), int(cfg.DryRunMinuto))
	dia := diaDaOcorrencia(devida)
	if cfg.UltimoDisparoDia != nil && mesmoDia(*cfg.UltimoDisparoDia, dia) {
		return nil // esta ocorrência já foi atendida
	}

	marcar := func(resultado string) error {
		if err := qtx.MarcarDisparoDryRun(ctx, db.MarcarDisparoDryRunParams{
			UltimoDisparoDia: &dia, UltimoDisparoResultado: &resultado,
		}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	// A API estava fora do ar na hora marcada: não roda atrasado (o vigia avisa).
	if agora.Sub(devida) > toleranciaAgendamento {
		slog.Warn("agendador: ocorrência perdida, dry-run automático não será criado atrasado",
			"ocorrencia", devida.Format(time.RFC3339), "atraso", agora.Sub(devida).String())
		return marcar(disparoAtrasada)
	}
	// Só por escrita direta no banco: o agendamento ligado sempre tem quem o ligou.
	if cfg.AtualizadoPor == nil {
		slog.Error("agendador: configuração sem responsável, dry-run automático não criado")
		return marcar(disparoSemResponsavel)
	}

	ids, err := qtx.CotasAtivasIDs(ctx)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		slog.Warn("agendador: nenhuma cota ativa no cadastro, dry-run automático não criado")
		return marcar(disparoSemCotas)
	}

	exec, err := qtx.CriarExecucao(ctx, db.CriarExecucaoParams{
		Tipo: "dry_run", CriadaPor: *cfg.AtualizadoPor, Origem: "agendada",
	})
	if err != nil {
		return err
	}
	n, err := qtx.InserirCotasNaExecucao(ctx, db.InserirCotasNaExecucaoParams{ExecucaoID: exec.ID, CotaIds: ids})
	if err != nil {
		return err
	}
	if _, err := qtx.InserirEvento(ctx, db.InserirEventoParams{
		ExecucaoID: exec.ID, Nivel: "info", Dados: []byte("{}"),
		Mensagem: fmt.Sprintf("Dry-run automático criado pelo agendamento (dia %d às %02d:%02d) com %d cota(s) ativa(s). Nenhum lance será confirmado.",
			cfg.DryRunDia, cfg.DryRunHora, cfg.DryRunMinuto, n),
	}); err != nil {
		return err
	}
	if err := qtx.RegistrarAuditoria(ctx, paramsAuditoria(nil, cfg.AtualizadoPor, "execucao_criada", "execucao", idTexto(exec.ID),
		map[string]any{"tipo": exec.Tipo, "cotas": n, "origem": "agendada", "ocorrencia": devida.Format(time.RFC3339)})); err != nil {
		return err
	}
	if err := marcar(disparoCriada); err != nil {
		return err
	}
	slog.Info("agendador: dry-run automático na fila", "execucao", exec.ID, "cotas", n,
		"ocorrencia", devida.Format(time.RFC3339))
	return nil
}

// avisarExecucoesAgendadas manda o resumo das execuções agendadas que terminaram. O aviso é
// marcado mesmo com o e-mail desligado: ligar o aviso depois não traz de volta execução antiga.
func (s *Servidor) avisarExecucoesAgendadas(ctx context.Context) {
	pendentes, err := s.q.ExecucoesAgendadasSemAviso(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("agendador: falha ao listar execuções agendadas sem aviso", "erro", err)
		}
		return
	}
	if len(pendentes) == 0 {
		return
	}
	cfg := s.configCanopus(ctx)
	enviar := cfg.AvisoEmail && s.cfg.Vigia.Email != nil
	for _, e := range pendentes {
		if enviar {
			ctxEnvio, cancel := context.WithTimeout(ctx, 45*time.Second)
			err := s.cfg.Vigia.Email.Enviar(ctxEnvio, s.resumoExecucaoAgendada(e))
			cancel()
			if err != nil {
				// Tenta de novo no próximo ciclo: a execução fica sem a marca.
				slog.Error("agendador: resumo do dry-run automático não foi enviado", "execucao", e.ID, "erro", err)
				return
			}
		}
		if err := s.q.MarcarAvisoEnviado(ctx, e.ID); err != nil {
			slog.Error("agendador: falha ao marcar o aviso do dry-run automático", "execucao", e.ID, "erro", err)
			return
		}
	}
}

// resumoExecucaoAgendada: só números e o link da execução, nunca nome de cliente.
func (s *Servidor) resumoExecucaoAgendada(e db.ExecucoesAgendadasSemAvisoRow) alerta.Mensagem {
	situacao := map[string]string{
		"concluida":           "terminou sem erros",
		"concluida_com_erros": "terminou com erros",
		"cancelada":           "foi cancelado",
		"falhou":              "falhou",
	}[e.Status]
	if situacao == "" {
		situacao = e.Status
	}
	quando := ""
	if e.FinalizadaEm != nil {
		quando = e.FinalizadaEm.In(fusoCanopus).Format("02/01/2006 15:04")
	}
	corpo := fmt.Sprintf("O dry-run automático nº %d %s em %s.\n\n", e.ID, situacao, quando)
	corpo += fmt.Sprintf("Cotas: %d no total, %d verificadas, %d com erro.\n", e.Total, e.Sucesso, e.ComErro)
	if e.Erro != nil && *e.Erro != "" {
		corpo += "Erro da execução: " + *e.Erro + "\n"
	}
	corpo += fmt.Sprintf("\nDetalhes, screenshots e log: %s/canopus/execucoes/%d\n", s.cfg.AppOrigin, e.ID)
	corpo += "Nenhum lance foi confirmado: um dry-run só confere as telas do Newcon.\n"

	resumo := fmt.Sprintf("%d ok", e.Sucesso)
	if e.ComErro > 0 {
		resumo += fmt.Sprintf(", %d com erro", e.ComErro)
	}
	return alerta.Mensagem{
		Assunto: fmt.Sprintf("[Travus %s] Dry-run automático nº %d: %s", s.ambienteAlerta(), e.ID, resumo),
		Corpo:   corpo,
	}
}

// marcarOcorrenciaAnterior fecha a ocorrência que já havia passado quando o agendamento foi
// ligado (ou quando o dia/hora mudou): o dry-run do mês passado não roda retroativo e o vigia
// não reclama de algo que ninguém tinha agendado ainda. A ocorrência já atendida fica como está.
func (s *Servidor) marcarOcorrenciaAnterior(ctx context.Context, cfg db.BuscarConfiguracaoCanopusRow) error {
	devida := ultimaOcorrencia(s.agora(), int(cfg.DryRunDia), int(cfg.DryRunHora), int(cfg.DryRunMinuto))
	dia := diaDaOcorrencia(devida)
	if cfg.UltimoDisparoDia != nil && mesmoDia(*cfg.UltimoDisparoDia, dia) {
		return nil
	}
	resultado := disparoAntesDeLigar
	return s.q.MarcarDisparoDryRun(ctx, db.MarcarDisparoDryRunParams{
		UltimoDisparoDia: &dia, UltimoDisparoResultado: &resultado,
	})
}

// verificarAgendamento: a ocorrência marcada chegou e o dry-run não entrou na fila. O motivo
// fica gravado na configuração (atrasada, sem cota ativa, sem responsável); sem marca nenhuma,
// a API não estava de pé na hora.
func (s *Servidor) verificarAgendamento(ctx context.Context, agora time.Time) []achado {
	cfg, err := s.lerConfiguracao(ctx)
	if err != nil {
		slog.Error("vigia: consulta da configuração do Canopus", "erro", err)
		return nil
	}
	if !cfg.DryRunAutomatico {
		return nil
	}
	devida := ultimaOcorrencia(agora, int(cfg.DryRunDia), int(cfg.DryRunHora), int(cfg.DryRunMinuto))
	if agora.Sub(devida) < folgaAgendamento {
		return nil // acabou de chegar a hora: o laço do agendador tem tempo de rodar
	}
	marcada := cfg.UltimoDisparoDia != nil && mesmoDia(*cfg.UltimoDisparoDia, diaDaOcorrencia(devida))
	resultado := valorOuVazio(cfg.UltimoDisparoResultado)
	if marcada && (resultado == disparoCriada || resultado == disparoAntesDeLigar) {
		return nil
	}

	quando := devida.Format("02/01/2006 15:04")
	if resultado == disparoAntesDeLigar {
		return nil
	}
	detalhe := map[string]string{
		disparoSemCotas:       "Nenhuma cota ativa no cadastro: não havia o que conferir. Ative as cotas em " + s.cfg.AppOrigin + "/canopus/cotas",
		disparoSemResponsavel: "A configuração do agendamento está sem a pessoa que a alterou (escrita direta no banco?). Salve a configuração de novo em " + s.cfg.AppOrigin + "/canopus/configuracao",
		disparoAtrasada:       "A API não estava de pé na hora marcada e o atraso passou de " + textoTempo(toleranciaAgendamento) + ": o dry-run não foi criado atrasado. Crie um dry-run à mão se ainda dá tempo da assembleia.",
	}[resultado]
	if !marcada {
		detalhe = "A API não registrou nenhuma tentativa nesse horário (estava fora do ar?). Crie um dry-run à mão se ainda dá tempo da assembleia."
	}
	return []achado{{
		Chave:   "agendamento:" + devida.Format("2006-01-02"),
		Titulo:  "O dry-run automático de " + quando + " não rodou",
		Detalhe: detalhe,
		Evento:  true,
	}}
}
