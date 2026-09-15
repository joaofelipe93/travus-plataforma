package importacao

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestParidadeComCsvJs compara com o resultado do csv.js ORIGINAL (script legado) sobre
// planilhas fictícias. Os .esperado.json são gerados por `make paridade-csv`.
func TestParidadeComCsvJs(t *testing.T) {
	arquivos, _ := filepath.Glob("testdata/paridade/*.csv")
	if len(arquivos) == 0 {
		t.Fatal("nenhuma planilha em testdata/paridade")
	}
	linhaDoErro := regexp.MustCompile(`^Linha (\d+) do CSV inválida`)

	for _, arquivo := range arquivos {
		t.Run(filepath.Base(arquivo), func(t *testing.T) {
			conteudo, err := os.ReadFile(arquivo)
			if err != nil {
				t.Fatal(err)
			}
			bruto, err := os.ReadFile(strings.TrimSuffix(arquivo, ".csv") + ".esperado.json")
			if err != nil {
				t.Fatalf("sem resultado esperado (rode make paridade-csv): %v", err)
			}
			var esperado struct {
				Erro  string `json:"erro"`
				Cotas []struct {
					Grupo, Cota, Versao, Nome string
				} `json:"cotas"`
			}
			if err := json.Unmarshal(bruto, &esperado); err != nil {
				t.Fatal(err)
			}

			linhas, errosLinha, err := LerPlanilha(conteudo)

			if esperado.Erro != "" {
				if m := linhaDoErro.FindStringSubmatch(esperado.Erro); m != nil {
					// Erro numa linha: o csv.js para no primeiro; aqui ele tem que aparecer.
					numero, _ := strconv.Atoi(m[1])
					if err != nil || len(errosLinha) == 0 || errosLinha[0].Linha != numero {
						t.Fatalf("csv.js: %q; Go: err=%v erros=%v", esperado.Erro, err, errosLinha)
					}
					return
				}
				if err == nil || err.Error() != esperado.Erro {
					t.Fatalf("erro diferente.\n csv.js: %q\n Go:     %v", esperado.Erro, err)
				}
				return
			}

			if err != nil || len(errosLinha) > 0 {
				t.Fatalf("csv.js aceitou, Go recusou: err=%v erros=%v", err, errosLinha)
			}
			if len(linhas) != len(esperado.Cotas) {
				t.Fatalf("csv.js leu %d cotas, Go leu %d", len(esperado.Cotas), len(linhas))
			}
			for i, e := range esperado.Cotas {
				g := linhas[i]
				if g.Grupo != e.Grupo || g.Cota != e.Cota || g.Versao != e.Versao || g.Nome != e.Nome {
					t.Errorf("cota %d: csv.js %s-%s-%s %q; Go %s %q", i, e.Grupo, e.Cota, e.Versao, e.Nome, g.Tag(), g.Nome)
				}
			}
		})
	}
}

func TestLinhasVaziasENumeroDaLinhaNoArquivo(t *testing.T) {
	csv := "grupo,cota,nome\n6650,924,A\n,,\n\n6660,1488,B\n"
	linhas, erros, err := LerPlanilha([]byte(csv))
	if err != nil || len(erros) > 0 {
		t.Fatalf("err=%v erros=%v", err, erros)
	}
	if len(linhas) != 2 || linhas[0].Linha != 2 || linhas[1].Linha != 5 {
		t.Fatalf("linhas inesperadas: %+v", linhas)
	}
}

func TestTodosOsErrosDeLinha(t *testing.T) {
	csv := "grupo,cota\n0,1\n6650,924\n6650,0\n"
	linhas, erros, err := LerPlanilha([]byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(linhas) != 1 || len(erros) != 2 || erros[0].Linha != 2 || erros[1].Linha != 4 {
		t.Fatalf("linhas=%+v erros=%+v", linhas, erros)
	}
}

func TestCamposDaPlataforma(t *testing.T) {
	csv := "ADMINISTRADORA,DIA DA ASSEMBLEIA,CONTRATAÇÃO,NOME,GRUPO,COTA,TIPO DE CONSÓRCIO,ACESSO A COTA ,TELEFONE,E-MAIL\n" +
		"canopus,15,10/1/2025,CLIENTE EXEMPLO,6650,924,IMÓVEL,senha-secreta,(11) 90000-0000,cliente@exemplo.com\n"
	linhas, _, err := LerPlanilha([]byte(csv))
	if err != nil || len(linhas) != 1 {
		t.Fatalf("err=%v linhas=%v", err, linhas)
	}
	l := linhas[0]
	if l.Administradora != "CANOPUS" || l.TipoConsorcio != "IMÓVEL" || l.Telefone != "(11) 90000-0000" || l.Email != "cliente@exemplo.com" {
		t.Errorf("campos: %+v", l)
	}
	if len(l.DadosPlanilha) != 2 || l.DadosPlanilha["dia da assembleia"] != "15" || l.DadosPlanilha["contratacao"] != "10/1/2025" {
		t.Errorf("dados_planilha: %v", l.DadosPlanilha)
	}
	for _, v := range l.DadosPlanilha {
		if strings.Contains(v, "senha") {
			t.Error("a coluna ACESSO A COTA não pode ser guardada")
		}
	}

	legado, _, _ := LerPlanilha([]byte("grupo,cota,versao\n6650,924,\n"))
	if legado[0].Administradora != "CANOPUS" || legado[0].Versao != "00" {
		t.Errorf("formato legado: %+v", legado[0])
	}
}

func TestLimiteDeLinhas(t *testing.T) {
	var b strings.Builder
	b.WriteString("grupo,cota\n")
	for i := 1; i <= MaxLinhas+1; i++ {
		b.WriteString("6650," + strconv.Itoa(i) + "\n")
	}
	if _, _, err := LerPlanilha([]byte(b.String())); err == nil {
		t.Fatal("aceitou planilha acima do limite")
	}
}
