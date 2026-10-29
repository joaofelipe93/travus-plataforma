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
	if v, err := Instrucoes("  Você atende a imobiliária e só fala de aluguel.  "); err != nil || strings.HasPrefix(v, " ") {
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

func TestEtapas(t *testing.T) {
	ok := []Etapa{{
		Nome:     "Entender o interesse",
		Objetivo: "Descobrir o que a pessoa procura e em que bairro",
		Campos:   []Campo{{ID: "interesse", Rotulo: "Interesse", Obrigatorio: true}, {ID: "bairro", Rotulo: "Bairro"}},
	}}
	saida, err := Etapas(ok)
	if err != nil || len(saida) != 1 || len(saida[0].Campos) != 2 {
		t.Fatalf("fluxo válido devia passar: %v", err)
	}
	// Fluxo vazio é legítimo: bot que só tira dúvida não coleta nada.
	if _, err := Etapas(nil); err != nil {
		t.Errorf("fluxo vazio devia passar: %v", err)
	}

	casos := map[string][]Etapa{
		"objetivo curto":   {{Nome: "X", Objetivo: "curto"}},
		"nome vazio":       {{Nome: "  ", Objetivo: "Descobrir o que a pessoa procura"}},
		"campo repetido":   {{Nome: "X", Objetivo: "Descobrir o que a pessoa procura", Campos: []Campo{{ID: "nome", Rotulo: "Nome"}, {ID: "nome", Rotulo: "Nome de novo"}}}},
		"id de campo ruim": {{Nome: "X", Objetivo: "Descobrir o que a pessoa procura", Campos: []Campo{{ID: "Nome Completo", Rotulo: "Nome"}}}},
		"campo sem rótulo": {{Nome: "X", Objetivo: "Descobrir o que a pessoa procura", Campos: []Campo{{ID: "nome"}}}},
	}
	for nome, etapas := range casos {
		if _, err := Etapas(etapas); err == nil {
			t.Errorf("%s: devia falhar", nome)
		}
	}

	demais := make([]Etapa, MaxEtapas+1)
	for i := range demais {
		demais[i] = Etapa{Nome: "X", Objetivo: "Descobrir o que a pessoa procura"}
	}
	if _, err := Etapas(demais); err == nil {
		t.Error("fluxo acima do limite de etapas devia falhar")
	}
}

func TestEtapasNormalizaCampo(t *testing.T) {
	saida, err := Etapas([]Etapa{{
		Nome:     "X",
		Objetivo: "Descobrir o que a pessoa procura",
		Campos:   []Campo{{ID: "  TELEFONE  ", Rotulo: "  Telefone  "}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if c := saida[0].Campos[0]; c.ID != "telefone" || c.Rotulo != "Telefone" {
		t.Errorf("campo devia ser normalizado, veio id=%q rotulo=%q", c.ID, c.Rotulo)
	}
}
