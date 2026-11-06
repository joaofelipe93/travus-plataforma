// Package funil fala com o Funil de Vendas (app.funildevendas.com.br), o CRM da Smart.
//
// A API deles é um POST só: uma lista de oportunidades, cada uma com **empresa e contato dentro**
// — é assim que o CRM pensa, e é por isso que o lead do atendimento encaixa bem (o bot de plano
// de saúde empresarial colhe justamente empresa + pessoa).
//
// O que esta camada **não** faz: decidir se envia, quando reenviar ou o que é lead bom. Isso é da
// API (a fila em `atendimento_funil.go`). Aqui só se monta o JSON e se faz o pedido.
package funil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// URLPadrao é o endereço da API deles. Fica aqui (e não em variável de ambiente) porque é o
// endereço do produto, não configuração de cliente.
const URLPadrao = "https://app.funildevendas.com.br/api/Opportunity"

// Codigos da conta no Funil de Vendas: o que a tela de Integrações deles chama de "Parâmetros da
// Integração". Todos são obrigatórios na API — exceto a etapa, que o CRM preenche com a primeira
// do funil quando não vem.
type Codigos struct {
	Vendedor int32
	Funil    int32 // "codigo_metodologia" na API deles
	Etapa    int32
	Canal    int32
	Segmento int32
}

// Completos diz se dá para montar uma oportunidade: faltando um código obrigatório, a API recusa
// e o lead ficaria girando na fila de tentativas.
func (c Codigos) Completos() bool {
	return c.Vendedor > 0 && c.Funil > 0 && c.Canal > 0 && c.Segmento > 0
}

// Lead é o que a plataforma sabe de quem conversou com o bot. É o recorte que o Funil aproveita:
// o resto continua na conversa, que ninguém apaga.
type Lead struct {
	ID       int64
	Nome     string
	Telefone string
	Email    string
	// Dados colhidos na conversa, pelo nome do campo da pergunta.
	Dados map[string]string
}

// camposNoFunil liga o nome do dado da pergunta ao título do campo personalizado no CRM.
//
// O título tem de bater **letra por letra** com o que está cadastrado no Funil, senão o valor é
// ignorado lá sem erro nenhum. "Idades" já existia na conta; "Qtde de vidas" e "Plano atual"
// foram criados para esta integração (decisão do usuário em 06/11/2026).
var camposNoFunil = map[string]string{
	"faixas_de_idade": "Idades",
	"quantas_vidas":   "Qtde de vidas",
	"plano_atual":     "Plano atual",
}

// camposDeEmpresa e camposDeCargo: o mesmo dado tem nome diferente conforme o ponto de partida do
// bot, e quem escreve as perguntas não tem de saber do CRM.
var (
	camposDeEmpresa = []string{"nome_da_empresa", "empresa", "nome_empresa"}
	camposDeCnpj    = []string{"cnpj", "cnpj_da_empresa"}
	camposDeCargo   = []string{"cargo", "contato_e_cargo"}
)

type oportunidade struct {
	CodigoInterno     string          `json:"codigo_interno"`
	Titulo            string          `json:"titulo"`
	CodigoVendedor    int32           `json:"codigo_vendedor"`
	CodigoMetodologia int32           `json:"codigo_metodologia"`
	CodigoEtapa       int32           `json:"codigo_etapa,omitempty"`
	CodigoCanalVenda  int32           `json:"codigo_canal_venda"`
	Personalizados    []personalizado `json:"personalizados,omitempty"`
	Empresa           empresa         `json:"empresa"`
	Contato           contato         `json:"contato"`
}

type empresa struct {
	Nome           string `json:"nome"`
	Cnpj           string `json:"cnpj,omitempty"`
	CodigoSegmento int32  `json:"codigo_segmento"`
}

type contato struct {
	CodigoInterno string `json:"codigo_interno,omitempty"`
	Nome          string `json:"nome"`
	Email         string `json:"email,omitempty"`
	Telefone1     string `json:"telefone1,omitempty"`
	Cargo         string `json:"cargo,omitempty"`
}

type personalizado struct {
	Titulo string `json:"titulo"`
	Valor  string `json:"valor"`
}

type envelope struct {
	Oportunidades []oportunidade `json:"oportunidades"`
}

