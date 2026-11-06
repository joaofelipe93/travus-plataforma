package httpapi

// Issue #128, item 3: o lead sobe para o Funil de Vendas (o CRM da Smart).
//
// Nenhum teste fala com o CRM: o cliente aponta para um servidor de mentira. O que está guardado
// aqui é o que protege a conta de vocês — nada sobe sem a integração ligada, nada sobe duas
// vezes, nada sobe retroativo, e falha não some.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
	"github.com/joaofelipe93/travus-plataforma/api/internal/funil"
)

// funilDeMentira devolve o servidor e um ponteiro para o que ele recebeu.
func funilDeMentira(t *testing.T, resposta func(w http.ResponseWriter)) (*httptest.Server, *[]string) {
	t.Helper()
	var corpos []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		corpos = append(corpos, string(b))
		resposta(w)
	}))
	t.Cleanup(srv.Close)
	return srv, &corpos
}

func ligarFunil(t *testing.T, s *Servidor, srv *httptest.Server) {
	t.Helper()
	s.cfg.FunilChave = "chave-de-teste"
	s.cfg.FunilCliente = &funil.Cliente{URL: srv.URL, Chave: "chave-de-teste", HTTP: srv.Client()}
	codigo := func(v int32) *int32 { return &v }
	// O TRUNCATE dos testes leva a linha única; em produção ela se recria sozinha na leitura.
	if _, err := s.q.GarantirConfiguracaoFunil(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.q.AtualizarConfiguracaoFunil(context.Background(), db.AtualizarConfiguracaoFunilParams{
		Habilitado:     true,
		CodigoVendedor: codigo(75661), CodigoFunil: codigo(24458), CodigoEtapa: codigo(112803),
		CodigoCanal: codigo(126717), CodigoSegmento: codigo(273730),
	}); err != nil {
		t.Fatal(err)
	}
}

// leadDeTeste roda uma conversa até o fim, que é o que cria o lead.
func leadDeTeste(t *testing.T, s *Servidor, slug, telefone string) int64 {
	t.Helper()
	bot := botComPerguntas(t, s, slug, true)
	res, err := s.responderAtendimento(context.Background(), bot, telefone, "Maria Souza",
		"somos a Acme Ltda, meu telefone é esse mesmo", "")
	if err != nil {
		t.Fatal(err)
	}
	leads, err := s.q.ListarLeads(context.Background(), db.ListarLeadsParams{Limite: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(leads) == 0 {
		t.Fatalf("a conversa devia ter virado lead (conversa %d)", res.ConversaID)
	}
	return leads[0].ID
}

func modeloQueColheEEncerra() *modeloRoteiroBot {
	return &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_resposta", entrada: `{"respostas":[
			{"campo":"nome_da_empresa","valor":"Acme Ltda"},
			{"campo":"melhor_telefone","valor":"11988887777"}]}`},
		{texto: "Obrigado! Um consultor fala com você.", ferramenta: "encerrar", entrada: `{"motivo":"concluido"}`},
	}}
}

func TestFunilEnviaOLeadQuandoLigado(t *testing.T) {
	s := servidorComBot(t, modeloQueColheEEncerra())
	srv, corpos := funilDeMentira(t, func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"Oportunidade cadastrada com sucesso"}`))
	})
	ligarFunil(t, s, srv)
	ctx := context.Background()

	id := leadDeTeste(t, s, "funil-ok", "5511900001111")
	if enviados := s.processarFilaFunil(ctx); enviados != 1 {
		t.Fatalf("devia ter enviado 1 lead, enviou %d", enviados)
	}
	if len(*corpos) != 1 {
		t.Fatalf("o CRM devia ter recebido um pedido: %+v", *corpos)
	}
	// O que chegou ao CRM é a oportunidade com empresa e contato.
	if !strings.Contains((*corpos)[0], "Acme Ltda") || !strings.Contains((*corpos)[0], "5511900001111") {
		t.Errorf("o pedido devia levar empresa e telefone: %s", (*corpos)[0])
	}

	lead, err := s.q.BuscarLead(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if lead.FunilStatus != "enviado" || lead.FunilEnviadoEm == nil {
		t.Fatalf("o lead devia ficar como enviado: %+v", lead.FunilStatus)
	}
	// E não sobe de novo: o CRM desta conta está com validação desligada, duplicata é real.
	if enviados := s.processarFilaFunil(ctx); enviados != 0 {
		t.Errorf("o mesmo lead não podia subir duas vezes")
	}
}

