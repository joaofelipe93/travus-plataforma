package importacao

import (
	"os"
	"testing"
)

func lerFixture(t *testing.T, nome string) []Linha {
	t.Helper()
	conteudo, err := os.ReadFile("testdata/paridade/" + nome)
	if err != nil {
		t.Fatal(err)
	}
	linhas, erros, err := LerPlanilha(conteudo)
	if err != nil || len(erros) > 0 {
		t.Fatalf("fixture %s: err=%v erros=%v", nome, err, erros)
	}
	return linhas
}

func TestPlanejarComCadastroVazio(t *testing.T) {
	p := Planejar(lerFixture(t, "01-planilha-clientes.csv"), nil, Estado{})
	want := Totais{Linhas: 4, Clientes: 3, ClientesNovos: 3, CotasNovas: 4}
	if p.Previa.Totais != want {
		t.Fatalf("totais = %+v, quer %+v", p.Previa.Totais, want)
	}
	if p.Previa.ClientesNovos[0].Nome != "CLIENTE EXEMPLO UM" || p.Previa.ClientesNovos[0].Cotas != 2 ||
		p.Previa.ClientesNovos[0].Telefone != "(11) 90000-0000" {
		t.Errorf("cliente novo: %+v", p.Previa.ClientesNovos[0])
	}
	if c := p.Previa.CotasNovas[3]; c.Grupo != "006650" || c.Cota != "0924" || c.Cliente != "EXEMPLO, CLIENTE TRÊS" {
		t.Errorf("cota nova: %+v", c)
	}
}

func TestPlanejarComCadastroExistente(t *testing.T) {
	linhas := lerFixture(t, "01-planilha-clientes.csv")
	dadosUm := linhas[0].DadosPlanilha

	estado := Estado{
		Clientes: []ClienteAtual{
			{ID: 1, Nome: "CLIENTE EXEMPLO UM", NomeNormalizado: "CLIENTE EXEMPLO UM"},
			{ID: 2, Nome: "Cliente Exemplo Dois", NomeNormalizado: "CLIENTE EXEMPLO DOIS", Telefone: "(11) 91111-1111"},
		},
		Cotas: []CotaAtual{
			// igual à planilha
			{ID: 10, ClienteID: 1, ClienteNome: "CLIENTE EXEMPLO UM", ClienteNomeNormalizado: "CLIENTE EXEMPLO UM",
				Administradora: "CANOPUS", Grupo: "001234", Cota: "0056", Versao: "00", TipoConsorcio: "IMÓVEL", Ativa: true, DadosPlanilha: dadosUm},
			// na planilha está com outro cliente e outro tipo
			{ID: 11, ClienteID: 2, ClienteNome: "Cliente Exemplo Dois", ClienteNomeNormalizado: "CLIENTE EXEMPLO DOIS",
				Administradora: "CANOPUS", Grupo: "001234", Cota: "0089", Versao: "00", TipoConsorcio: "AUTOMÓVEL", Ativa: true, DadosPlanilha: dadosUm},
			// não está na planilha
			{ID: 12, ClienteID: 2, ClienteNome: "Cliente Exemplo Dois", ClienteNomeNormalizado: "CLIENTE EXEMPLO DOIS",
				Administradora: "CANOPUS", Grupo: "009999", Cota: "0001", Versao: "00", Ativa: false},
		},
	}

	p := Planejar(linhas, nil, estado)
	want := Totais{Linhas: 4, Clientes: 3, ClientesNovos: 1, ClientesAlterados: 1, CotasNovas: 2, CotasAlteradas: 1, CotasSemMudanca: 1, ForaDaPlanilha: 1}
	if p.Previa.Totais != want {
		t.Fatalf("totais = %+v, quer %+v", p.Previa.Totais, want)
	}

	// Cliente UM ganhou telefone e e-mail; DOIS tem telefone no cadastro e vazio na planilha: não muda.
	if c := p.Previa.ClientesAlterados[0]; c.Nome != "CLIENTE EXEMPLO UM" || len(c.Mudancas) != 2 ||
		c.Mudancas[0].Campo != "telefone" || c.Mudancas[1].Campo != "email" {
		t.Errorf("cliente alterado: %+v", c)
	}
	alterada := p.Previa.CotasAlteradas[0]
	if alterada.Cota != "0089" || len(alterada.Mudancas) != 2 ||
		alterada.Mudancas[0] != (Mudanca{"cliente", "Cliente Exemplo Dois", "CLIENTE EXEMPLO UM"}) ||
		alterada.Mudancas[1] != (Mudanca{"tipo_consorcio", "AUTOMÓVEL", "IMÓVEL"}) {
		t.Errorf("cota alterada: %+v", alterada)
	}
	if f := p.Previa.ForaDaPlanilha[0]; f.Grupo != "009999" || f.Ativa == nil || *f.Ativa {
		t.Errorf("fora da planilha: %+v", f)
	}
}

func TestPlanejarErrosDeValidacao(t *testing.T) {
	linhas, errosLeitura, err := LerPlanilha([]byte("nome,grupo,cota\nA,6650,924\n,6650,236\nB,6650,0924\nC,0,1\n"))
	if err != nil {
		t.Fatal(err)
	}
	p := Planejar(linhas, errosLeitura, Estado{})
	if p.Previa.Totais.Erros != 3 || p.Previa.Totais.CotasNovas != 1 || p.Previa.Totais.Linhas != 4 {
		t.Fatalf("totais: %+v erros: %+v", p.Previa.Totais, p.Previa.Erros)
	}
	linhasComErro := [3]int{p.Previa.Erros[0].Linha, p.Previa.Erros[1].Linha, p.Previa.Erros[2].Linha}
	if linhasComErro != [3]int{3, 4, 5} {
		t.Errorf("erros fora de ordem ou nas linhas erradas: %+v", p.Previa.Erros)
	}
}

func TestMesmaPrevia(t *testing.T) {
	linhas := lerFixture(t, "01-planilha-clientes.csv")
	a := Planejar(linhas, nil, Estado{}).Previa
	b := Planejar(linhas, nil, Estado{}).Previa
	if !MesmaPrevia(a, b) {
		t.Error("prévias iguais consideradas diferentes")
	}
	c := Planejar(linhas[:3], nil, Estado{}).Previa
	if MesmaPrevia(a, c) {
		t.Error("prévias diferentes consideradas iguais")
	}
}
