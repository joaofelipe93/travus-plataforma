package funil

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// O mapeamento é o que decide o que chega ao CRM da Smart. Nenhum teste fala com o Funil de
// Vendas de verdade: o que existe aqui é um servidor de mentira.

func codigosDeTeste() Codigos {
	return Codigos{Vendedor: 75661, Funil: 24458, Etapa: 112803, Canal: 126717, Segmento: 273730}
}

func montarMapa(t *testing.T, l Lead) map[string]any {
	t.Helper()
	bruto, err := Montar(l, codigosDeTeste())
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Oportunidades []map[string]any `json:"oportunidades"`
	}
	if err := json.Unmarshal(bruto, &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Oportunidades) != 1 {
		t.Fatalf("devia ser uma oportunidade: %s", bruto)
	}
	return env.Oportunidades[0]
}

func TestMontaAOportunidadeComEmpresaEContato(t *testing.T) {
	o := montarMapa(t, Lead{
		ID: 7, Nome: "Maria Souza", Telefone: "5511999998888", Email: "maria@acme.com.br",
		Dados: map[string]string{
			"nome_da_empresa": "Acme Ltda",
			"cnpj":            "12345678000199",
			"contato_e_cargo": "gerente de RH",
			"quantas_vidas":   "40",
			"plano_atual":     "tem com a operadora X",
			"faixas_de_idade": "a maioria entre 30 e 45",
		},
	})

	if o["titulo"] != "Acme Ltda" {
		t.Errorf("o título devia ser o nome da empresa: %v", o["titulo"])
	}
	// O id do lead vai no campo que a API deles tem para isso: é o que liga a oportunidade à
	// conversa e deixa duplicata visível.
	if o["codigo_interno"] != "travus-lead-7" {
		t.Errorf("codigo_interno: %v", o["codigo_interno"])
	}
	for campo, esperado := range map[string]any{
		"codigo_vendedor": 75661.0, "codigo_metodologia": 24458.0,
		"codigo_etapa": 112803.0, "codigo_canal_venda": 126717.0,
	} {
		if o[campo] != esperado {
			t.Errorf("%s: %v (esperava %v)", campo, o[campo], esperado)
		}
	}

	emp := o["empresa"].(map[string]any)
	if emp["nome"] != "Acme Ltda" || emp["cnpj"] != "12345678000199" || emp["codigo_segmento"] != 273730.0 {
		t.Errorf("empresa: %+v", emp)
	}
	con := o["contato"].(map[string]any)
	if con["nome"] != "Maria Souza" || con["telefone1"] != "5511999998888" ||
		con["email"] != "maria@acme.com.br" || con["cargo"] != "gerente de RH" {
		t.Errorf("contato: %+v", con)
	}

	// Os campos personalizados precisam bater letra por letra com os do CRM, senão o valor é
	// ignorado lá sem erro nenhum.
	pers := map[string]string{}
	for _, p := range o["personalizados"].([]any) {
		m := p.(map[string]any)
		pers[m["titulo"].(string)] = m["valor"].(string)
	}
	for titulo, esperado := range map[string]string{
		"Idades": "a maioria entre 30 e 45", "Qtde de vidas": "40", "Plano atual": "tem com a operadora X",
	} {
		if pers[titulo] != esperado {
			t.Errorf("campo personalizado %q: %q", titulo, pers[titulo])
		}
	}
}

// Empresa é obrigatória na API deles. Sem o nome da empresa, o contato serve — melhor uma
// oportunidade rastreável que uma recusada.
func TestSemEmpresaUsaONomeDaPessoa(t *testing.T) {
	o := montarMapa(t, Lead{ID: 9, Nome: "João Pedro", Telefone: "5511911112222"})
	if o["titulo"] != "João Pedro" {
		t.Errorf("título: %v", o["titulo"])
	}
	if o["empresa"].(map[string]any)["nome"] != "João Pedro" {
		t.Errorf("empresa: %+v", o["empresa"])
	}
	// Campo personalizado vazio não vai: o CRM mostraria um campo em branco sem motivo.
	if _, tem := o["personalizados"]; tem {
		t.Errorf("sem dados, não devia mandar personalizados: %+v", o["personalizados"])
	}
}

// Falta um código obrigatório: nem monta. É o que impede o lead de girar na fila levando recusa
// do CRM a cada tentativa.
func TestSemOsCodigosNaoMonta(t *testing.T) {
	if _, err := Montar(Lead{ID: 1, Nome: "Alguém"}, Codigos{Vendedor: 1}); err == nil {
		t.Error("devia recusar sem os códigos da conta")
	}
	if codigosDeTeste().Completos() != true {
		t.Error("os códigos de teste deviam estar completos")
	}
	if (Codigos{Vendedor: 1, Funil: 2, Canal: 3}).Completos() {
		t.Error("sem segmento, os códigos não estão completos")
	}
}

func TestEnviaEDevolveAMensagemDoFunil(t *testing.T) {
	var recebido struct {
		corpo string
		chave string
		tipo  string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		recebido.corpo, recebido.chave, recebido.tipo = string(b), r.URL.Query().Get("IntegrationKey"), r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"Oportunidade cadastrada com sucesso"}`))
	}))
	defer srv.Close()

	c := &Cliente{URL: srv.URL, Chave: "chave-de-teste", HTTP: srv.Client()}
	msg, err := c.Enviar(context.Background(), []byte(`{"oportunidades":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if msg != "Oportunidade cadastrada com sucesso" {
		t.Errorf("mensagem: %q", msg)
	}
	if recebido.chave != "chave-de-teste" || recebido.tipo != "application/json" {
		t.Errorf("pedido: %+v", recebido)
	}
}

// Recusa do CRM é erro — e a mensagem dele vai junto, porque é o que explica na tela.
func TestRecusaDoFunilViraErroComMensagem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Código do vendedor inválido"}`))
	}))
	defer srv.Close()

	c := &Cliente{URL: srv.URL, Chave: "k", HTTP: srv.Client()}
	msg, err := c.Enviar(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("HTTP 400 devia virar erro")
	}
	if !strings.Contains(err.Error(), "Código do vendedor inválido") || msg == "" {
		t.Errorf("o motivo do CRM devia aparecer: %v / %q", err, msg)
	}
}

// A chave vai na URL (é a API deles). Ela não pode vazar no erro, que é o que vai para o log e
// para a tela.
func TestAChaveNaoVazaNoErro(t *testing.T) {
	c := &Cliente{URL: "http://127.0.0.1:1/naoexiste", Chave: "SEGREDO-123", HTTP: &http.Client{}}
	_, err := c.Enviar(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("devia falhar")
	}
	if strings.Contains(err.Error(), "SEGREDO-123") {
		t.Errorf("a chave vazou no erro: %v", err)
	}
	// Sem chave nenhuma, nem tenta.
	if _, err := (&Cliente{URL: "http://x", HTTP: &http.Client{}}).Enviar(context.Background(), nil); err == nil {
		t.Error("sem chave devia recusar antes de pedir")
	}
}
