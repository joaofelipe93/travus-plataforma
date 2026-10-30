package atendimento

import (
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	bons := []string{"vendas", "pre-venda", "bot-1", "atendimento-imoveis"}
	for _, s := range bons {
		if _, err := Slug(s); err != nil {
			t.Errorf("Slug(%q) devia passar: %v", s, err)
		}
	}
	// O slug é nome de pasta no volume e id em rota: espaço, acento, maiúscula e hífen na ponta
	// ficam de fora para ninguém descobrir isso num erro de sistema de arquivos.
	ruins := []string{"", "ab", "-vendas", "vendas-", "ven das", "vendás", "a/b", strings.Repeat("a", 41)}
	for _, s := range ruins {
		if _, err := Slug(s); err == nil {
			t.Errorf("Slug(%q) devia falhar", s)
		}
	}
	if v, _ := Slug("  VENDAS  "); v != "vendas" {
		t.Errorf("Slug devia normalizar para minúscula sem espaço, veio %q", v)
	}
}

func TestModeloDoBot(t *testing.T) {
	if v, err := ModeloDoBot(""); err != nil || v != ModeloPadrao {
		t.Errorf("vazio devia virar o padrão %q, veio %q (%v)", ModeloPadrao, v, err)
	}
	if !ModeloValido(ModeloPadrao) {
		t.Errorf("o padrão %q precisa estar no catálogo", ModeloPadrao)
	}
	if _, err := ModeloDoBot("gpt-4"); err == nil {
		t.Error("modelo de outro provedor devia falhar")
	}
	// A mensagem precisa dizer o que vale, senão quem preenche fica adivinhando.
	_, err := ModeloDoBot("claude-inexistente")
	if err == nil || !strings.Contains(err.Error(), ModeloPadrao) {
		t.Errorf("o erro devia listar os modelos aceitos, veio %v", err)
	}
}

func TestInstrucoesESaudacao(t *testing.T) {
	if _, err := Instrucoes("responda"); err == nil {
		t.Error("instrução de uma palavra devia falhar: bot sem propósito inventa")
	}
	if _, err := Instrucoes(strings.Repeat("a", MaxInstrucoes+1)); err == nil {
		t.Error("instrução acima do limite devia falhar")
	}
	if v, err := Instrucoes("  Você é o primeiro atendimento da Travus no WhatsApp.  "); err != nil || strings.HasPrefix(v, " ") {
		t.Errorf("devia aceitar e tirar espaço das pontas: %q (%v)", v, err)
	}
	if v, err := Saudacao("   "); err != nil || v != nil {
		t.Errorf("saudação em branco devia virar nil (apaga a que havia), veio %v", v)
	}
}

func TestTeto(t *testing.T) {
	if v, err := Teto("o teto", nil, 60, 1, 1000); err != nil || v != 60 {
		t.Errorf("ausente devia virar o padrão: %d (%v)", v, err)
	}
	fora := 5000
	if _, err := Teto("o teto", &fora, 60, 1, 1000); err == nil {
		t.Error("acima do máximo devia falhar")
	}
	dentro := 120
	if v, _ := Teto("o teto", &dentro, 60, 1, 1000); v != 120 {
		t.Errorf("devia respeitar o valor pedido, veio %d", v)
	}
}

