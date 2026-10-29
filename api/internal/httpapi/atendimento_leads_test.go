package httpapi

// Etapa E: o lead. O que está guardado aqui é o que separa "o atendimento rendeu algo" de
// "alguém sujou o cadastro": o lead nasce sozinho, mas **virar cliente é decisão de gente**, o
// cliente nasce ativo em nenhum serviço, e lead decidido não se decide de novo.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/assistente"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// conversaQueRendeu roda uma conversa inteira que coleta dois dados e encerra, do jeito que
// acontece de verdade — o lead tem de sair disso, sem ninguém pedir.
func conversaQueRendeu(t *testing.T, s *Servidor, slug, telefone string) resultadoAtendimento {
	t.Helper()
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "registrar_dado", entrada: `{"campo":"interesse","valor":"apartamento 2 quartos"}`},
		{ferramenta: "avancar_etapa"},
		{ferramenta: "registrar_dado", entrada: `{"campo":"telefone","valor":"11988887777"}`},
		{ferramenta: "avancar_etapa"},
		{texto: "Obrigado! Um corretor vai te ligar."},
	}}
	s.cfg.ModeloAtendimento = func(string) assistente.Modelo { return m }
	bot := botComFluxo(t, s, slug, true)
	res, err := s.responderAtendimento(context.Background(), bot, telefone, "Fulano de Teste", "quero um ap de 2 quartos, meu zap é 11988887777")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Encerrada {
		t.Fatalf("a conversa devia encerrar: %+v", res)
	}
	return res
}

func leadsDoServidor(t *testing.T, s *Servidor, situacao string) []leadJSON {
	t.Helper()
	rec := pegar(t, s, db.PerfilUsuarioOperador, "/atendimento/leads?situacao="+situacao)
	if rec.Code != http.StatusOK {
		t.Fatalf("listar leads: %d %s", rec.Code, rec.Body)
	}
	var resposta struct {
		Leads []leadJSON `json:"leads"`
	}
	decodificarEm(t, rec, &resposta)
	return resposta.Leads
}

func postar(t *testing.T, s *Servidor, perfil db.PerfilUsuario, caminho string, corpo any) *httptest.ResponseRecorder {
	t.Helper()
	nav := navegadorDoPerfil(t, s, perfil)
	return nav.enviarJSON(http.MethodPost, caminho, corpo)
}

func TestAtendimentoLeadNasceDaConversaEncerrada(t *testing.T) {
	s := servidorComBot(t, &modeloRoteiroBot{}, true)
	res := conversaQueRendeu(t, s, "leads", "5511900070001")

	leads := leadsDoServidor(t, s, "novos")
	if len(leads) != 1 {
		t.Fatalf("a conversa encerrada devia ter virado lead: %+v", leads)
	}
	l := leads[0]
	if l.ConversaID != res.ConversaID || l.Status != "novo" {
		t.Errorf("lead errado: %+v", l)
	}
	// O telefone do lead é o do WhatsApp (de onde a pessoa escreveu), não o que ela ditou.
	if l.Telefone == nil || *l.Telefone != "5511900070001" {
		t.Errorf("o telefone devia ser o do WhatsApp: %v", l.Telefone)
	}
	// O outro telefone não se perde: entra como observação para a revisão julgar.
	if l.Observacoes == nil || *l.Observacoes == "" {
		t.Error("o telefone informado na conversa devia virar observação")
	}
	// Sem campo de nome coletado, o nome do WhatsApp serve de ponto de partida.
	if l.Nome == nil || *l.Nome != "Fulano de Teste" {
		t.Errorf("o nome do contato devia vir pré-preenchido: %v", l.Nome)
	}
	// Tudo o que o bot colheu fica no lead, inclusive o que não tem campo próprio.
	if l.Dados["interesse"] != "apartamento 2 quartos" {
		t.Errorf("os dados crus deviam estar no lead: %+v", l.Dados)
	}
}

// Conversa que não colheu nada não vira lead: senão a tela de leads seria a lista de toda
// conversa que passou.
func TestAtendimentoConversaSemDadoNaoViraLead(t *testing.T) {
	m := &modeloRoteiroBot{turnos: []turnoBot{
		{ferramenta: "encerrar", entrada: `{"motivo":"fora_do_escopo"}`},
		{texto: "Isso não é com a gente, desculpe."},
	}}
	s := servidorComBot(t, m, true)
	bot := botComFluxo(t, s, "sem-dado", true)
	if _, err := s.responderAtendimento(context.Background(), bot, "5511900070002", "", "vocês vendem carro?"); err != nil {
		t.Fatal(err)
	}
	if leads := leadsDoServidor(t, s, "todos"); len(leads) != 0 {
		t.Errorf("conversa sem dado coletado não devia virar lead: %+v", leads)
	}
}

