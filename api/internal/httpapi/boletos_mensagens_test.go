package httpapi

// Etapa 3 do serviço de boletos: a fila de mensagens, a configuração e as rodadas automáticas.
// Precisam de Postgres (make test). **Nenhum teste manda mensagem**: não há WhatsApp aqui — o
// que se verifica é o que entra na fila e com que situação.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// rodadaDeEnvioComParcela: um envio concluído, com uma parcela e PDF, pelo caminho do worker.
// Devolve a rodada e o id da parcela.
func rodadaDeEnvioComParcela(t *testing.T, s *Servidor, n *navegador, interno http.Handler) (int64, int64) {
	t.Helper()
	wt := &workerTeste{t: t, h: interno, nome: "worker-boletos-teste"}
	id := n.criarRodada(t, "envio")
	esperarStatus(t, wt.post("/internal/boletos/rodadas/proxima", nil), http.StatusOK, "reservar envio")

	var parcelaID int64
	for {
		rec := wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/proximo-cliente", id), nil)
		if rec.Code == http.StatusNoContent {
			break
		}
		esperarStatus(t, rec, http.StatusOK, "cliente do envio")
		c := decodificar[clienteBoletoJSON](t, rec).Cliente
		status := "sem_boleto"
		// Só o primeiro cliente tem boleto em aberto: o segundo entra como sem_boleto.
		if parcelaID == 0 {
			rec = wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/parcelas", c.ID), map[string]any{
				"parcelas": []map[string]any{{
					"cota_id": c.Cotas[0].ID, "parcela": "018", "vencimento": "2026-11-16",
					"valor_centavos": 64462, "tipo": "overdue",
				}},
			})
			esperarStatus(t, rec, http.StatusCreated, "parcela")
			parcelaID = decodificar[struct{ IDs []int64 }](t, rec).IDs[0]
			esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/parcelas/%d/pdf", parcelaID), pdfFalso),
				http.StatusCreated, "PDF")
			status = "consultado"
		}
		esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/concluir", c.ID),
			map[string]any{"status": status}), http.StatusNoContent, "concluir cliente")
	}
	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/finalizar", id), nil), http.StatusOK, "finalizar envio")
	return id, parcelaID
}

func TestFilaDeEnvioEmEnsaio(t *testing.T) {
	s, n, interno := cenarioBoletos(t)
	// Chave do servidor desligada: é o estado normal até o usuário pedir.
	s.cfg.BoletosEnvioHabilitado = false

	id, _ := rodadaDeEnvioComParcela(t, s, n, interno)
	d := n.rodada(t, id)

	if len(d.Mensagens) != 1 {
		t.Fatalf("mensagens = %d, quer 1", len(d.Mensagens))
	}
	m := d.Mensagens[0]
	if m.Status != "cancelada" || m.UltimoErro == nil || !strings.Contains(*m.UltimoErro, "BOLETOS_ENVIO_HABILITADO") {
		t.Errorf("em ensaio a mensagem devia ficar cancelada com o motivo: %+v", m)
	}
	// O texto é montado de qualquer jeito: é assim que se confere o que sairia.
	for _, pedaco := range []string{"Oi Cliente,", "Grupo 006650", "Vencimento: 16/11/2026", "R$ 644,62"} {
		if !strings.Contains(m.Texto, pedaco) {
			t.Errorf("texto sem %q: %s", pedaco, m.Texto)
		}
	}
	if m.Tipo != "envio" || m.PdfID == nil {
		t.Errorf("mensagem de envio devia apontar para o PDF: %+v", m)
	}
	// Nada pendente: a fila do serviço de envio continua vazia.
	if anexoPendente(t, s) != 0 {
		t.Error("ensaio não devia deixar anexo na fila")
	}
}