func TestPerguntas(t *testing.T) {
	ok := []Pergunta{
		{Texto: "Qual é o nome da empresa?", Campo: "nome_da_empresa", Obrigatoria: true},
		{Texto: "Quantas pessoas entrariam?", Obrigatoria: true}, // campo vem do texto
	}
	saida, err := Perguntas(ok)
	if err != nil {
		t.Fatal(err)
	}
	if len(saida) != 2 {
		t.Fatalf("devia devolver as duas: %+v", saida)
	}
	// Campo em branco sai do texto da pergunta — é o que a tela faz ao digitar.
	if saida[1].Campo != "quantas_pessoas_entrariam" {
		t.Errorf("o campo devia sair do texto: %q", saida[1].Campo)
	}

	// Lista vazia é permitida: bot só de tirar dúvida.
	if _, err := Perguntas(nil); err != nil {
		t.Errorf("atendimento sem perguntas devia ser aceito: %v", err)
	}

	ruins := map[string][]Pergunta{
		"pergunta vazia": {{Texto: "   ", Campo: "x_qualquer"}},
		"pergunta curta": {{Texto: "Oi?", Campo: "oi"}},
		"campo inválido": {{Texto: "Qual é o nome da empresa?", Campo: "Nome Da Empresa"}},
		"campo repetido": {
			{Texto: "Qual é o seu nome?", Campo: "nome"},
			{Texto: "E o nome da empresa?", Campo: "nome"},
		},
	}
	for nome, perguntas := range ruins {
		if _, err := Perguntas(perguntas); err == nil {
			t.Errorf("%s devia ser recusado", nome)
		}
	}

	demais := make([]Pergunta, MaxPerguntas+1)
	for i := range demais {
		demais[i] = Pergunta{Texto: "Pergunta de teste número " + string(rune('a'+i%26)) + "?"}
	}
	if _, err := Perguntas(demais); err == nil {
		t.Errorf("mais de %d perguntas devia ser recusado", MaxPerguntas)
	}
}

func TestPerguntasNormaliza(t *testing.T) {
	saida, err := Perguntas([]Pergunta{{
		Texto: "  Qual o melhor telefone?  ", Campo: "  MELHOR_TELEFONE  ", Obrigatoria: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if saida[0].Texto != "Qual o melhor telefone?" {
		t.Errorf("o texto devia vir aparado: %q", saida[0].Texto)
	}
	if saida[0].Campo != "melhor_telefone" {
		t.Errorf("o campo devia vir aparado e em minúscula: %q", saida[0].Campo)
	}
}

func TestCampoDaPergunta(t *testing.T) {
	casos := map[string]string{
		"Qual é o nome da empresa?":   "qual_e_o_nome_da_empresa",
		"Quantas vidas vocês querem?": "quantas_vidas_voces_querem",
		"CNPJ?":                       "cnpj",
		"3 perguntas?":                "perguntas",
		"  Melhor horário!!!  ":       "melhor_horario",
	}
	for pergunta, esperado := range casos {
		if v := CampoDaPergunta(pergunta); v != esperado {
			t.Errorf("CampoDaPergunta(%q) = %q, esperava %q", pergunta, v, esperado)
		}
	}
	// O nome do dado vira argumento da ferramenta do modelo: precisa começar com letra.
	if v := CampoDaPergunta("123?"); v != "" {
		t.Errorf("sem letra nenhuma devia dar vazio (a validação recusa): %q", v)
	}
}

// Os pontos de partida são oferecidos na criação de um bot. A Travus tem vários segmentos, então
// o que se garante aqui é que eles são válidos e que nenhum é imposto.
func TestPontosDePartida(t *testing.T) {
	if len(PontosDePartida) < 2 {
		t.Fatal("o catálogo não devia estar vazio")
	}
	if PontosDePartida[0].ID != "em-branco" {
		t.Error("o primeiro ponto de partida precisa ser o vazio: ninguém começa obrigado a um segmento")
	}
	if len(PontosDePartida[0].Perguntas) != 0 || PontosDePartida[0].Instrucoes != "" {
		t.Error("o ponto de partida em branco não pode trazer texto")
	}
	vistos := map[string]bool{}
	for _, p := range PontosDePartida {
		if vistos[p.ID] {
			t.Errorf("ponto de partida repetido: %s", p.ID)
		}
		vistos[p.ID] = true
		if p.Nome == "" || p.Descricao == "" {
			t.Errorf("%s: nome e descrição são o que a tela mostra", p.ID)
		}
		if p.ID == "em-branco" {
			continue
		}
		// O que é copiado para o bot precisa passar na mesma validação da API.
		if _, err := Instrucoes(p.Instrucoes); err != nil {
			t.Errorf("%s: instruções inválidas: %v", p.ID, err)
		}
		if _, err := Perguntas(p.Perguntas); err != nil {
			t.Errorf("%s: perguntas inválidas: %v", p.ID, err)
		}
		if _, err := Saudacao(p.Saudacao); err != nil {
			t.Errorf("%s: saudação inválida: %v", p.ID, err)
		}
	}
	if _, ok := PontoDePartidaPorID("nao-existe"); ok {
		t.Error("id desconhecido não devia achar nada")
	}
}
