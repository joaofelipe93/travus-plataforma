package importacao

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// ClienteAtual e CotaAtual descrevem o que já está no cadastro.
type ClienteAtual struct {
	ID              int64
	Nome            string
	NomeNormalizado string
	Telefone        string
	Email           string
}

type CotaAtual struct {
	ID                     int64
	ClienteID              int64
	ClienteNome            string
	ClienteNomeNormalizado string
	Administradora         string
	Grupo                  string
	Cota                   string
	Versao                 string
	TipoConsorcio          string
	Ativa                  bool
	DadosPlanilha          map[string]string
}

type Estado struct {
	Clientes []ClienteAtual
	Cotas    []CotaAtual
}

type Totais struct {
	Linhas            int `json:"linhas"`
	Clientes          int `json:"clientes"`
	ClientesNovos     int `json:"clientes_novos"`
	ClientesAlterados int `json:"clientes_alterados"`
	CotasNovas        int `json:"cotas_novas"`
	CotasAlteradas    int `json:"cotas_alteradas"`
	CotasSemMudanca   int `json:"cotas_sem_mudanca"`
	ForaDaPlanilha    int `json:"fora_da_planilha"`
	Erros             int `json:"erros"`
}

type Mudanca struct {
	Campo  string `json:"campo"`
	Antes  string `json:"antes"`
	Depois string `json:"depois"`
}

type CotaPrevia struct {
	Linha          int       `json:"linha,omitempty"`
	Administradora string    `json:"administradora"`
	Grupo          string    `json:"grupo"`
	Cota           string    `json:"cota"`
	Versao         string    `json:"versao"`
	Cliente        string    `json:"cliente"`
	TipoConsorcio  string    `json:"tipo_consorcio,omitempty"`
	Ativa          *bool     `json:"ativa,omitempty"`
	Mudancas       []Mudanca `json:"mudancas,omitempty"`
}

type ClientePrevia struct {
	Nome     string    `json:"nome"`
	Telefone string    `json:"telefone,omitempty"`
	Email    string    `json:"email,omitempty"`
	Cotas    int       `json:"cotas"`
	Mudancas []Mudanca `json:"mudancas,omitempty"`
}

// Previa é o que o operador revisa antes de aplicar.
type Previa struct {
	Totais            Totais          `json:"totais"`
	Erros             []ErroLinha     `json:"erros"`
	ClientesNovos     []ClientePrevia `json:"clientes_novos"`
	ClientesAlterados []ClientePrevia `json:"clientes_alterados"`
	CotasNovas        []CotaPrevia    `json:"cotas_novas"`
	CotasAlteradas    []CotaPrevia    `json:"cotas_alteradas"`
	// Cotas do cadastro que não estão na planilha. Não são desativadas automaticamente.
	ForaDaPlanilha []CotaPrevia `json:"fora_da_planilha"`
}

// Plano é a prévia mais as ações para aplicá-la.
type Plano struct {
	Previa Previa

	idsClientes       map[string]int64 // nome normalizado → id (clientes já cadastrados)
	clientesNovos     []clientePlanilha
	clientesAlterados []clientePlanilha
	cotasNovas        []Linha
	cotasAlteradas    []cotaAlterar
}

type clientePlanilha struct {
	id          int64
	nome        string
	normalizado string
	telefone    string
	email       string
	cotas       int
}

type cotaAlterar struct {
	id    int64
	linha Linha
}

// NormalizarNome identifica o cliente: maiúsculas e espaços únicos.
func NormalizarNome(nome string) string {
	return strings.ToUpper(strings.Join(strings.Fields(nome), " "))
}

func chaveCota(administradora, grupo, cota, versao string) string {
	return administradora + "|" + grupo + "|" + cota + "|" + versao
}

