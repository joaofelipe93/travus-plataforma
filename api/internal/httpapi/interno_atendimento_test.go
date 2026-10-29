package httpapi

// Etapa D: a fila que o worker consome. O que está guardado aqui é o que impede dois erros
// caros: resposta saindo de bot que alguém desligou, e resposta entregue duas vezes.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// filaDoWorker pede trabalho como o worker: diz quais bots tem conectados.
func filaDoWorker(t *testing.T, s *Servidor, bots []string) []struct {
	ID         int64  `json:"id"`
	ConversaID int64  `json:"conversa_id"`
	Bot        string `json:"bot"`
	Telefone   string `json:"telefone"`
	Texto      string `json:"texto"`
	Tentativa  int32  `json:"tentativa"`
} {
	t.Helper()
	corpo, _ := json.Marshal(map[string]any{"bots": bots, "limite": 10})
	r := httptest.NewRequest(http.MethodPost, "/internal/atendimento/fila/proxima", strings.NewReader(string(corpo)))
	r.Header.Set("Authorization", "Bearer "+tokenWorkerTeste)
	r.Header.Set("X-Worker", "atendimento-de-teste")
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.RotasInternas().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("pedir fila: %d %s", rec.Code, rec.Body)
	}
	var resposta struct {
		Mensagens []struct {
			ID         int64  `json:"id"`
			ConversaID int64  `json:"conversa_id"`
			Bot        string `json:"bot"`
			Telefone   string `json:"telefone"`
			Texto      string `json:"texto"`
			Tentativa  int32  `json:"tentativa"`
		} `json:"mensagens"`
	}
	decodificarEm(t, rec, &resposta)
	return resposta.Mensagens
}

func avisarWorker(t *testing.T, s *Servidor, caminho string, corpo any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(corpo)
	r := httptest.NewRequest(http.MethodPost, caminho, strings.NewReader(string(b)))
	r.Header.Set("Authorization", "Bearer "+tokenWorkerTeste)
	r.Header.Set("X-Worker", "atendimento-de-teste")
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.RotasInternas().ServeHTTP(rec, r)
	return rec
}

func statusDaMensagem(t *testing.T, s *Servidor, id int64) (string, int32, *string) {
	t.Helper()
	var status string
	var tentativas int32
	var motivo *string
	if err := s.pool.QueryRow(context.Background(),
		"SELECT status, tentativas, motivo FROM atendimento.mensagens WHERE id = $1", id).
		Scan(&status, &tentativas, &motivo); err != nil {
		t.Fatal(err)
	}
	return status, tentativas, motivo
}

func TestAtendimentoFilaEntregaEregistra(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi! Em qual bairro?"}}}
	s := servidorComBot(t, m)
	bot := botComFluxo(t, s, "fila", true)
	res, err := s.responderAtendimento(context.Background(), bot, "5511900060001", "Ana", "oi")
	if err != nil {
		t.Fatal(err)
	}
	if res.MensagemID == nil {
		t.Fatal("a resposta devia ter entrado na fila")
	}

	fila := filaDoWorker(t, s, []string{bot.Slug})
	if len(fila) != 1 {
		t.Fatalf("o worker devia levar uma mensagem: %+v", fila)
	}
	if fila[0].Bot != bot.Slug || fila[0].Telefone != "5511900060001" || fila[0].Texto == "" {
		t.Errorf("a mensagem veio incompleta: %+v", fila[0])
	}
	if fila[0].Tentativa != 1 {
		t.Errorf("a primeira entrega é a tentativa 1: %d", fila[0].Tentativa)
	}

	// Pedir de novo não entrega a mesma: a trava de 2 minutos segura.
	if novamente := filaDoWorker(t, s, []string{bot.Slug}); len(novamente) != 0 {
		t.Errorf("a mesma mensagem não podia sair duas vezes: %+v", novamente)
	}

	rec := avisarWorker(t, s, "/internal/atendimento/mensagens/"+idTexto(*res.MensagemID)+"/enviada", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("registrar envio: %d %s", rec.Code, rec.Body)
	}
	if status, _, _ := statusDaMensagem(t, s, *res.MensagemID); status != "enviada" {
		t.Errorf("devia ficar enviada: %q", status)
	}
	// Registrar duas vezes não é erro (e não pode fazer o worker reenviar).
	if rec := avisarWorker(t, s, "/internal/atendimento/mensagens/"+idTexto(*res.MensagemID)+"/enviada", map[string]any{}); rec.Code != http.StatusOK {
		t.Errorf("registro repetido devia ser aceito em silêncio, veio %d", rec.Code)
	}
}