func TestFilaDeEnvioComEnvioLigado(t *testing.T) {
	s, n, interno := cenarioBoletos(t)
	h := s.Rotas()
	s.cfg.BoletosEnvioHabilitado = true
	criarUsuario(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	admin, _ := entrar(t, h, "admin@exemplo.com", senhaTeste)
	ligarEnvioBoletos(t, admin)

	id, _ := rodadaDeEnvioComParcela(t, s, n, interno)
	d := n.rodada(t, id)
	if len(d.Mensagens) != 1 || d.Mensagens[0].Status != "pendente" {
		t.Fatalf("mensagens = %+v", d.Mensagens)
	}
	// O anexo viaja na fila (o serviço de envio não lê o schema public) e some ao ser enviado.
	if anexoPendente(t, s) != 1 {
		t.Error("a mensagem pendente devia levar o anexo")
	}
}

func TestCobrancaSoAvisaQuemRecebeuBoletoENaoPagou(t *testing.T) {
	s, n, interno := cenarioBoletos(t)
	h := s.Rotas()
	s.cfg.BoletosEnvioHabilitado = true
	criarUsuario(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	admin, _ := entrar(t, h, "admin@exemplo.com", senhaTeste)
	ligarEnvioBoletos(t, admin)

	_, parcelaEnviada := rodadaDeEnvioComParcela(t, s, n, interno)
	if parcelaEnviada == 0 {
		t.Fatal("o envio devia ter registrado uma parcela")
	}

	// Cobrança: o primeiro cliente continua com a MESMA parcela em aberto (não pagou) e o
	// segundo não tem nada. Só o primeiro recebe o follow-up.
	wt := &workerTeste{t: t, h: interno, nome: "worker-boletos-teste"}
	cobranca := n.criarRodada(t, "cobranca")
	esperarStatus(t, wt.post("/internal/boletos/rodadas/proxima", nil), http.StatusOK, "reservar cobrança")
	primeiro := true
	for {
		rec := wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/proximo-cliente", cobranca), nil)
		if rec.Code == http.StatusNoContent {
			break
		}
		esperarStatus(t, rec, http.StatusOK, "cliente da cobrança")
		c := decodificar[clienteBoletoJSON](t, rec).Cliente
		status := "sem_boleto"
		if primeiro {
			esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/parcelas", c.ID), map[string]any{
				"parcelas": []map[string]any{{
					"cota_id": c.Cotas[0].ID, "parcela": "018", "vencimento": "2026-11-16",
					"valor_centavos": 64462, "tipo": "overdue",
				}},
			}), http.StatusCreated, "parcela ainda aberta")
			status = "consultado"
			primeiro = false
		}
		esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/concluir", c.ID),
			map[string]any{"status": status}), http.StatusNoContent, "concluir cliente")
	}
	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/finalizar", cobranca), nil), http.StatusOK, "finalizar cobrança")

	d := n.rodada(t, cobranca)
	if len(d.Mensagens) != 1 {
		t.Fatalf("a cobrança devia ter 1 mensagem (uma por cliente), veio %d", len(d.Mensagens))
	}
	m := d.Mensagens[0]
	if m.Tipo != "cobranca" || m.Status != "pendente" || m.ParcelaID != nil || m.PdfID != nil {
		t.Errorf("mensagem de cobrança = %+v (é por cliente e sem anexo)", m)
	}
	// Texto da issue #48, literal.
	if !strings.Contains(m.Texto, "ainda está disponível para pagamento") ||
		!strings.Contains(m.Texto, "Caso já tenha pago, desconsidere essa mensagem") {
		t.Errorf("texto da cobrança fora do que a issue pede: %s", m.Texto)
	}
	if d.Cobranca == nil || d.Cobranca.Enviadas != 1 || d.Cobranca.Pagas != 0 {
		t.Errorf("resumo da cobrança = %+v, quer 1 enviada e 0 paga", d.Cobranca)
	}
}