// Planejar compara a planilha com o cadastro atual. Não toca no banco.
func Planejar(linhas []Linha, errosLeitura []ErroLinha, estado Estado) Plano {
	p := Plano{
		idsClientes: map[string]int64{},
		Previa: Previa{
			Erros:             append([]ErroLinha{}, errosLeitura...),
			ClientesNovos:     []ClientePrevia{},
			ClientesAlterados: []ClientePrevia{},
			CotasNovas:        []CotaPrevia{},
			CotasAlteradas:    []CotaPrevia{},
			ForaDaPlanilha:    []CotaPrevia{},
		},
	}

	clientesAtuais := map[string]ClienteAtual{}
	for _, c := range estado.Clientes {
		clientesAtuais[c.NomeNormalizado] = c
		p.idsClientes[c.NomeNormalizado] = c.ID
	}
	cotasAtuais := map[string]CotaAtual{}
	for _, c := range estado.Cotas {
		cotasAtuais[chaveCota(c.Administradora, c.Grupo, c.Cota, c.Versao)] = c
	}

	// Validações da plataforma (o csv.js não exigia nome nem checava repetição).
	var validas []Linha
	primeiraLinha := map[string]int{}
	for _, l := range linhas {
		if NormalizarNome(l.Nome) == "" {
			p.Previa.Erros = append(p.Previa.Erros, ErroLinha{l.Linha, fmt.Sprintf("cota %s sem nome do cliente (coluna NOME)", l.Tag())})
			continue
		}
		k := chaveCota(l.Administradora, l.Grupo, l.Cota, l.Versao)
		if primeira, ok := primeiraLinha[k]; ok {
			p.Previa.Erros = append(p.Previa.Erros, ErroLinha{l.Linha, fmt.Sprintf("cota %s repetida (já aparece na linha %d)", l.Tag(), primeira)})
			continue
		}
		primeiraLinha[k] = l.Linha
		validas = append(validas, l)
	}

	// Clientes da planilha, na ordem em que aparecem. Contato: primeiro valor preenchido.
	porNome := map[string]*clientePlanilha{}
	var ordem []string
	for _, l := range validas {
		n := NormalizarNome(l.Nome)
		c, ok := porNome[n]
		if !ok {
			c = &clientePlanilha{nome: strings.Join(strings.Fields(l.Nome), " "), normalizado: n}
			porNome[n] = c
			ordem = append(ordem, n)
		}
		c.cotas++
		if c.telefone == "" {
			c.telefone = l.Telefone
		}
		if c.email == "" {
			c.email = l.Email
		}
	}
	for _, n := range ordem {
		c := porNome[n]
		atual, existe := clientesAtuais[n]
		if !existe {
			p.clientesNovos = append(p.clientesNovos, *c)
			p.Previa.ClientesNovos = append(p.Previa.ClientesNovos, ClientePrevia{Nome: c.nome, Telefone: c.telefone, Email: c.email, Cotas: c.cotas})
			continue
		}
		c.id = atual.ID
		// Telefone/e-mail vazios na planilha não apagam o cadastro.
		var mudancas []Mudanca
		if c.telefone != "" && c.telefone != atual.Telefone {
			mudancas = append(mudancas, Mudanca{"telefone", atual.Telefone, c.telefone})
		}
		if c.email != "" && c.email != atual.Email {
			mudancas = append(mudancas, Mudanca{"email", atual.Email, c.email})
		}
		if len(mudancas) > 0 {
			p.clientesAlterados = append(p.clientesAlterados, *c)
			p.Previa.ClientesAlterados = append(p.Previa.ClientesAlterados, ClientePrevia{Nome: atual.Nome, Telefone: c.telefone, Email: c.email, Cotas: c.cotas, Mudancas: mudancas})
		}
	}

	naPlanilha := map[string]bool{}
	for _, l := range validas {
		k := chaveCota(l.Administradora, l.Grupo, l.Cota, l.Versao)
		naPlanilha[k] = true
		cliente := porNome[NormalizarNome(l.Nome)]
		cp := CotaPrevia{Linha: l.Linha, Administradora: l.Administradora, Grupo: l.Grupo, Cota: l.Cota, Versao: l.Versao, Cliente: cliente.nome, TipoConsorcio: l.TipoConsorcio}

		atual, existe := cotasAtuais[k]
		if !existe {
			p.cotasNovas = append(p.cotasNovas, l)
			p.Previa.CotasNovas = append(p.Previa.CotasNovas, cp)
			continue
		}
		var mudancas []Mudanca
		if atual.ClienteNomeNormalizado != cliente.normalizado {
			mudancas = append(mudancas, Mudanca{"cliente", atual.ClienteNome, cliente.nome})
		}
		if atual.TipoConsorcio != l.TipoConsorcio {
			mudancas = append(mudancas, Mudanca{"tipo_consorcio", atual.TipoConsorcio, l.TipoConsorcio})
		}
		mudancas = append(mudancas, mudancasDados(atual.DadosPlanilha, l.DadosPlanilha)...)
		if len(mudancas) == 0 {
			p.Previa.Totais.CotasSemMudanca++
			continue
		}
		cp.Mudancas = mudancas
		p.cotasAlteradas = append(p.cotasAlteradas, cotaAlterar{atual.ID, l})
		p.Previa.CotasAlteradas = append(p.Previa.CotasAlteradas, cp)
	}

	for _, c := range estado.Cotas {
		if !naPlanilha[chaveCota(c.Administradora, c.Grupo, c.Cota, c.Versao)] {
			ativa := c.Ativa
			p.Previa.ForaDaPlanilha = append(p.Previa.ForaDaPlanilha, CotaPrevia{
				Administradora: c.Administradora, Grupo: c.Grupo, Cota: c.Cota, Versao: c.Versao,
				Cliente: c.ClienteNome, TipoConsorcio: c.TipoConsorcio, Ativa: &ativa,
			})
		}
	}
	sort.SliceStable(p.Previa.ForaDaPlanilha, func(i, j int) bool {
		a, b := p.Previa.ForaDaPlanilha[i], p.Previa.ForaDaPlanilha[j]
		return chaveCota(a.Administradora, a.Grupo, a.Cota, a.Versao) < chaveCota(b.Administradora, b.Grupo, b.Cota, b.Versao)
	})
	sort.SliceStable(p.Previa.Erros, func(i, j int) bool { return p.Previa.Erros[i].Linha < p.Previa.Erros[j].Linha })

	t := &p.Previa.Totais
	t.Linhas = len(linhas) + len(errosLeitura)
	t.Clientes = len(ordem)
	t.ClientesNovos = len(p.Previa.ClientesNovos)
	t.ClientesAlterados = len(p.Previa.ClientesAlterados)
	t.CotasNovas = len(p.Previa.CotasNovas)
	t.CotasAlteradas = len(p.Previa.CotasAlteradas)
	t.ForaDaPlanilha = len(p.Previa.ForaDaPlanilha)
	t.Erros = len(p.Previa.Erros)
	return p
}