func TestFunilDesligadoNaoMandaNada(t *testing.T) {
	s := servidorComBot(t, modeloQueColheEEncerra())
	srv, corpos := funilDeMentira(t, func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{}`)) })
	// Cliente configurado, integração NÃO ligada.
	s.cfg.FunilChave = "chave-de-teste"
	s.cfg.FunilCliente = &funil.Cliente{URL: srv.URL, Chave: "chave-de-teste", HTTP: srv.Client()}

	id := leadDeTeste(t, s, "funil-off", "5511900002222")
	if enviados := s.processarFilaFunil(context.Background()); enviados != 0 {
		t.Error("com a integração desligada, nada pode subir")
	}
	if len(*corpos) != 0 {
		t.Errorf("o CRM não devia ter recebido nada: %+v", *corpos)
	}
	lead, err := s.q.BuscarLead(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if lead.FunilStatus != "nao_enviado" {
		t.Errorf("o lead devia ficar fora da fila: %q", lead.FunilStatus)
	}
}

// Ligar a integração não pode despejar no CRM o que ficou para trás.
func TestFunilNaoSobeRetroativo(t *testing.T) {
	s := servidorComBot(t, modeloQueColheEEncerra())
	srv, corpos := funilDeMentira(t, func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"message":"ok"}`)) })

	// Lead nasce com a integração desligada…
	leadDeTeste(t, s, "funil-antes", "5511900003333")
	// …e só depois alguém liga.
	ligarFunil(t, s, srv)

	if enviados := s.processarFilaFunil(context.Background()); enviados != 0 {
		t.Error("lead antigo não pode subir sozinho ao ligar a integração")
	}
	if len(*corpos) != 0 {
		t.Errorf("o CRM não devia ter recebido nada: %+v", *corpos)
	}
}

// Recusa do CRM não some: fica no lead, com o motivo, e a fila tenta de novo depois.
func TestFunilGuardaARecusaDoCrm(t *testing.T) {
	s := servidorComBot(t, modeloQueColheEEncerra())
	srv, _ := funilDeMentira(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Código do vendedor inválido"}`))
	})
	ligarFunil(t, s, srv)
	ctx := context.Background()

	id := leadDeTeste(t, s, "funil-recusa", "5511900004444")
	if enviados := s.processarFilaFunil(ctx); enviados != 0 {
		t.Fatal("a recusa não podia contar como envio")
	}
	lead, err := s.q.BuscarLead(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if lead.FunilStatus != "erro" || lead.FunilErro == nil {
		t.Fatalf("a recusa devia ficar registrada: %+v %+v", lead.FunilStatus, lead.FunilErro)
	}
	if !strings.Contains(*lead.FunilErro, "Código do vendedor inválido") {
		t.Errorf("o motivo do CRM devia aparecer: %q", *lead.FunilErro)
	}
	if lead.FunilTentativas != 1 || lead.FunilProximaEm == nil {
		t.Errorf("devia ter marcado a tentativa e a próxima: %d %+v", lead.FunilTentativas, lead.FunilProximaEm)
	}
	// A chave não pode aparecer em lugar nenhum do que ficou guardado.
	if strings.Contains(*lead.FunilErro, "chave-de-teste") {
		t.Error("a chave de integração vazou no erro guardado")
	}
}

// A tela: só admin configura, e ligar sem os códigos é recusado com o motivo.
func TestFunilConfiguracaoSoAdminEExigeOsCodigos(t *testing.T) {
	s := novoServidorTeste(t)
	limparAtendimento(t, s)
	s.cfg.FunilChave = "chave-de-teste"

	operador := entrarComo(t, s, "operador@travus.test", db.PerfilUsuarioOperador)
	if rec := operador.enviarJSON(http.MethodGet, "/atendimento/integracao/funil", nil); rec.Code != http.StatusForbidden {
		t.Errorf("operador não devia ver a integração: %d", rec.Code)
	}

	admin := entrarComo(t, s, "admin@travus.test", db.PerfilUsuarioAdmin)
	rec := admin.enviarJSON(http.MethodPut, "/atendimento/integracao/funil",
		map[string]any{"habilitado": true, "codigo_vendedor": 75661})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ligar sem todos os códigos devia ser recusado: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "códigos") {
		t.Errorf("a recusa devia dizer o que falta: %s", rec.Body)
	}

	rec = admin.enviarJSON(http.MethodPut, "/atendimento/integracao/funil", map[string]any{
		"habilitado": true, "codigo_vendedor": 75661, "codigo_funil": 24458,
		"codigo_etapa": 112803, "codigo_canal": 126717, "codigo_segmento": 273730,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("com todos os códigos devia salvar: %d %s", rec.Code, rec.Body)
	}

	rec = admin.enviarJSON(http.MethodGet, "/atendimento/integracao/funil", nil)
	var vista struct {
		Habilitado    bool `json:"habilitado"`
		ChavePresente bool `json:"chave_presente"`
		Codigos       bool `json:"codigos_completos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &vista); err != nil {
		t.Fatal(err)
	}
	if !vista.Habilitado || !vista.ChavePresente || !vista.Codigos {
		t.Errorf("a tela devia ver a integração ligada e completa: %+v", vista)
	}
	// A chave em si nunca volta ao navegador.
	if strings.Contains(rec.Body.String(), "chave-de-teste") {
		t.Error("a chave de integração não pode voltar na resposta")
	}
}

// Sem a chave no servidor, ligar é recusado: senão todo lead giraria na fila sem poder subir.
func TestFunilNaoLigaSemAChaveNoServidor(t *testing.T) {
	s := novoServidorTeste(t)
	limparAtendimento(t, s)
	admin := entrarComo(t, s, "admin@travus.test", db.PerfilUsuarioAdmin)

	rec := admin.enviarJSON(http.MethodPut, "/atendimento/integracao/funil", map[string]any{
		"habilitado": true, "codigo_vendedor": 1, "codigo_funil": 2, "codigo_canal": 3, "codigo_segmento": 4,
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "FUNIL_CHAVE") {
		t.Errorf("devia recusar dizendo que falta a chave: %d %s", rec.Code, rec.Body)
	}
}
