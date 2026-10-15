package httpapi

// Rotina automática do Canopus. A cada minuto a API confere se chegou a ocorrência marcada na
// configuração (dia, hora e minuto, fuso de Brasília) e, se chegou, põe na fila um **dry-run**
// com todas as cotas ativas.
//
// Com o **lance real automático** ligado (decisão do usuário em 15/10/2026), quando esse dry-run
// termina a API também aprova o lance real sozinha, sem ninguém na tela — e aí os comprovantes
// vão para o Drive pela fila de sempre. Isso só acontece com as quatro travas de pé:
//   1. a chave LANCE_REAL_HABILITADO do servidor (API e worker);
//   2. o teto lance_real_max_cotas (cadastro errado não vira lance em massa);
//   3. a espera lance_real_espera_minutos entre o fim do dry-run e o lance, com aviso por
//      e-mail, para dar tempo de desligar ou cancelar;
//   4. cota que já tem lance na assembleia é pulada (o "registrar mesmo assim" exige gente).
//
// O que o automático NÃO muda: o worker continua gravando confirmacao_iniciada antes de clicar
// em Confirmar, o trigger do banco continua impedindo reprocessar cota confirmada e nada aqui
// repete o clique. Um dry-run origina no máximo uma execução real.
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
	"strings"
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

	// Origem da execução (coluna execucoes.origem, migração 00013).
	origemManual   = "manual"
	origemAgendada = "agendada"

	// Decisão do lance real automático sobre um dry-run agendado (migração 00014).
	autoCriada          = "criada"
	autoChaveDesligada  = "chave_desligada"
	autoAcimaDoLimite   = "acima_do_limite"
	autoSemCotaLiberada = "sem_cota_liberada"
	autoBloqueado       = "bloqueado"
	autoAntesDeLigar    = "antes_de_ligar"
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
	// O aviso vai antes: quem recebe o resumo do dry-run tem a espera inteira para desligar o
	// automático ou cancelar a execução.
	s.avisarExecucoesAgendadas(ctx)
	s.dispararLanceRealAgendado(ctx)
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
		Tipo: "dry_run", CriadaPor: *cfg.AtualizadoPor, Origem: origemAgendada,
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
			err := s.cfg.Vigia.Email.Enviar(ctxEnvio, s.resumoExecucaoAgendada(ctx, e))
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

// dispararLanceRealAgendado: dry-run automático que terminou e cuja espera já venceu. A decisão
// sobre cada dry-run fica gravada (execucoes.lance_real_auto_resultado): o laço roda a cada
// minuto e decide uma vez só, mesmo que a API reinicie.
func (s *Servidor) dispararLanceRealAgendado(ctx context.Context) {
	cfg := s.configCanopus(ctx)
	if !cfg.LanceRealAutomatico {
		return
	}
	// A espera (janela para ler o aviso e impedir o lance) é filtrada na consulta, com o
	// relógio do banco.
	candidatos, err := s.q.DryRunsAgendadosSemDecisao(ctx, int32(cfg.LanceRealEspera.Minutes()))
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("agendador: falha ao listar dry-runs automáticos sem decisão", "erro", err)
		}
		return
	}
	for _, c := range candidatos {
		if err := s.decidirLanceRealAgendado(ctx, c.ID, cfg); err != nil && ctx.Err() == nil {
			// Erro de banco: a decisão não foi gravada e o próximo ciclo tenta de novo.
			slog.Error("agendador: falha ao decidir o lance real automático", "execucao", c.ID, "erro", err)
		}
	}
}

