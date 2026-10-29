package httpapi

// Etapa C: as rotas que as telas leem. O que está guardado aqui é o que a tela precisa poder
// confiar — e a régua de quem vê conversa, que é dado de gente de verdade.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Sessões dos testes das telas: uma por servidor e perfil, reusada entre as chamadas do mesmo
// teste (criar usuário a cada GET só gastaria argon2id).
var navegadoresDaTela = map[string]*navegador{}
var seqNavegadorDaTela int

// navegadorDoPerfil devolve uma sessão daquele perfil, criada uma vez por servidor e reusada
// entre as chamadas do mesmo teste (criar usuário a cada requisição só gastaria argon2id).
func navegadorDoPerfil(t *testing.T, s *Servidor, perfil db.PerfilUsuario) *navegador {
	t.Helper()
	chave := fmt.Sprintf("%p|%s", s, perfil)
	if nav, ok := navegadoresDaTela[chave]; ok {
		return nav
	}
	seqNavegadorDaTela++
	nav := entrarComo(t, s, fmt.Sprintf("tela%d-%s@teste", seqNavegadorDaTela, perfil), perfil)
	navegadoresDaTela[chave] = nav
	return nav
}

// pegar faz um GET autenticado com o perfil pedido.
func pegar(t *testing.T, s *Servidor, perfil db.PerfilUsuario, caminho string) *httptest.ResponseRecorder {
	t.Helper()
	return navegadorDoPerfil(t, s, perfil).req(http.MethodGet, caminho, nil, nil)
}

// decodificarEm lê o JSON da resposta no destino (o decodificar[T] da casa é por tipo; aqui a
// mesma struct é relida em várias chamadas).
func decodificarEm(t *testing.T, rec *httptest.ResponseRecorder, destino any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), destino); err != nil {
		t.Fatalf("resposta ilegível (%d): %v — %s", rec.Code, err, rec.Body)
	}
}

// conversaDeTeste faz uma conversa acontecer pelo caminho de verdade (o agente), para o teste
// das telas ler o que o serviço realmente grava, e não um fixture inventado.
func conversaDeTeste(t *testing.T, s *Servidor, bot db.AtendimentoBot, telefone, contato, texto string) resultadoAtendimento {
	t.Helper()
	res, err := s.responderAtendimento(context.Background(), bot, telefone, contato, texto)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAtendimentoResumoDoPainel(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_dado", entrada: `{"campo":"interesse","valor":"apartamento"}`},
		{texto: "Em qual bairro?"},
	}}
	s := servidorComBot(t, m)
	bot := botComFluxo(t, s, "resumo", true)
	conversaDeTeste(t, s, bot, "5511900010001", "Fulano", "quero um ap")

	rec := pegar(t, s, db.PerfilUsuarioLeitura, "/atendimento/resumo")
	if rec.Code != http.StatusOK {
		t.Fatalf("todo perfil vê o painel: %d %s", rec.Code, rec.Body)
	}
	var res struct {
		Bots struct {
			Total  int64 `json:"total"`
			Ativos int64 `json:"ativos"`
		} `json:"bots"`
		Conversas struct {
			Abertas int64 `json:"abertas"`
			Hoje    int64 `json:"hoje"`
		} `json:"conversas"`
		Fila struct {
			Pendentes     int64 `json:"pendentes"`
			Falharam      int64 `json:"falharam"`
			CanceladasMes int64 `json:"canceladas_mes"`
		} `json:"fila"`
		DadosColetadosMes int64 `json:"dados_coletados_mes"`
		TokensMes         int64 `json:"tokens_mes"`
		ModeloDisponivel  bool  `json:"modelo_disponivel"`
	}
	decodificarEm(t, rec, &res)

	if res.Bots.Total != 1 || res.Bots.Ativos != 1 {
		t.Errorf("os bots deviam estar contados: %+v", res.Bots)
	}
	if res.Conversas.Abertas != 1 || res.Conversas.Hoje != 1 {
		t.Errorf("a conversa de agora devia contar hoje e como aberta: %+v", res.Conversas)
	}
	if res.DadosColetadosMes != 1 {
		t.Errorf("o dado coletado devia contar no mês: %d", res.DadosColetadosMes)
	}
	if res.TokensMes == 0 {
		t.Error("o gasto da conversa devia aparecer: é o que mostra o custo do serviço")
	}
	if !res.ModeloDisponivel {
		t.Errorf("o dublê está ligado: o modelo devia constar como disponível: %+v", res)
	}
}