func mudancasDados(antes, depois map[string]string) []Mudanca {
	chaves := map[string]bool{}
	for k := range antes {
		chaves[k] = true
	}
	for k := range depois {
		chaves[k] = true
	}
	ordenadas := make([]string, 0, len(chaves))
	for k := range chaves {
		ordenadas = append(ordenadas, k)
	}
	sort.Strings(ordenadas)

	var out []Mudanca
	for _, k := range ordenadas {
		if antes[k] != depois[k] {
			out = append(out, Mudanca{"planilha: " + k, antes[k], depois[k]})
		}
	}
	return out
}

// MesmaPrevia diz se duas prévias são equivalentes (usada para recusar a aplicação
// de uma prévia calculada sobre um cadastro que mudou depois).
func MesmaPrevia(a, b Previa) bool {
	return reflect.DeepEqual(normalizar(a), normalizar(b))
}

func normalizar(p Previa) Previa {
	bruto, _ := json.Marshal(p)
	var out Previa
	_ = json.Unmarshal(bruto, &out)
	return out
}

// Executar grava o plano. Deve rodar dentro de uma transação (q vindo de WithTx).
func (p Plano) Executar(ctx context.Context, q *db.Queries, importacaoID int64) error {
	if p.Previa.Totais.Erros > 0 {
		return fmt.Errorf("a prévia tem %d erro(s)", p.Previa.Totais.Erros)
	}
	origem := fmt.Sprintf("importacao:%d", importacaoID)
	ids := map[string]int64{}
	for n, id := range p.idsClientes {
		ids[n] = id
	}

	for _, c := range p.clientesNovos {
		id, err := q.CriarCliente(ctx, db.CriarClienteParams{
			Nome: c.nome, NomeNormalizado: c.normalizado,
			Telefone: textoOuNil(c.telefone), Email: textoOuNil(c.email), Origem: origem,
		})
		if err != nil {
			return fmt.Errorf("criando cliente %q: %w", c.nome, err)
		}
		ids[c.normalizado] = id
	}
	for _, c := range p.clientesAlterados {
		if err := q.AtualizarContatoCliente(ctx, db.AtualizarContatoClienteParams{
			ID: c.id, Telefone: textoOuNil(c.telefone), Email: textoOuNil(c.email),
		}); err != nil {
			return fmt.Errorf("atualizando cliente %q: %w", c.nome, err)
		}
	}

	for _, l := range p.cotasNovas {
		if _, err := q.InserirCota(ctx, db.InserirCotaParams{
			ClienteID: ids[NormalizarNome(l.Nome)], Administradora: l.Administradora,
			Grupo: l.Grupo, Cota: l.Cota, Versao: l.Versao,
			TipoConsorcio: textoOuNil(l.TipoConsorcio), DadosPlanilha: dadosJSON(l.DadosPlanilha),
			ImportacaoID: &importacaoID,
		}); err != nil {
			return fmt.Errorf("inserindo cota %s: %w", l.Tag(), err)
		}
	}
	for _, c := range p.cotasAlteradas {
		if err := q.AtualizarCotaImportada(ctx, db.AtualizarCotaImportadaParams{
			ID: c.id, ClienteID: ids[NormalizarNome(c.linha.Nome)],
			TipoConsorcio: textoOuNil(c.linha.TipoConsorcio), DadosPlanilha: dadosJSON(c.linha.DadosPlanilha),
			ImportacaoID: &importacaoID,
		}); err != nil {
			return fmt.Errorf("atualizando cota %s: %w", c.linha.Tag(), err)
		}
	}
	return nil
}

func textoOuNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func dadosJSON(m map[string]string) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	b, _ := json.Marshal(m)
	return b
}