// decidirLanceRealAgendado decide um dry-run: ou cria a execução real, ou fecha com o motivo.
// Decisão e execução vão na mesma transação — ou as duas, ou nenhuma.
func (s *Servidor) decidirLanceRealAgendado(ctx context.Context, dryRunID int64, cfg ConfigCanopus) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	// A decisão é gravada uma vez só (a consulta tem AND lance_real_auto_em IS NULL): se outra
	// instância da API decidiu antes, este ciclo não sobrescreve nada.
	marcar := func(resultado string) error {
		_, err := qtx.MarcarDecisaoLanceRealAutomatico(ctx, db.MarcarDecisaoLanceRealAutomaticoParams{
			ID: dryRunID, Resultado: &resultado,
		})
		return err
	}
	// fechar grava a decisão e o motivo no log da execução: o vigia avisa, e quem abrir a tela
	// do dry-run entende por que não saiu lance.
	fechar := func(resultado, motivo string) error {
		if err := marcar(resultado); err != nil {
			return err
		}
		if err := s.evento(ctx, qtx, dryRunID, nil, "aviso", motivo); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		slog.Warn("agendador: lance real automático não registrou lance",
			"execucao", dryRunID, "resultado", resultado, "motivo", motivo)
		return nil
	}

	// A chave do servidor é a última palavra.
	if !s.cfg.LanceRealHabilitado {
		return fechar(autoChaveDesligada, "Lance real automático ligado na configuração, mas a chave LANCE_REAL_HABILITADO do servidor está desligada: nenhum lance foi registrado.")
	}
	// Só por escrita direta no banco: o automático ligado sempre tem o admin que o ligou.
	if cfg.LanceRealAutomaticoPor == nil {
		return fechar(autoBloqueado, "Lance real automático sem o admin que o ligou (escrita direta no banco?): nenhum lance foi registrado.")
	}

	rv, err := s.montarRevisao(ctx, qtx, dryRunID)
	if err != nil {
		return err
	}
	if rv.Bloqueio != "" {
		return fechar(autoBloqueado, "Lance real automático não registrado: "+rv.Bloqueio+".")
	}

	// Cota com lance na assembleia é pulada: "registrar mesmo assim" exige gente na revisão.
	var escolhas []escolhaCotaReal
	var puladas []string
	for _, c := range rv.Cotas {
		if c.Bloqueio != "" || c.ExigeAutorizacao {
			puladas = append(puladas, c.Grupo+"-"+c.Cota+"-"+c.Versao)
			continue
		}
		escolhas = append(escolhas, escolhaCotaReal{ExecucaoCotaID: c.ExecucaoCotaID})
	}
	switch {
	case len(escolhas) == 0:
		return fechar(autoSemCotaLiberada, "Lance real automático: nenhuma cota ficou liberada neste dry-run (todas com erro ou já com lance na assembleia).")
	case len(escolhas) > cfg.LanceRealMaxCotas:
		return fechar(autoAcimaDoLimite, fmt.Sprintf(
			"Lance real automático não registrado: %d cota(s) liberadas passam do teto de %d. Confira o cadastro e aprove à mão se for o caso.",
			len(escolhas), cfg.LanceRealMaxCotas))
	}

	real, err := s.criarExecucaoRealDoDryRun(ctx, qtx, nil, dryRunID, escolhas, quemAprova{
		UsuarioID: *cfg.LanceRealAutomaticoPor, Nome: cfg.LanceRealAutomaticoPorNome, Origem: origemAgendada,
	})
	var conflito erroConflito
	var invalido erroValidacao
	switch {
	case errors.As(err, &conflito):
		return fechar(autoBloqueado, "Lance real automático não registrado: "+conflito.Error()+".")
	case errors.As(err, &invalido):
		return fechar(autoBloqueado, "Lance real automático não registrado: "+invalido.Error()+".")
	case errors.Is(err, errNaoEncontrado):
		return fechar(autoBloqueado, "Lance real automático não registrado: o dry-run não foi encontrado.")
	case err != nil:
		return err
	}
	if len(puladas) > 0 {
		if err := s.evento(ctx, qtx, real.ID, nil, "info", fmt.Sprintf(
			"Cotas puladas pelo automático (erro no dry-run ou lance já registrado nesta assembleia): %s.",
			strings.Join(puladas, ", "))); err != nil {
			return err
		}
	}
	if err := marcar(autoCriada); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	slog.Warn("agendador: LANCE REAL automático na fila", "execucao", real.ID, "dry_run", dryRunID,
		"cotas", len(escolhas), "puladas", len(puladas))
	return nil
}

// avisoLanceRealAutomatico: o parágrafo que entra no e-mail do dry-run automático quando o
// automático está ligado. É o aviso que a pessoa tem antes de o lance sair.
func (s *Servidor) avisoLanceRealAutomatico(ctx context.Context, dryRunID int64, finalizadaEm *time.Time) string {
	cfg := s.configCanopus(ctx)
	if !cfg.LanceRealAutomatico || finalizadaEm == nil {
		return ""
	}
	if !s.cfg.LanceRealHabilitado {
		return "\nO lance real automático está ligado na configuração, mas a chave LANCE_REAL_HABILITADO do servidor está desligada: nenhum lance será registrado.\n"
	}
	rv, err := s.montarRevisao(ctx, s.q, dryRunID)
	if err != nil {
		return ""
	}
	if rv.Bloqueio != "" {
		return "\nO lance real automático está ligado, mas a revisão deste dry-run não permite aprovar: " + rv.Bloqueio + ". Nenhum lance será registrado.\n"
	}
	liberadas := 0
	for _, c := range rv.Cotas {
		if c.Bloqueio == "" && !c.ExigeAutorizacao {
			liberadas++
		}
	}
	switch {
	case liberadas == 0:
		return "\nO lance real automático está ligado, mas nenhuma cota ficou liberada neste dry-run: nenhum lance será registrado.\n"
	case liberadas > cfg.LanceRealMaxCotas:
		return fmt.Sprintf("\nATENÇÃO: o lance real automático está ligado e %d cota(s) ficaram liberadas, acima do teto de %d: nenhum lance será registrado. Confira o cadastro e aprove à mão se for o caso.\n",
			liberadas, cfg.LanceRealMaxCotas)
	}
	return fmt.Sprintf("\nATENÇÃO: o lance real automático está ligado. A partir de %s a plataforma vai registrar LANCE DE VERDADE em %d cota(s), sem ninguém aprovar.\nPara impedir: desligue em %s/canopus/configuracao, ou cancele a execução real quando ela aparecer em %s/canopus/execucoes\n",
		finalizadaEm.Add(cfg.LanceRealEspera).In(fusoCanopus).Format("02/01/2006 15:04"), liberadas,
		s.cfg.AppOrigin, s.cfg.AppOrigin)
}