func TestAtendimentoLeadViraCliente(t *testing.T) {
	s := servidorComBot(t, &modeloRoteiroBot{}, true)
	conversaQueRendeu(t, s, "vira-cliente", "5511900070003")
	lead := leadsDoServidor(t, s, "novos")[0]

	// A revisão corrige o que o modelo entendeu torto.
	nav := navegadorDoPerfil(t, s, db.PerfilUsuarioOperador)
	rec := nav.enviarJSON(http.MethodPatch, "/atendimento/leads/"+idTexto(lead.ID), map[string]any{
		"nome": "Maria da Silva", "telefone": "11988887777", "email": "maria@exemplo.com",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("corrigir o lead: %d %s", rec.Code, rec.Body)
	}

	rec = postar(t, s, db.PerfilUsuarioOperador, "/atendimento/leads/"+idTexto(lead.ID)+"/aproveitar", map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("aproveitar: %d %s", rec.Code, rec.Body)
	}
	var r struct {
		Lead leadJSON `json:"lead"`
	}
	decodificarEm(t, rec, &r)
	if r.Lead.Status != "aproveitado" || r.Lead.ClienteID == nil {
		t.Fatalf("o lead devia virar aproveitado com cliente: %+v", r.Lead)
	}
	// O cadastro guarda o nome como foi digitado (a forma maiúscula só serve para comparar).
	if r.Lead.ClienteNome == nil || *r.Lead.ClienteNome != "Maria da Silva" {
		t.Errorf("o cliente devia ter o nome da revisão: %v", valorOuVazio(r.Lead.ClienteNome))
	}
	if r.Lead.RevisadoPorNome == nil {
		t.Error("quem revisou devia ficar registrado")
	}

	// **O cliente nasce ativo em nenhum serviço**: um lead de atendimento não entra em dry-run
	// de lance nem em rodada de boletos por ter sido cadastrado.
	var ativos int
	if err := s.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM cliente_servicos WHERE cliente_id = $1 AND ativo", *r.Lead.ClienteID).
		Scan(&ativos); err != nil {
		t.Fatal(err)
	}
	if ativos != 0 {
		t.Errorf("o cliente do lead não podia nascer ativo em serviço nenhum: %d ativo(s)", ativos)
	}
	// E ele existe no cadastro de verdade, com o contato revisado.
	var telefone, email *string
	if err := s.pool.QueryRow(context.Background(),
		"SELECT telefone, email FROM clientes WHERE id = $1", *r.Lead.ClienteID).Scan(&telefone, &email); err != nil {
		t.Fatal(err)
	}
	if telefone == nil || *telefone != "11988887777" || email == nil || *email != "maria@exemplo.com" {
		t.Errorf("o cliente devia nascer com o contato da revisão: %v %v", telefone, email)
	}

	// Decidido não se decide de novo.
	if rec := postar(t, s, db.PerfilUsuarioOperador, "/atendimento/leads/"+idTexto(lead.ID)+"/aproveitar", map[string]any{}); rec.Code != http.StatusConflict {
		t.Errorf("aproveitar duas vezes devia dar 409, veio %d", rec.Code)
	}
	if rec := nav.enviarJSON(http.MethodPatch, "/atendimento/leads/"+idTexto(lead.ID), map[string]any{"nome": "Outro"}); rec.Code != http.StatusConflict {
		t.Errorf("editar lead decidido devia dar 409, veio %d", rec.Code)
	}
}

// O caso que evita cadastro duplicado: alguém da carteira escreveu no WhatsApp.
func TestAtendimentoLeadLigaAClienteExistente(t *testing.T) {
	s := servidorComBot(t, &modeloRoteiroBot{}, true)
	nav := navegadorDoPerfil(t, s, db.PerfilUsuarioOperador)
	criado := nav.cadastrarCliente(t, map[string]any{"nome": "Cliente Antigo"})

	conversaQueRendeu(t, s, "ja-cliente", "5511900070004")
	lead := leadsDoServidor(t, s, "novos")[0]

	rec := postar(t, s, db.PerfilUsuarioOperador, "/atendimento/leads/"+idTexto(lead.ID)+"/aproveitar",
		map[string]any{"cliente_id": criado.Cliente.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("ligar ao cliente existente: %d %s", rec.Code, rec.Body)
	}
	var r struct {
		Lead leadJSON `json:"lead"`
	}
	decodificarEm(t, rec, &r)
	if r.Lead.ClienteID == nil || *r.Lead.ClienteID != criado.Cliente.ID {
		t.Fatalf("o lead devia apontar para o cliente existente: %+v", r.Lead)
	}
	// Nenhum cliente novo foi criado.
	var quantos int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM clientes").Scan(&quantos); err != nil {
		t.Fatal(err)
	}
	if quantos != 1 {
		t.Errorf("não devia ter criado cliente novo: %d clientes", quantos)
	}
}

func TestAtendimentoLeadDescartado(t *testing.T) {
	s := servidorComBot(t, &modeloRoteiroBot{}, true)
	conversaQueRendeu(t, s, "descarte", "5511900070005")
	lead := leadsDoServidor(t, s, "novos")[0]

	// Descartar sem motivo é recusado: "por que isto não virou cliente?" é pergunta de depois.
	if rec := postar(t, s, db.PerfilUsuarioOperador, "/atendimento/leads/"+idTexto(lead.ID)+"/descartar", map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Errorf("descartar sem motivo devia dar 400, veio %d", rec.Code)
	}
	rec := postar(t, s, db.PerfilUsuarioOperador, "/atendimento/leads/"+idTexto(lead.ID)+"/descartar",
		map[string]any{"motivo": "não era do nosso ramo"})
	if rec.Code != http.StatusOK {
		t.Fatalf("descartar: %d %s", rec.Code, rec.Body)
	}
	var r struct {
		Lead leadJSON `json:"lead"`
	}
	decodificarEm(t, rec, &r)
	if r.Lead.Status != "descartado" || r.Lead.MotivoDescarte == nil {
		t.Fatalf("devia ficar descartado com motivo: %+v", r.Lead)
	}
	// A conversa e os dados continuam guardados: descartar o lead não apaga histórico.
	var mensagens int
	if err := s.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM atendimento.mensagens WHERE conversa_id = $1", r.Lead.ConversaID).Scan(&mensagens); err != nil {
		t.Fatal(err)
	}
	if mensagens == 0 {
		t.Error("as mensagens da conversa não podiam desaparecer")
	}
	if len(leadsDoServidor(t, s, "novos")) != 0 {
		t.Error("o lead descartado não devia continuar na lista de novos")
	}
	if len(leadsDoServidor(t, s, "descartados")) != 1 {
		t.Error("o lead devia aparecer entre os descartados")
	}
}

