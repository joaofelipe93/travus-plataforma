package httpapi

// Lance real automático (migração 00014): o dry-run automático termina e a API aprova o lance
// real sozinha. Nada aqui fala com o Newcon — o worker é simulado e o "lance" é só a execução
// entrando na fila. O que se testa é quem decide, quando, e com quais travas.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// ligarLanceRealAutomatico liga o agendamento e o lance real automático, como o admin faria.
func ligarLanceRealAutomatico(t *testing.T, s *Servidor, admin *navegador, teto, espera int) {
	t.Helper()
	s.agora = func() time.Time { return emBrasilia(t, "09/09/2026 12:00") }
	rec := admin.enviarJSON(http.MethodPut, "/configuracao/canopus", map[string]any{
		"dry_run_automatico": true, "dry_run_dia": 10, "dry_run_hora": 8, "dry_run_minuto": 0,
		"aviso_email": true, "validade_dry_run_minutos": 120, "retencao_screenshots_dias": 30,
		"lance_real_automatico": true, "lance_real_max_cotas": teto, "lance_real_espera_minutos": espera,
	})
	esperarStatus(t, rec, http.StatusOK, "ligar o lance real automático")
}

// dryRunAgendadoRevisado: o dry-run do mês criado pelo agendamento e processado pelo worker. Na
// ordem da execução: cota 1 liberada; cota 2 liberada mas já com lance na assembleia; as outras
// com erro conhecido.
func dryRunAgendadoRevisado(t *testing.T, s *Servidor, interno http.Handler) int64 {
	t.Helper()
	ctx := context.Background()
	s.cicloAgendador(ctx)
	ids := execucoesAgendadas(t, s)
	if len(ids) != 1 {
		t.Fatalf("execuções agendadas = %v, quer 1", ids)
	}
	id := ids[0]
	w := &workerTeste{t: t, h: interno, nome: "worker-agendado"}
	tarefa, codigo := w.proxima()
	if codigo != http.StatusOK || tarefa.Execucao.ID != id {
		t.Fatalf("dry-run agendado não saiu da fila: %d", codigo)
	}
	if len(tarefa.Cotas) < 3 {
		t.Fatalf("o cenário precisa de ao menos 3 cotas ativas, veio %d", len(tarefa.Cotas))
	}
	for i, ec := range tarefa.Cotas {
		esperarStatus(t, w.post(rotaCota(ec.ID, "iniciar"), nil), http.StatusNoContent, "iniciar")
		var corpo map[string]any
		switch i {
		case 0:
			corpo = map[string]any{"status": "verificada", "detalhes": detalhesDryRunTeste(0)}
		case 1:
			corpo = map[string]any{"status": "verificada", "detalhes": detalhesDryRunTeste(1)}
		default:
			corpo = map[string]any{"status": "erro_antes_confirmar", "erro_tipo": "conhecido", "erro": "grupo sem 2º Fixo"}
		}
		esperarStatus(t, w.post(rotaCota(ec.ID, "concluir"), corpo), http.StatusNoContent, "concluir cota")
	}
	esperarStatus(t, w.post(fmt.Sprintf("/internal/execucoes/%d/finalizar", id), map[string]any{}), http.StatusOK, "finalizar")
	return id
}

// vencerEspera envelhece o fim do dry-run no banco: a espera do automático é contada com o
// relógio do Postgres (now()), não com o relógio falso dos testes.
func vencerEspera(t *testing.T, s *Servidor, dryRun int64) {
	t.Helper()
	s.exec(t, "UPDATE execucoes SET finalizada_em = now() - interval '10 minutes' WHERE id = $1", dryRun)
}

func decisaoDoAutomatico(t *testing.T, s *Servidor, dryRun int64) string {
	t.Helper()
	var resultado *string
	if err := s.pool.QueryRow(context.Background(),
		"SELECT lance_real_auto_resultado FROM execucoes WHERE id = $1", dryRun).Scan(&resultado); err != nil {
		t.Fatal(err)
	}
	return valorOuVazio(resultado)
}