// verificarLanceRealAutomatico: o automático está ligado e a última decisão não foi registrar
// lance. Vira alerta do vigia (e-mail só quando muda).
func (s *Servidor) verificarLanceRealAutomatico(ctx context.Context) []achado {
	cfg := s.configCanopus(ctx)
	if !cfg.LanceRealAutomatico {
		return nil
	}
	ultima, err := s.q.UltimaDecisaoLanceRealAutomatico(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		slog.Error("vigia: consulta da última decisão do lance real automático", "erro", err)
		return nil
	}
	resultado := valorOuVazio(ultima.LanceRealAutoResultado)
	if resultado == autoCriada || resultado == autoAntesDeLigar || resultado == "" {
		return nil
	}
	// Depois de uma semana a ocorrência é história: o alerta (com lembrete a cada 12 h) pararia
	// de ser notícia e só incomodaria até o dry-run do mês seguinte.
	if ultima.LanceRealAutoEm != nil && s.agora().Sub(*ultima.LanceRealAutoEm) > 7*24*time.Hour {
		return nil
	}
	detalhe := map[string]string{
		autoChaveDesligada:  "A chave LANCE_REAL_HABILITADO do servidor está desligada: o automático não registra lance nenhum.",
		autoAcimaDoLimite:   "Mais cotas liberadas que o teto configurado: confira o cadastro e aprove à mão se ainda dá tempo da assembleia.",
		autoSemCotaLiberada: "Nenhuma cota ficou liberada no dry-run (todas com erro ou já com lance na assembleia).",
		autoBloqueado:       "A revisão não permitiu aprovar: o dry-run foi cancelado ou falhou, passou do prazo, já tinha sido aprovado, ou o automático está sem o admin que o ligou.",
	}[resultado]
	return []achado{{
		Chave:   fmt.Sprintf("lance-real-automatico:%d", ultima.ID),
		Titulo:  fmt.Sprintf("O lance real automático do dry-run nº %d não registrou lance", ultima.ID),
		Detalhe: detalhe + " Log da execução: " + s.cfg.AppOrigin + "/canopus/execucoes/" + idTexto(ultima.ID),
	}}
}

// resumoExecucaoAgendada: só números e o link da execução, nunca nome de cliente. Serve o
// dry-run automático e a execução real que ele originou (e.Tipo diz qual).
func (s *Servidor) resumoExecucaoAgendada(ctx context.Context, e db.ExecucoesAgendadasSemAvisoRow) alerta.Mensagem {
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

	if e.Tipo == "real" {
		corpo := fmt.Sprintf("O lance real automático nº %d %s em %s.\n\n", e.ID, situacao, quando)
		corpo += fmt.Sprintf("Cotas: %d no total, %d com lance registrado, %d com erro.\n", e.Total, e.Sucesso, e.ComErro)
		if e.Erro != nil && *e.Erro != "" {
			corpo += "Erro da execução: " + *e.Erro + "\n"
		}
		if e.AposConfirmar > 0 {
			corpo += fmt.Sprintf("\nATENÇÃO: %d cota(s) pararam depois do clique em Confirmar: o lance pode ter sido registrado no Newcon. Confira no Histórico da cota antes de tentar de novo — a plataforma não repete esse passo.\n", e.AposConfirmar)
		}
		corpo += fmt.Sprintf("\nProtocolos, comprovantes e situação no Google Drive: %s/canopus/execucoes/%d\n", s.cfg.AppOrigin, e.ID)
		resumo := fmt.Sprintf("%d lance(s)", e.Sucesso)
		if e.ComErro > 0 {
			resumo += fmt.Sprintf(", %d com erro", e.ComErro)
		}
		return alerta.Mensagem{
			Assunto: fmt.Sprintf("[Travus %s] Lance real automático nº %d: %s", s.ambienteAlerta(), e.ID, resumo),
			Corpo:   corpo,
		}
	}

	corpo := fmt.Sprintf("O dry-run automático nº %d %s em %s.\n\n", e.ID, situacao, quando)
	corpo += fmt.Sprintf("Cotas: %d no total, %d verificadas, %d com erro.\n", e.Total, e.Sucesso, e.ComErro)
	if e.Erro != nil && *e.Erro != "" {
		corpo += "Erro da execução: " + *e.Erro + "\n"
	}
	corpo += fmt.Sprintf("\nDetalhes, screenshots e log: %s/canopus/execucoes/%d\n", s.cfg.AppOrigin, e.ID)
	corpo += "Nenhum lance foi confirmado: um dry-run só confere as telas do Newcon.\n"
	corpo += s.avisoLanceRealAutomatico(ctx, e.ID, e.FinalizadaEm)

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