// Lead é dado pessoal de quem escreveu, e promovê-lo mexe no cadastro: leitura não entra.
func TestAtendimentoLeadNaoEDeLeitura(t *testing.T) {
	s := servidorComBot(t, &modeloRoteiroBot{}, true)
	conversaQueRendeu(t, s, "privacidade-lead", "5511900070006")
	lead := leadsDoServidor(t, s, "novos")[0]

	for _, caminho := range []string{"/atendimento/leads", "/atendimento/leads/" + idTexto(lead.ID)} {
		if rec := pegar(t, s, db.PerfilUsuarioLeitura, caminho); rec.Code != http.StatusForbidden {
			t.Errorf("%s com perfil leitura devia dar 403, veio %d", caminho, rec.Code)
		}
	}
	if rec := postar(t, s, db.PerfilUsuarioLeitura, "/atendimento/leads/"+idTexto(lead.ID)+"/aproveitar", map[string]any{}); rec.Code != http.StatusForbidden {
		t.Errorf("aproveitar com perfil leitura devia dar 403, veio %d", rec.Code)
	}
}

func TestAtendimentoLeadNoResumoDoPainel(t *testing.T) {
	s := servidorComBot(t, &modeloRoteiroBot{}, true)
	conversaQueRendeu(t, s, "resumo-lead", "5511900070007")

	rec := pegar(t, s, db.PerfilUsuarioLeitura, "/atendimento/resumo")
	var res struct {
		Leads struct {
			Novos        int64 `json:"novos"`
			Aproveitados int64 `json:"aproveitados"`
			NoMes        int64 `json:"no_mes"`
		} `json:"leads"`
	}
	decodificarEm(t, rec, &res)
	if res.Leads.Novos != 1 || res.Leads.NoMes != 1 {
		t.Errorf("o painel devia contar o lead novo: %+v", res.Leads)
	}
}

// A conversa aponta para o lead que ela originou: é o atalho de quem está lendo a conversa.
func TestAtendimentoConversaApontaParaOLead(t *testing.T) {
	s := servidorComBot(t, &modeloRoteiroBot{}, true)
	res := conversaQueRendeu(t, s, "aponta-lead", "5511900070008")

	rec := pegar(t, s, db.PerfilUsuarioOperador, "/atendimento/conversas/"+idTexto(res.ConversaID))
	var detalhe struct {
		Lead *struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
		} `json:"lead"`
	}
	decodificarEm(t, rec, &detalhe)
	if detalhe.Lead == nil || detalhe.Lead.Status != "novo" {
		t.Fatalf("a conversa devia apontar para o lead: %+v", detalhe.Lead)
	}

	// Conversa sem lead devolve null, não erro.
	m := &modeloRoteiroBot{turnos: []turnoBot{{texto: "Oi!"}}}
	s.cfg.ModeloAtendimento = func(string) assistente.Modelo { return m }
	bot := botComFluxo(t, s, "sem-lead", true)
	aberta, err := s.responderAtendimento(context.Background(), bot, "5511900070009", "", "oi")
	if err != nil {
		t.Fatal(err)
	}
	rec = pegar(t, s, db.PerfilUsuarioOperador, "/atendimento/conversas/"+idTexto(aberta.ConversaID))
	decodificarEm(t, rec, &detalhe)
	if detalhe.Lead != nil {
		t.Errorf("conversa aberta não tem lead ainda: %+v", detalhe.Lead)
	}
}