// Sem a chave da Anthropic nenhum bot responde, mesmo ligado: o painel precisa dizer isso, em
// vez de deixar alguém montar um fluxo que nunca vai rodar.
func TestAtendimentoResumoSemChaveDaAnthropic(t *testing.T) {
	s := servidorComBot(t, &modeloRoteiroBot{})
	var res struct {
		ModeloDisponivel bool `json:"modelo_disponivel"`
	}
	decodificarEm(t, pegar(t, s, db.PerfilUsuarioLeitura, "/atendimento/resumo"), &res)
	if !res.ModeloDisponivel {
		t.Error("o dublê está ligado: o modelo devia constar como disponível")
	}

	semChave := novoServidorTeste(t)
	limparAtendimento(t, semChave)
	semChave.cfg.ModeloAtendimento = nil
	decodificarEm(t, pegar(t, semChave, db.PerfilUsuarioLeitura, "/atendimento/resumo"), &res)
	if res.ModeloDisponivel {
		t.Error("sem ANTHROPIC_API_KEY o painel precisa avisar que nenhum bot responde")
	}
}

func TestAtendimentoListarConversas(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi! Como ajudo?"}, {texto: "Certo!"}}}
	s := servidorComBot(t, m)
	bot := botComFluxo(t, s, "lista", true)
	conversaDeTeste(t, s, bot, "5511900020001", "Ana", "bom dia")
	conversaDeTeste(t, s, bot, "5511900020002", "", "oi")

	rec := pegar(t, s, db.PerfilUsuarioOperador, "/atendimento/conversas")
	if rec.Code != http.StatusOK {
		t.Fatalf("operador devia listar: %d %s", rec.Code, rec.Body)
	}
	var res struct {
		Conversas []struct {
			ID            int64   `json:"id"`
			BotNome       string  `json:"bot_nome"`
			Telefone      string  `json:"telefone"`
			ContatoNome   *string `json:"contato_nome"`
			Status        string  `json:"status"`
			Mensagens     int64   `json:"mensagens"`
			UltimoTexto   string  `json:"ultimo_texto"`
			UltimaDirecao string  `json:"ultima_direcao"`
		} `json:"conversas"`
	}
	decodificarEm(t, rec, &res)
	if len(res.Conversas) != 2 {
		t.Fatalf("deviam vir as duas conversas: %+v", res.Conversas)
	}
	// A lista é a tela de quem vai triar: precisa do nome do bot, do telefone e da última fala.
	for _, c := range res.Conversas {
		if c.BotNome == "" || c.Telefone == "" || c.Mensagens == 0 {
			t.Errorf("faltou o essencial da linha: %+v", c)
		}
		if c.UltimoTexto == "" || c.UltimaDirecao == "" {
			t.Errorf("a última fala devia aparecer na lista: %+v", c)
		}
	}
	// Filtro por situação e por bot.
	rec = pegar(t, s, db.PerfilUsuarioOperador, "/atendimento/conversas?situacao=encerradas")
	decodificarEm(t, rec, &res)
	if len(res.Conversas) != 0 {
		t.Errorf("nenhuma encerrou ainda: %+v", res.Conversas)
	}
	rec = pegar(t, s, db.PerfilUsuarioOperador, "/atendimento/conversas?bot=999999")
	decodificarEm(t, rec, &res)
	if len(res.Conversas) != 0 {
		t.Errorf("bot que não existe devia dar lista vazia: %+v", res.Conversas)
	}
	if rec := pegar(t, s, db.PerfilUsuarioOperador, "/atendimento/conversas?situacao=qualquer"); rec.Code != http.StatusBadRequest {
		t.Errorf("situação inventada devia dar 400, veio %d", rec.Code)
	}
	if rec := pegar(t, s, db.PerfilUsuarioOperador, "/atendimento/conversas?limite=5000"); rec.Code != http.StatusBadRequest {
		t.Errorf("limite acima do teto devia dar 400, veio %d", rec.Code)
	}
}

// Conversa traz telefone e o que a pessoa escreveu: o perfil leitura não entra, como na tela de
// Reservas. Ele continua vendo o painel (contagem não é conteúdo).
func TestAtendimentoConversaNaoEDeLeitura(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi!"}}}
	s := servidorComBot(t, m)
	bot := botComFluxo(t, s, "privacidade", true)
	c := conversaDeTeste(t, s, bot, "5511900030001", "Bruno", "oi")

	for _, caminho := range []string{"/atendimento/conversas", "/atendimento/conversas/" + idTexto(c.ConversaID)} {
		if rec := pegar(t, s, db.PerfilUsuarioLeitura, caminho); rec.Code != http.StatusForbidden {
			t.Errorf("%s com perfil leitura devia dar 403, veio %d", caminho, rec.Code)
		}
	}
	if rec := pegar(t, s, db.PerfilUsuarioLeitura, "/atendimento/resumo"); rec.Code != http.StatusOK {
		t.Errorf("o painel continua sendo de todo perfil, veio %d", rec.Code)
	}
}

