package httpapi

// Dry-run automático: o cálculo da ocorrência é testado sem banco; o laço que cria a execução
// precisa de Postgres (make test). Nenhum teste fala com o Newcon: a execução só entra na fila.

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

func TestUltimaOcorrencia(t *testing.T) {
	casos := []struct {
		nome              string
		agora             string
		dia, hora, minuto int
		quer              string
	}{
		{"antes da hora, vale o mês anterior", "10/10/2026 07:59", 10, 8, 0, "10/09/2026 08:00"},
		{"na hora exata", "10/10/2026 08:00", 10, 8, 0, "10/10/2026 08:00"},
		{"depois da hora", "10/10/2026 09:30", 10, 8, 0, "10/10/2026 08:00"},
		{"começo do mês vale o anterior", "02/10/2026 09:00", 10, 8, 0, "10/09/2026 08:00"},
		{"virada de ano para trás", "05/01/2027 09:00", 10, 8, 0, "10/12/2026 08:00"},
		{"mês anterior mais curto", "05/03/2027 09:00", 31, 8, 0, "28/02/2027 08:00"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := ultimaOcorrencia(emBrasilia(t, c.agora), c.dia, c.hora, c.minuto)
			if quer := emBrasilia(t, c.quer); !got.Equal(quer) {
				t.Errorf("ultimaOcorrencia = %s, quer %s", got.Format(time.RFC3339), quer.Format(time.RFC3339))
			}
		})
	}
}

// ligarAgendamento liga o dry-run automático no dia/hora pedidos, como a tela faria. O relógio
// fica num momento antes da ocorrência de 10/09/2026, que os testes usam como a primeira a
// valer (ligar o agendamento fecha a ocorrência anterior como "antes de ligar").
func ligarAgendamento(t *testing.T, s *Servidor, n *navegador, dia, hora, minuto int) {
	t.Helper()
	s.agora = func() time.Time { return emBrasilia(t, "09/09/2026 12:00") }
	rec := n.enviarJSON(http.MethodPut, "/configuracao/canopus", map[string]any{
		"dry_run_automatico": true, "dry_run_dia": dia, "dry_run_hora": hora, "dry_run_minuto": minuto,
		"aviso_email": true, "validade_dry_run_minutos": 120, "retencao_screenshots_dias": 30,
	})
	esperarStatus(t, rec, http.StatusOK, "ligar o agendamento")
}

