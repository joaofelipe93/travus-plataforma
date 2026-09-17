// Package credenciais guarda as credenciais pessoais de cada usuário em serviços de
// terceiros (ex.: Trello), cifradas no banco. O que existe está neste catálogo: a tela de
// perfil desenha o formulário a partir dele e um serviço pergunta "esta pessoa já cadastrou
// a credencial X?" antes de rodar. Credencial nova = uma entrada aqui.
package credenciais

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Limites do que a pessoa digita (os valores vão cifrados num JSON só).
const (
	tamanhoMaximoValor = 500
	tamanhoMinimoDica  = 8 // abaixo disso a dica seria quase o segredo inteiro
	caracteresDaDica   = 4
)

// Campo é uma caixa do formulário. Todo valor é tratado como segredo: nenhum volta ao
// navegador depois de gravado.
type Campo struct {
	ID          string `json:"id"`
	Rotulo      string `json:"rotulo"`
	Ajuda       string `json:"ajuda,omitempty"`
	Obrigatorio bool   `json:"obrigatorio"`
	// Principal: é deste campo que sai a dica (últimos caracteres) mostrada na tela.
	Principal bool `json:"-"`
}

// Definicao é um tipo de credencial do catálogo.
type Definicao struct {
	ID        string `json:"id"`
	Nome      string `json:"nome"`
	Descricao string `json:"descricao"`
	// Passo a passo curto de onde tirar os valores, e o link da página do serviço.
	ComoObter string  `json:"como_obter"`
	LinkAjuda string  `json:"link_ajuda,omitempty"`
	Campos    []Campo `json:"campos"`
}

var catalogo = []Definicao{
	{
		ID:        "trello",
		Nome:      "Trello",
		Descricao: "Chave e token da sua conta do Trello. Os serviços que criam ou movem cartões agem em seu nome, com o que a sua conta enxerga.",
		ComoObter: "No Trello, abra o painel de Power-Ups, crie (ou abra) um Power-Up seu e copie a API key. Na mesma página, gere um token para a sua conta e cole aqui.",
		LinkAjuda: "https://trello.com/power-ups/admin",
		Campos: []Campo{
			{ID: "chave", Rotulo: "API key", Ajuda: "A chave do seu Power-Up no Trello", Obrigatorio: true},
			{ID: "token", Rotulo: "Token", Ajuda: "O token gerado para a sua conta", Obrigatorio: true, Principal: true},
		},
	},
}

// ErrDesconhecida: id que não está no catálogo.
var ErrDesconhecida = errors.New("credencial desconhecida")

// Catalogo devolve todas as definições, em ordem de nome.
func Catalogo() []Definicao {
	lista := make([]Definicao, len(catalogo))
	copy(lista, catalogo)
	sort.Slice(lista, func(i, j int) bool { return lista[i].Nome < lista[j].Nome })
	return lista
}

// Buscar acha a definição pelo id.
func Buscar(id string) (Definicao, error) {
	for _, d := range catalogo {
		if d.ID == id {
			return d, nil
		}
	}
	return Definicao{}, fmt.Errorf("%w: %q", ErrDesconhecida, id)
}

// Validar limpa e confere os valores recebidos da tela: nada de campo fora do catálogo, de
// obrigatório vazio ou de valor absurdamente longo. Opcional em branco some.
func (d Definicao) Validar(valores map[string]string) (map[string]string, error) {
	conhecidos := map[string]Campo{}
	for _, c := range d.Campos {
		conhecidos[c.ID] = c
	}
	limpos := make(map[string]string, len(valores))
	for id, v := range valores {
		campo, ok := conhecidos[id]
		if !ok {
			return nil, fmt.Errorf("campo %q não existe em %s", id, d.Nome)
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if utf8.RuneCountInString(v) > tamanhoMaximoValor {
			return nil, fmt.Errorf("%s: valor longo demais (máximo %d caracteres)", campo.Rotulo, tamanhoMaximoValor)
		}
		limpos[id] = v
	}
	for _, c := range d.Campos {
		if c.Obrigatorio && limpos[c.ID] == "" {
			return nil, fmt.Errorf("preencha %s", c.Rotulo)
		}
	}
	return limpos, nil
}

// Dica são os últimos caracteres do campo principal, só para a pessoa reconhecer o que
// cadastrou. Valor curto não vira dica: seria quase o segredo inteiro.
func (d Definicao) Dica(valores map[string]string) string {
	id := ""
	for _, c := range d.Campos {
		if c.Principal {
			id = c.ID
			break
		}
	}
	if id == "" && len(d.Campos) > 0 {
		id = d.Campos[0].ID
	}
	v := valores[id]
	if utf8.RuneCountInString(v) < tamanhoMinimoDica {
		return ""
	}
	r := []rune(v)
	return string(r[len(r)-caracteresDaDica:])
}