// Padrão: o lance real automático nasce desligado. O dry-run do mês termina e nada mais acontece.
func TestLanceRealAutomaticoNasceDesligado(t *testing.T) {
	s, operador, _, interno := cenarioExecucoes(t)
	s.cfg.LanceRealHabilitado = true
	ligarAgendamento(t, s, operador, 10, 8, 0)
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 08:05") }
	dryRun := dryRunAgendadoRevisado(t, s, interno)

	vencerEspera(t, s, dryRun)
	s.cicloAgendador(context.Background())
	if n := contarExecucoes(t, s, "real"); n != 0 {
		t.Errorf("execuções reais = %d, quer 0 com o automático desligado", n)
	}
	if d := decisaoDoAutomatico(t, s, dryRun); d != "" {
		t.Errorf("decisão = %q, quer nenhuma (o automático está desligado)", d)
	}
}

// Caminho feliz: vencida a espera, a execução real entra na fila com as cotas liberadas. A cota
// que já tem lance na assembleia fica de fora, e a decisão não se repete.
func TestLanceRealAutomaticoRegistraDepoisDaEspera(t *testing.T) {
	s, _, _, interno := cenarioExecucoes(t)
	admin := entrarComo(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	s.cfg.LanceRealHabilitado = true
	ligarLanceRealAutomatico(t, s, admin, 50, 5)
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 08:05") }
	dryRun := dryRunAgendadoRevisado(t, s, interno)
	ctx := context.Background()

	// Dentro da espera (o dry-run acabou agora): ainda nada.
	s.cicloAgendador(ctx)
	if n := contarExecucoes(t, s, "real"); n != 0 {
		t.Fatalf("execuções reais dentro da espera = %d, quer 0", n)
	}

	// Vencida a espera: registra.
	vencerEspera(t, s, dryRun)
	s.cicloAgendador(ctx)
	if n := contarExecucoes(t, s, "real"); n != 1 {
		t.Fatalf("execuções reais = %d, quer 1", n)
	}
	if d := decisaoDoAutomatico(t, s, dryRun); d != autoCriada {
		t.Errorf("decisão = %q, quer %q", d, autoCriada)
	}

	ids := execucoesAgendadas(t, s)
	if len(ids) != 2 {
		t.Fatalf("execuções agendadas = %v, quer o dry-run e a execução real", ids)
	}
	d := admin.detalheComLances(t, ids[1])
	if d.Execucao.Tipo != "real" || d.Execucao.Status != "na_fila" {
		t.Errorf("execução real = %+v", d.Execucao)
	}
	if d.Execucao.DryRunOrigemID == nil || *d.Execucao.DryRunOrigemID != dryRun {
		t.Errorf("a execução real devia apontar para o dry-run %d: %+v", dryRun, d.Execucao)
	}
	// Só a cota 1: a cota 2 já tinha lance na assembleia e as outras deram erro.
	if len(d.Cotas) != 1 {
		t.Fatalf("cotas da execução real = %d, quer 1 (a que já tem lance é pulada)", len(d.Cotas))
	}
	if d.Cotas[0].PermitirLanceExistente {
		t.Error("o automático nunca autoriza lance existente")
	}
	if contarAuditoria(t, s, "execucao_real_aprovada") != 1 {
		t.Error("o lance real automático não foi auditado")
	}

	// Ciclos seguintes não criam outra.
	s.cicloAgendador(ctx)
	s.cicloAgendador(ctx)
	if n := contarExecucoes(t, s, "real"); n != 1 {
		t.Errorf("execuções reais depois de mais ciclos = %d, quer 1", n)
	}
}

// A chave do servidor é a última palavra: desligada, não há lance e o vigia avisa.
func TestLanceRealAutomaticoRespeitaAChaveDoServidor(t *testing.T) {
	s, _, _, interno := cenarioExecucoes(t)
	admin := entrarComo(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	s.cfg.LanceRealHabilitado = true // para ligar pela tela
	ligarLanceRealAutomatico(t, s, admin, 50, 5)
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 08:05") }
	dryRun := dryRunAgendadoRevisado(t, s, interno)

	s.cfg.LanceRealHabilitado = false // o deploy subiu sem a chave
	vencerEspera(t, s, dryRun)
	s.cicloAgendador(context.Background())

	if n := contarExecucoes(t, s, "real"); n != 0 {
		t.Errorf("execuções reais sem a chave = %d, quer 0", n)
	}
	if d := decisaoDoAutomatico(t, s, dryRun); d != autoChaveDesligada {
		t.Errorf("decisão = %q, quer %q", d, autoChaveDesligada)
	}
	achados := s.verificarLanceRealAutomatico(context.Background())
	if len(achados) != 1 || !strings.Contains(achados[0].Detalhe, "LANCE_REAL_HABILITADO") {
		t.Errorf("o vigia devia avisar que a chave está desligada: %+v", achados)
	}
}

// Acima do teto de cotas: não registra nada e o vigia avisa.
func TestLanceRealAutomaticoAcimaDoTeto(t *testing.T) {
	s, _, _, interno := cenarioExecucoes(t)
	admin := entrarComo(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	s.cfg.LanceRealHabilitado = true
	ligarLanceRealAutomatico(t, s, admin, 1, 5)
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 08:05") }
	dryRun := dryRunAgendadoRevisado(t, s, interno)

	// Com o teto em 1, uma cota liberada passaria: libera também a que tinha lance.
	s.exec(t, `UPDATE execucao_cotas SET detalhes = jsonb_set(detalhes, '{lances_nesta_assembleia}', '0')
	           WHERE execucao_id = $1 AND status = 'verificada'`, dryRun)
	vencerEspera(t, s, dryRun)
	s.cicloAgendador(context.Background())

	if n := contarExecucoes(t, s, "real"); n != 0 {
		t.Errorf("execuções reais acima do teto = %d, quer 0", n)
	}
	if d := decisaoDoAutomatico(t, s, dryRun); d != autoAcimaDoLimite {
		t.Errorf("decisão = %q, quer %q", d, autoAcimaDoLimite)
	}
	if achados := s.verificarLanceRealAutomatico(context.Background()); len(achados) != 1 {
		t.Errorf("o vigia devia avisar do teto: %+v", achados)
	}
}

// Dry-run pedido na tela não vira lance automático: o automático é da rotina do mês.
func TestLanceRealAutomaticoIgnoraDryRunManual(t *testing.T) {
	s, operador, cotas, interno := cenarioExecucoes(t)
	admin := entrarComo(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	s.cfg.LanceRealHabilitado = true
	ligarLanceRealAutomatico(t, s, admin, 50, 30)
	dryRun, _ := dryRunRevisado(t, operador, interno, cotas)

	vencerEspera(t, s, dryRun)
	s.cicloAgendador(context.Background())
	if n := contarExecucoes(t, s, "real"); n != 0 {
		t.Errorf("execuções reais de um dry-run manual = %d, quer 0", n)
	}
	if d := decisaoDoAutomatico(t, s, dryRun); d != "" {
		t.Errorf("decisão num dry-run manual = %q, quer nenhuma", d)
	}
}

// Ligar o automático não faz a plataforma correr atrás de dry-run que já terminou.
func TestLigarLanceRealAutomaticoNaoPegaDryRunAntigo(t *testing.T) {
	s, operador, _, interno := cenarioExecucoes(t)
	admin := entrarComo(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	s.cfg.LanceRealHabilitado = true
	ligarAgendamento(t, s, operador, 10, 8, 0)
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 08:05") }
	dryRun := dryRunAgendadoRevisado(t, s, interno)

	ligarLanceRealAutomatico(t, s, admin, 50, 5) // só agora
	vencerEspera(t, s, dryRun)
	s.cicloAgendador(context.Background())

	if n := contarExecucoes(t, s, "real"); n != 0 {
		t.Errorf("execuções reais = %d: dry-run de antes de ligar não vira lance", n)
	}
	if d := decisaoDoAutomatico(t, s, dryRun); d != autoAntesDeLigar {
		t.Errorf("decisão = %q, quer %q", d, autoAntesDeLigar)
	}
}

// O e-mail do dry-run avisa, antes de o lance sair, o que vai acontecer e como impedir.
func TestAvisoDoDryRunFalaDoLanceRealAutomatico(t *testing.T) {
	s, _, _, interno := cenarioExecucoes(t)
	admin := entrarComo(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	email := &emailFalso{}
	s.cfg.Vigia.Email = email
	s.cfg.LanceRealHabilitado = true
	ligarLanceRealAutomatico(t, s, admin, 50, 5)
	s.agora = func() time.Time { return emBrasilia(t, "10/09/2026 08:05") }
	dryRunAgendadoRevisado(t, s, interno)

	s.cicloAgendador(context.Background())
	if len(email.mensagens) == 0 {
		t.Fatal("o resumo do dry-run não foi enviado")
	}
	corpo := email.mensagens[0].Corpo
	for _, trecho := range []string{"LANCE DE VERDADE", "1 cota(s)", "/lances/configuracao"} {
		if !strings.Contains(corpo, trecho) {
			t.Errorf("o aviso devia conter %q: %s", trecho, corpo)
		}
	}
}

// Só admin liga o lance real automático: o operador continua editando o resto da configuração.
func TestLanceRealAutomaticoSoAdminLiga(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, rec := entrar(t, h, "operador@exemplo.com", senhaTeste)
	esperarStatus(t, rec, http.StatusOK, "login")

	comAutomatico := map[string]any{
		"dry_run_automatico": true, "dry_run_dia": 10, "dry_run_hora": 8, "dry_run_minuto": 0,
		"aviso_email": true, "validade_dry_run_minutos": 120, "retencao_screenshots_dias": 30,
		"lance_real_automatico": true, "lance_real_max_cotas": 50, "lance_real_espera_minutos": 30,
	}
	esperarStatus(t, n.enviarJSON(http.MethodPut, "/configuracao/canopus", comAutomatico),
		http.StatusForbidden, "operador ligando o lance real automático")

	// Mexer no teto também é coisa de admin.
	outroTeto := map[string]any{
		"dry_run_automatico": true, "dry_run_dia": 10, "dry_run_hora": 8, "dry_run_minuto": 0,
		"aviso_email": true, "validade_dry_run_minutos": 120, "retencao_screenshots_dias": 30,
		"lance_real_automatico": false, "lance_real_max_cotas": 500, "lance_real_espera_minutos": 30,
	}
	esperarStatus(t, n.enviarJSON(http.MethodPut, "/configuracao/canopus", outroTeto),
		http.StatusForbidden, "operador afrouxando o teto")

	// O resto do formulário o operador salva.
	semMexer := map[string]any{
		"dry_run_automatico": true, "dry_run_dia": 12, "dry_run_hora": 9, "dry_run_minuto": 0,
		"aviso_email": true, "validade_dry_run_minutos": 120, "retencao_screenshots_dias": 30,
		"lance_real_automatico": false, "lance_real_max_cotas": 50, "lance_real_espera_minutos": 30,
	}
	esperarStatus(t, n.enviarJSON(http.MethodPut, "/configuracao/canopus", semMexer), http.StatusOK,
		"operador salvando o agendamento")

	emVigor := s.configCanopus(context.Background())
	if emVigor.LanceRealAutomatico || emVigor.LanceRealMaxCotas != 50 {
		t.Errorf("configuração do lance real mudou com perfil operador: %+v", emVigor)
	}
	if emVigor.DryRunDia != 12 {
		t.Errorf("o operador devia ter salvado o dia 12: %+v", emVigor)
	}

	// Admin liga, e a configuração passa a saber quem ligou.
	admin := entrarComo(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	esperarStatus(t, admin.enviarJSON(http.MethodPut, "/configuracao/canopus", comAutomatico), http.StatusOK,
		"admin ligando o lance real automático")
	ligado := s.configCanopus(context.Background())
	if !ligado.LanceRealAutomatico || ligado.LanceRealAutomaticoPor == nil || ligado.LanceRealAutomaticoPorNome == "" {
		t.Errorf("o automático ligado precisa guardar quem ligou: %+v", ligado)
	}
}