// Montar traduz o lead na oportunidade que o Funil espera.
//
// O `codigo_interno` leva o id do lead: é o campo que a API deles tem para o identificador de
// quem chama, e é o que permite reconhecer no CRM de onde a oportunidade veio — e perceber
// duplicata, que nesta conta é um risco real (a integração está marcada como "não validar").
func Montar(l Lead, c Codigos) ([]byte, error) {
	if !c.Completos() {
		return nil, fmt.Errorf("faltam códigos da conta no Funil de Vendas")
	}
	nomeEmpresa := primeiro(l.Dados, camposDeEmpresa)
	nomeContato := strings.TrimSpace(l.Nome)
	if nomeContato == "" {
		nomeContato = "Sem nome informado"
	}
	if nomeEmpresa == "" {
		// Empresa é obrigatória na API deles. Sem o nome, o contato serve de empresa: é o que
		// mantém a oportunidade rastreável em vez de recusada.
		nomeEmpresa = nomeContato
	}

	o := oportunidade{
		CodigoInterno:     fmt.Sprintf("travus-lead-%d", l.ID),
		Titulo:            nomeEmpresa,
		CodigoVendedor:    c.Vendedor,
		CodigoMetodologia: c.Funil,
		CodigoEtapa:       c.Etapa,
		CodigoCanalVenda:  c.Canal,
		Empresa: empresa{
			Nome:           nomeEmpresa,
			Cnpj:           primeiro(l.Dados, camposDeCnpj),
			CodigoSegmento: c.Segmento,
		},
		Contato: contato{
			CodigoInterno: fmt.Sprintf("travus-lead-%d", l.ID),
			Nome:          nomeContato,
			Email:         strings.TrimSpace(l.Email),
			Telefone1:     strings.TrimSpace(l.Telefone),
			Cargo:         primeiro(l.Dados, camposDeCargo),
		},
	}
	// Campos personalizados em ordem estável: JSON que muda de ordem a cada envio atrapalha quem
	// compara dois pedidos no log.
	for _, campo := range ordenar(camposNoFunil) {
		if valor := strings.TrimSpace(l.Dados[campo]); valor != "" {
			o.Personalizados = append(o.Personalizados, personalizado{Titulo: camposNoFunil[campo], Valor: valor})
		}
	}
	return json.Marshal(envelope{Oportunidades: []oportunidade{o}})
}

func primeiro(dados map[string]string, campos []string) string {
	for _, c := range campos {
		if v := strings.TrimSpace(dados[c]); v != "" {
			return v
		}
	}
	return ""
}

func ordenar(m map[string]string) []string {
	fora := make([]string, 0, len(m))
	for k := range m {
		fora = append(fora, k)
	}
	for i := 1; i < len(fora); i++ {
		for j := i; j > 0 && fora[j] < fora[j-1]; j-- {
			fora[j], fora[j-1] = fora[j-1], fora[j]
		}
	}
	return fora
}

// Cliente manda a oportunidade. A chave vai na query string porque é como a API deles funciona —
// por isso ela nunca entra em log nosso, nem em mensagem de erro.
type Cliente struct {
	URL   string
	Chave string
	HTTP  *http.Client
}

func NovoCliente(chave string) *Cliente {
	return &Cliente{URL: URLPadrao, Chave: chave, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Enviar devolve a mensagem que o Funil respondeu (a API deles responde `{"message": ...}`).
func (c *Cliente) Enviar(ctx context.Context, corpo []byte) (string, error) {
	if strings.TrimSpace(c.Chave) == "" {
		return "", fmt.Errorf("sem a chave de integração do Funil de Vendas")
	}
	endereco := c.URL + "?IntegrationKey=" + url.QueryEscape(c.Chave)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endereco, bytes.NewReader(corpo))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// O erro do net/http traz a URL — e a URL traz a chave.
		return "", fmt.Errorf("falha ao falar com o Funil de Vendas: %s", semChave(err.Error(), c.Chave))
	}
	defer resp.Body.Close()
	bruto, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	mensagem := mensagemDaResposta(bruto)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return mensagem, fmt.Errorf("o Funil de Vendas recusou (HTTP %d): %s", resp.StatusCode, semChave(mensagem, c.Chave))
	}
	return mensagem, nil
}

// mensagemDaResposta tira o texto de `{"message": "..."}`; não sendo JSON, devolve o corpo cru
// cortado — é o que vai aparecer na tela para alguém entender o que o CRM disse.
func mensagemDaResposta(bruto []byte) string {
	var r struct {
		Message  string `json:"message"`
		Mensagem string `json:"mensagem"`
	}
	if err := json.Unmarshal(bruto, &r); err == nil {
		if m := strings.TrimSpace(r.Message); m != "" {
			return m
		}
		if m := strings.TrimSpace(r.Mensagem); m != "" {
			return m
		}
	}
	texto := strings.TrimSpace(string(bruto))
	if len([]rune(texto)) > 500 {
		texto = string([]rune(texto)[:500])
	}
	return texto
}

// semChave garante que a chave não vaze em log nem na tela, mesmo quando vem dentro de um erro
// do net/http (que inclui a URL inteira).
func semChave(texto, chave string) string {
	if chave == "" {
		return texto
	}
	return strings.ReplaceAll(texto, chave, "<chave>")
}
