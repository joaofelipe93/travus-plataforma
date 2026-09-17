package credenciais

import (
	"errors"
	"strings"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/cripto"
)

// Valor fictício com a cara de um token, sem entropia de segredo de verdade.
const valorFicticio = "fake-fake-fake-1234"

func TestCatalogoBemFormado(t *testing.T) {
	vistos := map[string]bool{}
	for _, d := range Catalogo() {
		if d.ID == "" || d.Nome == "" || d.Descricao == "" || len(d.Campos) == 0 {
			t.Errorf("definição incompleta: %+v", d)
		}
		if vistos[d.ID] {
			t.Errorf("id repetido no catálogo: %q", d.ID)
		}
		vistos[d.ID] = true
		principais := 0
		campos := map[string]bool{}
		for _, c := range d.Campos {
			if c.ID == "" || c.Rotulo == "" {
				t.Errorf("%s: campo incompleto: %+v", d.ID, c)
			}
			if campos[c.ID] {
				t.Errorf("%s: campo repetido: %q", d.ID, c.ID)
			}
			campos[c.ID] = true
			if c.Principal {
				principais++
			}
		}
		if principais > 1 {
			t.Errorf("%s: mais de um campo principal (a dica sai de um só)", d.ID)
		}
	}
	if !vistos["trello"] {
		t.Error("o catálogo perdeu o Trello")
	}
}

func TestBuscarDesconhecida(t *testing.T) {
	if _, err := Buscar("servico-que-nao-existe"); !errors.Is(err, ErrDesconhecida) {
		t.Fatalf("erro = %v, quer ErrDesconhecida", err)
	}
}

func TestValidar(t *testing.T) {
	trello, err := Buscar("trello")
	if err != nil {
		t.Fatal(err)
	}

	limpos, err := trello.Validar(map[string]string{"chave": "  abc123  ", "token": valorFicticio})
	if err != nil {
		t.Fatalf("valores válidos recusados: %v", err)
	}
	if limpos["chave"] != "abc123" {
		t.Errorf("espaços não foram tirados: %q", limpos["chave"])
	}

	casos := map[string]map[string]string{
		"obrigatório vazio":      {"chave": "abc123", "token": "   "},
		"obrigatório ausente":    {"chave": "abc123"},
		"campo fora do catálogo": {"chave": "abc123", "token": valorFicticio, "senha": "x"},
		"valor longo demais":     {"chave": "abc123", "token": strings.Repeat("t", tamanhoMaximoValor+1)},
	}
	for nome, valores := range casos {
		if _, err := trello.Validar(valores); err == nil {
			t.Errorf("%s: aceito, devia recusar", nome)
		}
	}
}

func TestDicaNaoRevelaSegredoCurto(t *testing.T) {
	trello, _ := Buscar("trello")
	if dica := trello.Dica(map[string]string{"token": valorFicticio}); dica != "1234" {
		t.Errorf("dica = %q, quer os 4 últimos do token", dica)
	}
	if dica := trello.Dica(map[string]string{"token": "curto"}); dica != "" {
		t.Errorf("segredo curto virou dica: %q", dica)
	}
}

// O contexto do cofre amarra o valor à pessoa e à credencial: um vazamento de linhas do
// banco não deixa usar o segredo de alguém no lugar do de outro.
func TestContextoIsolaPessoaECredencial(t *testing.T) {
	cofre, err := cripto.NovoCofre(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	cifrado, err := cofre.Cifrar([]byte(`{"token":"segredo"}`), contexto(1, "trello"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cofre.Decifrar(cifrado, contexto(1, "trello")); err != nil {
		t.Fatalf("mesmo contexto devia decifrar: %v", err)
	}
	for nome, ctx := range map[string]string{
		"outra pessoa":     contexto(2, "trello"),
		"outra credencial": contexto(1, "outro"),
	} {
		if _, err := cofre.Decifrar(cifrado, ctx); !errors.Is(err, cripto.ErrDadosInvalidos) {
			t.Errorf("%s: decifrou (erro %v)", nome, err)
		}
	}
}