func TestCobrancaIgnoraQuemPagou(t *testing.T) {
	s, n, interno := cenarioBoletos(t)
	h := s.Rotas()
	s.cfg.BoletosEnvioHabilitado = true
	criarUsuario(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	admin, _ := entrar(t, h, "admin@exemplo.com", senhaTeste)
	ligarEnvioBoletos(t, admin)

	rodadaDeEnvioComParcela(t, s, n, interno)

	// Cobrança em que NINGUÉM tem parcela em aberto: todo mundo pagou.
	wt := &workerTeste{t: t, h: interno, nome: "worker-boletos-teste"}
	cobranca := n.criarRodada(t, "cobranca")
	esperarStatus(t, wt.post("/internal/boletos/rodadas/proxima", nil), http.StatusOK, "reservar cobrança")
	for {
		rec := wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/proximo-cliente", cobranca), nil)
		if rec.Code == http.StatusNoContent {
			break
		}
		c := decodificar[clienteBoletoJSON](t, rec).Cliente
		esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/concluir", c.ID),
			map[string]any{"status": "sem_boleto"}), http.StatusNoContent, "concluir cliente")
	}
	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/finalizar", cobranca), nil), http.StatusOK, "finalizar cobrança")

	d := n.rodada(t, cobranca)
	if len(d.Mensagens) != 0 {
		t.Errorf("quem pagou não recebe mensagem: %+v", d.Mensagens)
	}
	if d.Cobranca == nil || d.Cobranca.Pagas != 1 {
		t.Errorf("resumo da cobrança = %+v, quer 1 paga", d.Cobranca)
	}
}