func TestAtendimentoConversaDetalhada(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_dado", entrada: `{"campo":"interesse","valor":"kitnet no centro"}`},
		{texto: "Anotado! Qual seu telefone?"},
	}}
	s := servidorComBot(t, m)
	bot := botComFluxo(t, s, "detalhe", true)
	c := conversaDeTeste(t, s, bot, "5511900040001", "Carla", "procuro uma kitnet no centro")

	rec := pegar(t, s, db.PerfilUsuarioAdmin, "/atendimento/conversas/"+idTexto(c.ConversaID))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin devia abrir a conversa: %d %s", rec.Code, rec.Body)
	}
	var res struct {
		Conversa struct {
			ID          int64   `json:"id"`
			BotNome     string  `json:"bot_nome"`
			Telefone    string  `json:"telefone"`
			ContatoNome *string `json:"contato_nome"`
			Status      string  `json:"status"`
			EtapaNome   *string `json:"etapa_nome"`
			EtapasDoBot int64   `json:"etapas_do_bot"`
		} `json:"conversa"`
		Mensagens []struct {
			Direcao string  `json:"direcao"`
			Texto   string  `json:"texto"`
			Status  string  `json:"status"`
			Motivo  *string `json:"motivo"`
		} `json:"mensagens"`
		Dados []struct {
			Campo     string  `json:"campo"`
			Valor     string  `json:"valor"`
			EtapaNome *string `json:"etapa_nome"`
		} `json:"dados"`
	}
	decodificarEm(t, rec, &res)

	if res.Conversa.Telefone != "5511900040001" || res.Conversa.EtapasDoBot != 2 {
		t.Errorf("o cabeçalho da conversa está errado: %+v", res.Conversa)
	}
	if res.Conversa.EtapaNome == nil || *res.Conversa.EtapaNome != "Interesse" {
		t.Errorf("a etapa atual devia vir pelo nome: %+v", res.Conversa.EtapaNome)
	}
	// As duas pontas da conversa, na ordem: o que a pessoa disse e o que o bot respondeu.
	if len(res.Mensagens) != 2 || res.Mensagens[0].Direcao != "recebida" || res.Mensagens[1].Direcao != "enviada" {
		t.Fatalf("a conversa devia ter recebida e enviada, nessa ordem: %+v", res.Mensagens)
	}
	if res.Mensagens[0].Texto != "procuro uma kitnet no centro" {
		t.Errorf("o texto de quem escreveu precisa aparecer igual: %q", res.Mensagens[0].Texto)
	}
	if len(res.Dados) != 1 || res.Dados[0].Campo != "interesse" || res.Dados[0].Valor != "kitnet no centro" {
		t.Errorf("o dado coletado devia vir com a etapa: %+v", res.Dados)
	}
	if res.Dados[0].EtapaNome == nil || *res.Dados[0].EtapaNome != "Interesse" {
		t.Errorf("a etapa em que o dado apareceu devia vir: %+v", res.Dados[0].EtapaNome)
	}
	if rec := pegar(t, s, db.PerfilUsuarioAdmin, "/atendimento/conversas/999999"); rec.Code != http.StatusNotFound {
		t.Errorf("conversa que não existe devia dar 404, veio %d", rec.Code)
	}
}

// A tela precisa mostrar o que o bot DIRIA quando a resposta não sai. Com um botão só, o motivo
// que sobra é o teto de respostas por hora — e ele continua aparecendo na conversa, com o texto.
func TestAtendimentoConversaMostraOQueNaoSaiu(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Primeira resposta."}, {texto: "Segunda resposta."}}}
	s := servidorComBot(t, m)
	bot := botComFluxo(t, s, "nao-saiu", true)
	s.exec(t, "UPDATE atendimento.bots SET max_mensagens_hora = 1 WHERE id = $1", bot.ID)
	bot, _ = s.q.BuscarBot(context.Background(), bot.ID)

	if _, err := s.responderAtendimento(context.Background(), bot, "5511900050001", "", "oi"); err != nil {
		t.Fatal(err)
	}
	segunda, err := s.responderAtendimento(context.Background(), bot, "5511900050002", "", "oi")
	if err != nil {
		t.Fatal(err)
	}
	if segunda.Status != "cancelada" {
		t.Fatalf("passado o teto da hora, a resposta nasce cancelada: %+v", segunda)
	}

	rec := pegar(t, s, db.PerfilUsuarioOperador, "/atendimento/conversas/"+idTexto(segunda.ConversaID))
	var res struct {
		Mensagens []struct {
			Direcao string  `json:"direcao"`
			Texto   string  `json:"texto"`
			Status  string  `json:"status"`
			Motivo  *string `json:"motivo"`
		} `json:"mensagens"`
	}
	decodificarEm(t, rec, &res)
	var cancelada bool
	for _, msg := range res.Mensagens {
		if msg.Direcao == "enviada" && msg.Status == "cancelada" {
			cancelada = true
			if msg.Texto == "" {
				t.Error("o texto do que sairia precisa aparecer na tela")
			}
			if msg.Motivo == nil || *msg.Motivo == "" {
				t.Error("a tela precisa do motivo para explicar por que não saiu")
			}
		}
	}
	if !cancelada {
		t.Errorf("a mensagem que não saiu devia aparecer na conversa: %+v", res.Mensagens)
	}
}