func configuracaoNoBanco(t *testing.T, s *Servidor) db.BuscarConfiguracaoCanopusRow {
	t.Helper()
	c, err := s.lerConfiguracao(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func execucoesAgendadas(t *testing.T, s *Servidor) []int64 {
	t.Helper()
	rows, err := s.pool.Query(context.Background(), "SELECT id FROM execucoes WHERE origem = 'agendada' ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestAgendamentoDesligadoNaoCriaNada(t *testing.T) {
	s, _, _, _ := cenarioExecucoes(t)
	s.cicloAgendador(context.Background())
	if ids := execucoesAgendadas(t, s); len(ids) != 0 {
		t.Fatalf("execuções agendadas = %v, quer nenhuma (o agendamento nasce desligado)", ids)
	}
	if r := configuracaoNoBanco(t, s).UltimoDisparoResultado; r != nil {
		t.Errorf("resultado do disparo = %q, quer nenhum", *r)
	}
}

func TestAgendamentoCriaDryRunUmaVezPorOcorrencia(t *testing.T) {
	s, n, cotas, _ := cenarioExecucoes(t)
	ctx := context.Background()
	ligarAgendamento(t, s, n, 10, 8, 0)
	// 10/09/2026 às 08:05: a ocorrência das 08:00 acabou de chegar. Data no passado: o relógio
	// injetado não pode passar da validade da sessão criada no login.
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 08:05") }

	s.cicloAgendador(ctx)
	ids := execucoesAgendadas(t, s)
	if len(ids) != 1 {
		t.Fatalf("execuções agendadas = %v, quer uma", ids)
	}
	// Roda de novo no mesmo minuto e meia hora depois: a ocorrência já foi atendida.
	s.cicloAgendador(ctx)
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 08:35") }
	s.cicloAgendador(ctx)
	if ids2 := execucoesAgendadas(t, s); len(ids2) != 1 {
		t.Fatalf("execuções agendadas depois de três ciclos = %v, quer uma", ids2)
	}

	cfg := configuracaoNoBanco(t, s)
	if valorOuVazio(cfg.UltimoDisparoResultado) != disparoCriada {
		t.Errorf("resultado do disparo = %v, quer %q", cfg.UltimoDisparoResultado, disparoCriada)
	}
	if cfg.UltimoDisparoDia == nil || cfg.UltimoDisparoDia.UTC().Format("2006-01-02") != "2026-09-10" {
		t.Errorf("dia do disparo = %v, quer 2026-09-10", cfg.UltimoDisparoDia)
	}

	// A execução é um dry-run na fila, com todas as cotas ativas e o nome de quem agendou.
	d := n.detalhe(t, ids[0])
	if d.Execucao.Tipo != "dry_run" || d.Execucao.Status != "na_fila" {
		t.Errorf("execução agendada = tipo %q, status %q; quer dry_run na fila", d.Execucao.Tipo, d.Execucao.Status)
	}
	if d.Execucao.Origem != "agendada" {
		t.Errorf("origem = %q, quer agendada", d.Execucao.Origem)
	}
	if len(d.Cotas) != len(cotas) {
		t.Errorf("cotas na execução = %d, quer %d (todas as ativas)", len(d.Cotas), len(cotas))
	}
	if d.Execucao.CriadaPorNome == "" {
		t.Error("a execução agendada devia ficar no nome de quem ligou o agendamento")
	}

	var mensagem string
	if err := s.pool.QueryRow(ctx,
		"SELECT mensagem FROM execucao_eventos WHERE execucao_id = $1 ORDER BY id LIMIT 1", ids[0]).Scan(&mensagem); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mensagem, "automático") || !strings.Contains(mensagem, "Nenhum lance será confirmado") {
		t.Errorf("evento do dry-run automático = %q", mensagem)
	}
}

func TestAgendamentoSemCotaAtivaNaoCria(t *testing.T) {
	s, n, _, _ := cenarioExecucoes(t)
	ctx := context.Background()
	s.exec(t, "UPDATE cotas SET ativa = false")
	ligarAgendamento(t, s, n, 10, 8, 0)
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 08:05") }

	s.cicloAgendador(ctx)
	if ids := execucoesAgendadas(t, s); len(ids) != 0 {
		t.Fatalf("execuções agendadas = %v, quer nenhuma (sem cota ativa)", ids)
	}
	if r := valorOuVazio(configuracaoNoBanco(t, s).UltimoDisparoResultado); r != disparoSemCotas {
		t.Errorf("resultado do disparo = %q, quer %q", r, disparoSemCotas)
	}
	// A ocorrência fica marcada: o laço não tenta de novo a cada minuto.
	s.cicloAgendador(ctx)
	if ids := execucoesAgendadas(t, s); len(ids) != 0 {
		t.Fatalf("execuções agendadas no segundo ciclo = %v", ids)
	}
	// O vigia avisa, passada a folga do laço.
	achados := s.verificarAgendamento(ctx, emBrasilia(t, "10/09/2026 09:00"))
	if len(achados) != 1 || !strings.Contains(achados[0].Titulo, "não rodou") {
		t.Fatalf("achados do vigia = %+v", achados)
	}
}

func TestAgendamentoPerdidoNaoRodaAtrasado(t *testing.T) {
	s, n, _, _ := cenarioExecucoes(t)
	ctx := context.Background()
	ligarAgendamento(t, s, n, 10, 8, 0)
	// A API só voltou no dia seguinte: muito depois da tolerância.
	s.agora = func() time.Time { return emBrasilia(t, "11/09/2026 09:00") }

	s.cicloAgendador(ctx)
	if ids := execucoesAgendadas(t, s); len(ids) != 0 {
		t.Fatalf("execuções agendadas = %v, quer nenhuma (ocorrência perdida)", ids)
	}
	if r := valorOuVazio(configuracaoNoBanco(t, s).UltimoDisparoResultado); r != disparoAtrasada {
		t.Errorf("resultado do disparo = %q, quer %q", r, disparoAtrasada)
	}
	achados := s.verificarAgendamento(ctx, s.agora())
	if len(achados) != 1 || !strings.Contains(achados[0].Detalhe, "não foi criado atrasado") {
		t.Fatalf("achados do vigia = %+v", achados)
	}
}

// Ligar o agendamento depois da hora do mês não roda o dry-run retroativo nem vira alerta: a
// ocorrência anterior fica fechada como "antes de ligar".
func TestLigarAgendamentoDepoisDaHoraNaoRodaRetroativo(t *testing.T) {
	s, n, _, _ := cenarioExecucoes(t)
	ctx := context.Background()
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 15:00") }
	rec := n.enviarJSON(http.MethodPut, "/configuracao/canopus", map[string]any{
		"dry_run_automatico": true, "dry_run_dia": 10, "dry_run_hora": 8, "dry_run_minuto": 0,
		"aviso_email": true, "validade_dry_run_minutos": 120, "retencao_screenshots_dias": 30,
	})
	esperarStatus(t, rec, http.StatusOK, "ligar o agendamento depois da hora")

	s.cicloAgendador(ctx)
	if ids := execucoesAgendadas(t, s); len(ids) != 0 {
		t.Fatalf("execuções agendadas = %v, quer nenhuma (a ocorrência já havia passado)", ids)
	}
	if r := valorOuVazio(configuracaoNoBanco(t, s).UltimoDisparoResultado); r != disparoAntesDeLigar {
		t.Errorf("resultado do disparo = %q, quer %q", r, disparoAntesDeLigar)
	}
	if achados := s.verificarAgendamento(ctx, s.agora()); len(achados) != 0 {
		t.Errorf("achados do vigia = %+v, quer nenhum (ninguém tinha agendado esse horário)", achados)
	}

	// A próxima ocorrência roda normalmente.
	s.agora = func() time.Time { return emBrasilia(t, "10/10/2026 08:05") }
	s.cicloAgendador(ctx)
	if ids := execucoesAgendadas(t, s); len(ids) != 1 {
		t.Fatalf("execuções agendadas no mês seguinte = %v, quer uma", ids)
	}
}

// Dentro da tolerância (deploy, banco fora por minutos), o dry-run ainda é criado.
func TestAgendamentoAtrasoCurtoAindaRoda(t *testing.T) {
	s, n, _, _ := cenarioExecucoes(t)
	ligarAgendamento(t, s, n, 10, 8, 0)
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 12:00") }

	s.cicloAgendador(context.Background())
	if ids := execucoesAgendadas(t, s); len(ids) != 1 {
		t.Fatalf("execuções agendadas = %v, quer uma (4 h de atraso cabe na tolerância)", ids)
	}
}

// Enquanto a hora não chega, o vigia não reclama.
func TestVigiaCaladoAntesDaHora(t *testing.T) {
	s, n, _, _ := cenarioExecucoes(t)
	ligarAgendamento(t, s, n, 10, 8, 0)
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 08:05") }
	if achados := s.verificarAgendamento(context.Background(), s.agora()); len(achados) != 0 {
		t.Fatalf("achados do vigia = %+v, quer nenhum (a folga ainda não passou)", achados)
	}
}

func TestResumoDoDryRunAutomaticoPorEmail(t *testing.T) {
	s, n, cotas, _ := cenarioExecucoes(t)
	ctx := context.Background()
	email := &emailFalso{}
	s.cfg.Vigia.Email = email
	ligarAgendamento(t, s, n, 10, 8, 0)
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 08:05") }

	s.cicloAgendador(ctx)
	ids := execucoesAgendadas(t, s)
	if len(ids) != 1 {
		t.Fatalf("execuções agendadas = %v", ids)
	}
	// Execução ainda em andamento: nenhum aviso.
	s.cicloAgendador(ctx)
	if len(email.mensagens) != 0 {
		t.Fatalf("e-mails antes de terminar = %d", len(email.mensagens))
	}

	// Terminou com uma cota verificada e uma com erro.
	d := n.detalhe(t, ids[0])
	s.exec(t, "UPDATE execucao_cotas SET status = 'verificada', finalizada_em = now() WHERE id = $1", d.Cotas[0].ID)
	if len(d.Cotas) > 1 {
		s.exec(t, "UPDATE execucao_cotas SET status = 'erro_antes_confirmar', erro = 'cota sem 2º Fixo' WHERE id = $1", d.Cotas[1].ID)
	}
	s.exec(t, "UPDATE execucoes SET status = 'concluida_com_erros', finalizada_em = now() WHERE id = $1", ids[0])

	s.cicloAgendador(ctx)
	if len(email.mensagens) != 1 {
		t.Fatalf("e-mails depois de terminar = %d, quer 1", len(email.mensagens))
	}
	m := email.mensagens[0]
	if !strings.Contains(m.Assunto, "Dry-run automático") {
		t.Errorf("assunto = %q", m.Assunto)
	}
	if !strings.Contains(m.Corpo, "Nenhum lance foi confirmado") || !strings.Contains(m.Corpo, "/canopus/execucoes/") {
		t.Errorf("corpo = %q", m.Corpo)
	}
	// Dados de cliente não saem no e-mail.
	for _, c := range cotas {
		if strings.Contains(m.Corpo, c.ClienteNome) {
			t.Errorf("o resumo não pode levar nome de cliente: %q", m.Corpo)
		}
	}
	// Não repete nos ciclos seguintes.
	s.cicloAgendador(ctx)
	s.cicloAgendador(ctx)
	if len(email.mensagens) != 1 {
		t.Errorf("e-mails depois de mais ciclos = %d, quer 1", len(email.mensagens))
	}
}

// Sem SMTP (ou com o aviso desligado) a execução é marcada como avisada: ligar o aviso depois
// não traz de volta execução antiga.
func TestResumoMarcadoSemEmail(t *testing.T) {
	s, n, _, _ := cenarioExecucoes(t)
	ctx := context.Background()
	ligarAgendamento(t, s, n, 10, 8, 0)
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 08:05") }
	s.cicloAgendador(ctx)
	ids := execucoesAgendadas(t, s)
	s.exec(t, "UPDATE execucoes SET status = 'concluida', finalizada_em = now() WHERE id = $1", ids[0])

	s.cicloAgendador(ctx)
	var avisado *time.Time
	if err := s.pool.QueryRow(ctx, "SELECT aviso_email_em FROM execucoes WHERE id = $1", ids[0]).Scan(&avisado); err != nil {
		t.Fatal(err)
	}
	if avisado == nil {
		t.Error("sem SMTP a execução devia ficar marcada como avisada")
	}
}

// A execução manual continua marcada como manual (a tela mostra a diferença).
func TestExecucaoManualTemOrigemManual(t *testing.T) {
	s, n, cotas, _ := cenarioExecucoes(t)
	id := n.criarDryRun(t, idsDe(cotas[:1]))
	if origem := n.detalhe(t, id).Execucao.Origem; origem != "manual" {
		t.Errorf("origem = %q, quer manual", origem)
	}
	if ids := execucoesAgendadas(t, s); len(ids) != 0 {
		t.Errorf("execuções agendadas = %v, quer nenhuma", ids)
	}
}