func TestClienteSemTelefoneNaoVaiParaFila(t *testing.T) {
	s, n, interno := cenarioBoletos(t)
	h := s.Rotas()
	s.cfg.BoletosEnvioHabilitado = true
	criarUsuario(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	admin, _ := entrar(t, h, "admin@exemplo.com", senhaTeste)
	ligarEnvioBoletos(t, admin)

	// O cliente do cenário tem telefone; aqui ele é apagado antes da rodada.
	s.exec(t, "UPDATE clientes SET telefone = NULL")

	id, _ := rodadaDeEnvioComParcela(t, s, n, interno)
	d := n.rodada(t, id)
	if len(d.Mensagens) != 0 {
		t.Errorf("sem telefone não dá para mandar: %+v", d.Mensagens)
	}
	if !temEvento(d.Eventos, "aviso", "não dá para mandar o boleto") {
		t.Errorf("devia avisar no log da rodada: %+v", d.Eventos)
	}
}

func TestConfiguracaoDeBoletos(t *testing.T) {
	s, n, _ := cenarioBoletos(t)
	h := s.Rotas()

	// Padrões da issue: dia 8 e dia 14, antecipando para o dia útil anterior, envio desligado.
	inicial := decodificar[respostaConfigBoletos](t, n.req(http.MethodGet, "/boletos/configuracao", nil, nil))
	c := inicial.Configuracao
	if c.DiaEnvio != 8 || c.DiaCobranca != 14 || !c.AnteciparDiaUtil || c.EnvioHabilitado || c.AgendamentoAtivo {
		t.Fatalf("configuração inicial = %+v", c)
	}
	if inicial.PodeLigarEnvio {
		t.Error("operador não liga o envio")
	}

	// PUT parcial é recusado.
	esperarStatus(t, n.enviarJSON(http.MethodPut, "/boletos/configuracao", map[string]any{"dia_envio": 9}),
		http.StatusBadRequest, "PUT parcial")

	completa := func(extras map[string]any) map[string]any {
		corpo := map[string]any{
			"agendamento_ativo": false, "dia_envio": 8, "hora_envio": 9,
			"dia_cobranca": 14, "hora_cobranca": 9, "antecipar_dia_util": true,
			"envio_habilitado": false, "pausa_envio_segundos": 20, "max_mensagens_rodada": 200,
		}
		for k, v := range extras {
			corpo[k] = v
		}
		return corpo
	}
	for nome, extras := range map[string]map[string]any{
		"dia fora do mês":    {"dia_envio": 0},
		"hora inválida":      {"hora_cobranca": 24},
		"pausa curta demais": {"pausa_envio_segundos": 1},
		"teto zerado":        {"max_mensagens_rodada": 0},
	} {
		esperarStatus(t, n.enviarJSON(http.MethodPut, "/boletos/configuracao", completa(extras)), http.StatusBadRequest, nome)
	}

	// Operador muda o agendamento...
	rec := n.enviarJSON(http.MethodPut, "/boletos/configuracao", completa(map[string]any{
		"agendamento_ativo": true, "dia_cobranca": 15,
	}))
	esperarStatus(t, rec, http.StatusOK, "operador muda o agendamento")
	salva := decodificar[respostaConfigBoletos](t, rec).Configuracao
	if !salva.AgendamentoAtivo || salva.DiaCobranca != 15 {
		t.Errorf("configuração salva = %+v", salva)
	}
	// ...mas não liga o envio no WhatsApp dos clientes.
	esperarStatus(t, n.enviarJSON(http.MethodPut, "/boletos/configuracao", completa(map[string]any{"envio_habilitado": true})),
		http.StatusForbidden, "operador liga o envio")

	// Admin liga, mas a chave do servidor manda.
	criarUsuario(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	admin, _ := entrar(t, h, "admin@exemplo.com", senhaTeste)
	s.cfg.BoletosEnvioHabilitado = false
	esperarStatus(t, admin.enviarJSON(http.MethodPut, "/boletos/configuracao", completa(map[string]any{"envio_habilitado": true})),
		http.StatusConflict, "chave do servidor desligada")
	s.cfg.BoletosEnvioHabilitado = true
	rec = admin.enviarJSON(http.MethodPut, "/boletos/configuracao", completa(map[string]any{"envio_habilitado": true}))
	esperarStatus(t, rec, http.StatusOK, "admin liga o envio")
	ligada := decodificar[respostaConfigBoletos](t, rec).Configuracao
	if !ligada.EnvioHabilitado || ligada.EnvioHabilitadoPorNome == "" {
		t.Errorf("quem ligou o envio devia ficar registrado: %+v", ligada)
	}
	if contarAuditoria(t, s, "boletos_configuracao_atualizada") == 0 {
		t.Error("mudança de configuração não auditada")
	}
}

// A próxima ocorrência mostrada na tela já vem antecipada, com o motivo.
func TestProximaOcorrenciaAntecipa(t *testing.T) {
	// 14/02/2026 é sábado: a cobrança de fevereiro cai em 13 (sexta).
	agora := emBrasilia(t, "01/02/2026 08:00")
	quando, motivo, ok := proximaOcorrencia(agora, 14, 9, true)
	if !ok || quando.Format(time.DateOnly) != "2026-02-13" {
		t.Fatalf("próxima cobrança = %s (ok=%v)", quando.Format(time.RFC3339), ok)
	}
	if !strings.Contains(motivo, "sábado") {
		t.Errorf("motivo = %q, devia citar o sábado", motivo)
	}
	// Sem antecipar, é dia 14 mesmo.
	semAntecipar, motivo, _ := proximaOcorrencia(agora, 14, 9, false)
	if semAntecipar.Format(time.DateOnly) != "2026-02-14" || motivo != "" {
		t.Errorf("sem antecipar = %s, motivo %q", semAntecipar.Format(time.DateOnly), motivo)
	}
}

func TestRodadasAgendadas(t *testing.T) {
	s, n, _ := cenarioBoletos(t)
	h := s.Rotas()
	criarUsuario(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	admin, _ := entrar(t, h, "admin@exemplo.com", senhaTeste)

	// Liga o agendamento no dia 8 às 9h (relógio antes da ocorrência de agosto/2026, que cai
	// num sábado e antecipa para 07/08).
	s.agora = func() time.Time { return emBrasilia(t, "01/08/2026 08:00") }
	esperarStatus(t, admin.enviarJSON(http.MethodPut, "/boletos/configuracao", map[string]any{
		"agendamento_ativo": true, "dia_envio": 8, "hora_envio": 9, "dia_cobranca": 14, "hora_cobranca": 9,
		"antecipar_dia_util": true, "envio_habilitado": false, "pausa_envio_segundos": 20, "max_mensagens_rodada": 200,
	}), http.StatusOK, "ligar o agendamento")

	// Antes da hora: nada.
	if err := s.dispararRodadasAgendadas(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rodadasAgendadas(t, s)) != 0 {
		t.Fatal("não era hora de rodar")
	}

	// 07/08/2026 (sexta) às 9h: a ocorrência do dia 8 (sábado) antecipada.
	s.agora = func() time.Time { return emBrasilia(t, "07/08/2026 09:00") }
	if err := s.dispararRodadasAgendadas(context.Background()); err != nil {
		t.Fatal(err)
	}
	criadas := rodadasAgendadas(t, s)
	if len(criadas) != 1 || criadas[0].tipo != "envio" {
		t.Fatalf("rodadas criadas = %+v, quer uma de envio", criadas)
	}
	// Rodar de novo no mesmo dia não cria outra.
	if err := s.dispararRodadasAgendadas(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rodadasAgendadas(t, s)) != 1 {
		t.Error("a ocorrência do mês dispara uma vez só")
	}

	// A cobrança do dia 14 não roda sem envio concluído no mês.
	s.agora = func() time.Time { return emBrasilia(t, "14/08/2026 09:00") }
	if err := s.dispararRodadasAgendadas(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rodadasAgendadas(t, s)) != 1 {
		t.Error("cobrança sem envio concluído não devia ser criada")
	}
	c := decodificar[respostaConfigBoletos](t, n.req(http.MethodGet, "/boletos/configuracao", nil, nil))
	_ = c // a configuração continua legível depois dos disparos
}

func TestAgendamentoNaoRodaAtrasado(t *testing.T) {
	s, _, _ := cenarioBoletos(t)
	h := s.Rotas()
	criarUsuario(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	admin, _ := entrar(t, h, "admin@exemplo.com", senhaTeste)

	s.agora = func() time.Time { return emBrasilia(t, "01/08/2026 08:00") }
	esperarStatus(t, admin.enviarJSON(http.MethodPut, "/boletos/configuracao", map[string]any{
		"agendamento_ativo": true, "dia_envio": 8, "hora_envio": 9, "dia_cobranca": 14, "hora_cobranca": 9,
		"antecipar_dia_util": true, "envio_habilitado": false, "pausa_envio_segundos": 20, "max_mensagens_rodada": 200,
	}), http.StatusOK, "ligar o agendamento")

	// Dois dias depois da ocorrência: a API estava fora do ar. Boleto atrasado não ajuda.
	s.agora = func() time.Time { return emBrasilia(t, "09/08/2026 09:00") }
	if err := s.dispararRodadasAgendadas(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rodadasAgendadas(t, s)) != 0 {
		t.Error("ocorrência atrasada não devia criar rodada")
	}
}

// ===== ajudantes =====

type respostaConfigBoletos struct {
	Configuracao            configBoletosJSON `json:"configuracao"`
	PodeEditar              bool              `json:"pode_editar"`
	PodeLigarEnvio          bool              `json:"pode_ligar_envio"`
	EnvioHabilitadoServidor bool              `json:"envio_habilitado_servidor"`
}

func ligarEnvioBoletos(t *testing.T, admin *navegador) {
	t.Helper()
	esperarStatus(t, admin.enviarJSON(http.MethodPut, "/boletos/configuracao", map[string]any{
		"agendamento_ativo": false, "dia_envio": 8, "hora_envio": 9, "dia_cobranca": 14, "hora_cobranca": 9,
		"antecipar_dia_util": true, "envio_habilitado": true, "pausa_envio_segundos": 20, "max_mensagens_rodada": 200,
	}), http.StatusOK, "ligar o envio")
}

func anexoPendente(t *testing.T, s *Servidor) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(),
		"SELECT count(*)::int FROM boletos.mensagens WHERE status = 'pendente' AND anexo_pdf IS NOT NULL").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type rodadaCriada struct {
	id   int64
	tipo string
}

func rodadasAgendadas(t *testing.T, s *Servidor) []rodadaCriada {
	t.Helper()
	rows, err := s.pool.Query(context.Background(),
		"SELECT id, tipo FROM boletos.rodadas WHERE origem = 'agendada' ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []rodadaCriada
	for rows.Next() {
		var r rodadaCriada
		if err := rows.Scan(&r.id, &r.tipo); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func temEvento(eventos []eventoBoletoJSON, nivel, pedaco string) bool {
	for _, e := range eventos {
		if e.Nivel == nivel && strings.Contains(e.Mensagem, pedaco) {
			return true
		}
	}
	return false
}