// A decisão mais nova é a que vale: desligar o bot segura o que ele já tinha composto.
func TestAtendimentoFilaNaoEntregaDeBotDesligado(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi!"}}}
	s := servidorComBot(t, m)
	bot := botComFluxo(t, s, "desligado-depois", true)
	if _, err := s.responderAtendimento(context.Background(), bot, "5511900060002", "", "oi"); err != nil {
		t.Fatal(err)
	}

	s.exec(t, "UPDATE atendimento.bots SET ativo = false WHERE id = $1", bot.ID)
	if fila := filaDoWorker(t, s, []string{bot.Slug}); len(fila) != 0 {
		t.Errorf("bot desligado não devia entregar nada: %+v", fila)
	}

	// Ligar de volta destrava o que já estava composto: a decisão mais nova é a que vale.
	s.exec(t, "UPDATE atendimento.bots SET ativo = true WHERE id = $1", bot.ID)
	if fila := filaDoWorker(t, s, []string{bot.Slug}); len(fila) != 1 {
		t.Errorf("bot ligado devia entregar: %+v", fila)
	}
}

func TestAtendimentoFilaSoDeQuemTemSessao(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi!"}}}
	s := servidorComBot(t, m)
	bot := botComFluxo(t, s, "com-sessao", true)
	if _, err := s.responderAtendimento(context.Background(), bot, "5511900060003", "", "oi"); err != nil {
		t.Fatal(err)
	}

	// O worker não tem sessão deste bot: a mensagem fica na fila em vez de ser dada por entregue.
	if fila := filaDoWorker(t, s, []string{"outro-bot"}); len(fila) != 0 {
		t.Errorf("não devia entregar mensagem de bot sem sessão: %+v", fila)
	}
	// Sem sessão nenhuma também não é erro.
	if fila := filaDoWorker(t, s, []string{}); len(fila) != 0 {
		t.Errorf("sem sessão, lista vazia: %+v", fila)
	}
}

func TestAtendimentoFilaFalhaVoltaOuDesiste(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi!"}}}
	s := servidorComBot(t, m)
	bot := botComFluxo(t, s, "falhas", true)
	res, err := s.responderAtendimento(context.Background(), bot, "5511900060004", "", "oi")
	if err != nil {
		t.Fatal(err)
	}
	id := *res.MensagemID
	caminho := "/internal/atendimento/mensagens/" + idTexto(id) + "/falhou"

	// Falha passageira na tentativa 1: volta à fila.
	rec := avisarWorker(t, s, caminho, map[string]any{"erro": "timeout no envio", "tentativa": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("registrar falha: %d %s", rec.Code, rec.Body)
	}
	var r struct {
		Status string `json:"status"`
	}
	decodificarEm(t, rec, &r)
	if r.Status != "pendente" {
		t.Errorf("devia voltar à fila: %+v", r)
	}
	if status, _, _ := statusDaMensagem(t, s, id); status != "pendente" {
		t.Errorf("no banco devia continuar pendente: %q", status)
	}

	// Número sem WhatsApp: definitivo já na primeira tentativa.
	rec = avisarWorker(t, s, caminho, map[string]any{
		"erro": "o número 5511***04 não tem WhatsApp", "tentativa": 1, "definitivo": true,
	})
	decodificarEm(t, rec, &r)
	if r.Status != "falhou" {
		t.Fatalf("falha definitiva devia desistir na hora: %+v", r)
	}
	status, _, motivo := statusDaMensagem(t, s, id)
	if status != "falhou" {
		t.Errorf("devia ficar falhou: %q", status)
	}
	if motivo == nil || *motivo == "" {
		t.Error("o motivo precisa ficar gravado: é o que a tela mostra")
	}
	// Mensagem já resolvida não muda mais.
	rec = avisarWorker(t, s, caminho, map[string]any{"erro": "outra tentativa", "tentativa": 2})
	decodificarEm(t, rec, &r)
	if r.Status != "ja_resolvida" {
		t.Errorf("mensagem fora da fila devia responder ja_resolvida: %+v", r)
	}
}

func TestAtendimentoFilaDesisteNoTeto(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi!"}}}
	s := servidorComBot(t, m)
	bot := botComFluxo(t, s, "teto-tentativas", true)
	res, err := s.responderAtendimento(context.Background(), bot, "5511900060005", "", "oi")
	if err != nil {
		t.Fatal(err)
	}
	caminho := "/internal/atendimento/mensagens/" + idTexto(*res.MensagemID) + "/falhou"

	rec := avisarWorker(t, s, caminho, map[string]any{"erro": "falhou de novo", "tentativa": maxTentativasAtendimento})
	var r struct {
		Status string `json:"status"`
	}
	decodificarEm(t, rec, &r)
	if r.Status != "falhou" {
		t.Errorf("na última tentativa devia desistir: %+v", r)
	}
}

func TestAtendimentoFilaExigeToken(t *testing.T) {
	s := servidorComBot(t, &modeloRoteiroBot{})
	r := httptest.NewRequest(http.MethodPost, "/internal/atendimento/fila/proxima", strings.NewReader(`{"bots":["x"]}`))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.RotasInternas().ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("sem token devia dar 401, veio %d", rec.Code)
	}
}
