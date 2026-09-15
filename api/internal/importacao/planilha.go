// Package importacao lê a planilha de clientes e aplica no cadastro de clientes e cotas.
package importacao

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// MaxLinhas limita o tamanho de uma importação.
const MaxLinhas = 5000

// ErrPlanilhaVazia usa a mesma mensagem do csv.js do script legado.
var ErrPlanilhaVazia = errors.New("CSV vazio (nenhuma cota para processar).")

// Linha é uma linha válida da planilha, normalizada com as regras do csv.js
// (workers/canopus/src/csv.js): grupo/cota/versão só com dígitos e zeros à esquerda
// (6/4/2), versão padrão "00".
type Linha struct {
	Linha          int               `json:"linha"`
	Administradora string            `json:"administradora"`
	Grupo          string            `json:"grupo"`
	Cota           string            `json:"cota"`
	Versao         string            `json:"versao"`
	Nome           string            `json:"nome"`
	Telefone       string            `json:"telefone,omitempty"`
	Email          string            `json:"email,omitempty"`
	TipoConsorcio  string            `json:"tipo_consorcio,omitempty"`
	DadosPlanilha  map[string]string `json:"dados_planilha,omitempty"`
}

// Tag no formato usado no nome do PDF e nos logs do worker: 006650-0924-00.
func (l Linha) Tag() string { return l.Grupo + "-" + l.Cota + "-" + l.Versao }

// ErroLinha é um problema numa linha específica. A importação só é aplicada sem erros.
type ErroLinha struct {
	Linha    int    `json:"linha"`
	Mensagem string `json:"mensagem"`
}

// Colunas que viram campos próprios. "acesso a cota" é sempre descartada: o nome
// sugere senha de acesso e não queremos isso no banco.
var colunasConhecidas = map[string]bool{
	"administradora": true, "nome": true, "grupo": true, "cota": true, "versao": true,
	"tipo de consorcio": true, "telefone": true, "e-mail": true, "email": true,
	"acesso a cota": true,
}

// LerPlanilha interpreta o CSV. Diferenças intencionais em relação ao csv.js:
//   - devolve todos os erros de linha, em vez de parar no primeiro;
//   - o número da linha é o do arquivo (o csv.js conta só linhas não vazias);
//   - linhas com todas as colunas vazias (",,,,") são ignoradas, como linhas em branco.
//
// O erro de retorno é para problemas no arquivo inteiro (vazio, sem colunas, ilegível).
func LerPlanilha(conteudo []byte) ([]Linha, []ErroLinha, error) {
	conteudo = bytes.TrimPrefix(conteudo, []byte("\xef\xbb\xbf"))

	// Mesma detecção do csv.js: ";" na primeira linha define o separador.
	primeira := conteudo
	if i := bytes.IndexByte(conteudo, '\n'); i >= 0 {
		primeira = conteudo[:i]
	}
	r := csv.NewReader(bytes.NewReader(conteudo))
	r.Comma = ','
	if bytes.IndexByte(primeira, ';') >= 0 {
		r.Comma = ';'
	}
	r.FieldsPerRecord = -1

	cabecalho, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, nil, ErrPlanilhaVazia
	}
	if err != nil {
		return nil, nil, erroLeitura(err)
	}
	colunas := make([]string, len(cabecalho))
	temGrupo, temCota := false, false
	for i, h := range cabecalho {
		colunas[i] = normalizarCabecalho(h)
		temGrupo = temGrupo || colunas[i] == "grupo"
		temCota = temCota || colunas[i] == "cota"
	}

	var linhas []Linha
	var erros []ErroLinha
	for {
		registro, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, erroLeitura(err)
		}
		numero, _ := r.FieldPos(0)

		campos := make(map[string]string, len(colunas))
		vazia := true
		for i, col := range colunas {
			v := ""
			if i < len(registro) {
				v = strings.TrimSpace(registro[i])
			}
			campos[col] = v
			vazia = vazia && v == ""
		}
		if vazia {
			continue
		}
		if !temGrupo || !temCota {
			return nil, nil, fmt.Errorf(`CSV sem as colunas "grupo" e "cota" (cabeçalho lido: %s)`, strings.Join(unicos(colunas), ", "))
		}
		if len(linhas)+len(erros) >= MaxLinhas {
			return nil, nil, fmt.Errorf("a planilha tem mais de %d linhas", MaxLinhas)
		}

		versao := campos["versao"]
		if versao == "" {
			versao = "00"
		}
		l := Linha{
			Linha:          numero,
			Administradora: strings.ToUpper(campos["administradora"]),
			Grupo:          comZeros(digitos(campos["grupo"]), 6),
			Cota:           comZeros(digitos(campos["cota"]), 4),
			Versao:         comZeros(digitos(versao), 2),
			Nome:           campos["nome"],
			Telefone:       campos["telefone"],
			Email:          primeiroPreenchido(campos["e-mail"], campos["email"]),
			TipoConsorcio:  campos["tipo de consorcio"],
		}
		if l.Administradora == "" {
			l.Administradora = "CANOPUS" // formato legado (grupo,cota,versao) era só Canopus
		}
		if l.Grupo == "000000" || l.Cota == "0000" {
			erros = append(erros, ErroLinha{numero, fmt.Sprintf("grupo ou cota inválidos (grupo %q, cota %q)", campos["grupo"], campos["cota"])})
			continue
		}
		for col, v := range campos {
			if v != "" && !colunasConhecidas[col] {
				if l.DadosPlanilha == nil {
					l.DadosPlanilha = map[string]string{}
				}
				l.DadosPlanilha[col] = v
			}
		}
		linhas = append(linhas, l)
	}

	if len(linhas) == 0 && len(erros) == 0 {
		return nil, nil, ErrPlanilhaVazia
	}
	return linhas, erros, nil
}

// normalizarCabecalho: "CONTRATAÇÃO " → "contratacao" (mesma regra do csv.js).
func normalizarCabecalho(h string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(h) {
		if r >= 0x0300 && r <= 0x036f {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToLower(strings.TrimSpace(b.String()))
}

func digitos(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func comZeros(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat("0", n-len(s)) + s
}

func primeiroPreenchido(valores ...string) string {
	for _, v := range valores {
		if v != "" {
			return v
		}
	}
	return ""
}

func unicos(itens []string) []string {
	vistos := map[string]bool{}
	var out []string
	for _, i := range itens {
		if !vistos[i] {
			vistos[i] = true
			out = append(out, i)
		}
	}
	return out
}

func erroLeitura(err error) error {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		return fmt.Errorf("planilha ilegível na linha %d: %v", pe.Line, pe.Err)
	}
	return fmt.Errorf("planilha ilegível: %w", err)
}
