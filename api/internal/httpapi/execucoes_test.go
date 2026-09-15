package httpapi

// Testes de integração da fila de execuções (Etapa 2). Precisam de Postgres (make test).

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const tokenWorkerTeste = "token-de-teste-do-worker-com-mais-de-32-caracteres"

var pngFalso = append([]byte("\x89PNG\r\n\x1a\n"), []byte("conteudo")...)

type workerTeste struct {
	t    *testing.T
	h    http.Handler
	nome string
}

func (wt *workerTeste) post(caminho string, corpo any) *httptest.ResponseRecorder {
	wt.t.Helper()
	var leitor io.Reader = strings.NewReader("{}")
	switch c := corpo.(type) {
	case nil:
	case []byte:
		leitor = bytes.NewReader(c)
	default:
		b, _ := json.Marshal(c)
		leitor = bytes.NewReader(b)
	}
	r := httptest.NewRequest(http.MethodPost, caminho, leitor)
	r.Header.Set("Authorization", "Bearer "+tokenWorkerTeste)
	r.Header.Set("X-Worker", wt.nome)
	rec := httptest.NewRecorder()
	wt.h.ServeHTTP(rec, r)
	return rec
}

func (wt *workerTeste) proxima() (tarefaJSON, int) {
	wt.t.Helper()
	rec := wt.post("/internal/tarefas/proxima", map[string][]string{"tipos": {"dry_run"}})
	if rec.Code != http.StatusOK {
		return tarefaJSON{}, rec.Code
	}
	return decodificar[tarefaJSON](wt.t, rec), rec.Code
}

type detalheExecucao struct {
	Execucao struct {
		ID                     int64   `json:"id"`
		Status                 string  `json:"status"`
		PosicaoFila            *int32  `json:"posicao_fila"`
		CancelamentoSolicitado bool    `json:"cancelamento_solicitado"`
		Erro                   *string `json:"erro"`
	} `json:"execucao"`
	Totais map[string]int     `json:"totais"`
	Cotas  []cotaExecucaoJSON `json:"cotas"`
}

// cenarioExecucoes: operador logado, 4 cotas importadas da planilha fictícia e os dois handlers.
func cenarioExecucoes(t *testing.T) (*Servidor, *navegador, []cotaJSON, http.Handler) {
	t.Helper()
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)
	planilha, err := os.ReadFile("../importacao/testdata/paridade/01-planilha-clientes.csv")
	if err != nil {
		t.Fatal(err)
	}
	imp := decodificar[importacaoJSON](t, n.enviarPlanilha("clientes.csv", planilha))
	esperarStatus(t, n.enviar(http.MethodPost, fmt.Sprintf("/importacoes/%d/aplicar", imp.ID), nil, ""), http.StatusOK, "aplicar planilha")
	cotas := decodificar[struct{ Cotas []cotaJSON }](t, n.req(http.MethodGet, "/cotas", nil, nil)).Cotas
	return s, n, cotas, s.RotasInternas()
}

func idsDe(cotas []cotaJSON) []int64 {
	ids := make([]int64, len(cotas))
	for i, c := range cotas {
		ids[i] = c.ID
	}
	return ids
}

func (n *navegador) criarDryRun(t *testing.T, ids []int64) int64 {
	t.Helper()
	rec := n.enviarJSON(http.MethodPost, "/execucoes", map[string]any{"tipo": "dry_run", "cota_ids": ids})
	esperarStatus(t, rec, http.StatusCreated, "criar dry-run")
	return decodificar[struct{ ID int64 }](t, rec).ID
}

func (n *navegador) detalhe(t *testing.T, id int64) detalheExecucao {
	t.Helper()
	rec := n.req(http.MethodGet, fmt.Sprintf("/execucoes/%d", id), nil, nil)
	esperarStatus(t, rec, http.StatusOK, "detalhe da execução")
	return decodificar[detalheExecucao](t, rec)
}

func (s *Servidor) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func TestCriarExecucaoRegras(t *testing.T) {
	s, n, cotas, _ := cenarioExecucoes(t)
	h := s.Rotas()

	criarUsuario(t, s, "leitura@exemplo.com", db.PerfilUsuarioLeitura)
	leitura, _ := entrar(t, h, "leitura@exemplo.com", senhaTeste)
	esperarStatus(t, leitura.enviarJSON(http.MethodPost, "/execucoes", map[string]any{"tipo": "dry_run", "cota_ids": idsDe(cotas)}), http.StatusForbidden, "leitura cria execução")

	casos := []struct {
		nome   string
		corpo  map[string]any
		status int
	}{
		{"execução real", map[string]any{"tipo": "real", "cota_ids": idsDe(cotas)}, http.StatusUnprocessableEntity},
		{"sem cotas", map[string]any{"tipo": "dry_run", "cota_ids": []int64{}}, http.StatusUnprocessableEntity},
		{"cota inexistente", map[string]any{"tipo": "dry_run", "cota_ids": []int64{cotas[0].ID, 999999}}, http.StatusUnprocessableEntity},
	}
	for _, c := range casos {
		esperarStatus(t, n.enviarJSON(http.MethodPost, "/execucoes", c.corpo), c.status, c.nome)
	}
	esperarStatus(t, n.enviarJSON(http.MethodPatch, fmt.Sprintf("/cotas/%d", cotas[0].ID), map[string]bool{"ativa": false}), http.StatusOK, "desativar cota")
	esperarStatus(t, n.enviarJSON(http.MethodPost, "/execucoes", map[string]any{"tipo": "dry_run", "cota_ids": idsDe(cotas[:2])}), http.StatusUnprocessableEntity, "cota inativa")

	id := n.criarDryRun(t, []int64{cotas[2].ID, cotas[1].ID, cotas[1].ID})
	d := n.detalhe(t, id)
	if d.Execucao.Status != "na_fila" || d.Execucao.PosicaoFila == nil || *d.Execucao.PosicaoFila != 1 || len(d.Cotas) != 2 {
		t.Fatalf("detalhe: %+v", d)
	}
	lista := decodificar[struct{ Execucoes []execucaoResumoJSON }](t, n.req(http.MethodGet, "/execucoes", nil, nil)).Execucoes
	if len(lista) != 1 || lista[0].Total != 2 || lista[0].Restantes != 2 {
		t.Errorf("lista: %+v", lista)
	}
	if contarAuditoria(t, s, "execucao_criada") != 1 {
		t.Error("criação não auditada")
	}
}

func TestRotasInternasExigemToken(t *testing.T) {
	s := novoServidorTeste(t)
	interno := s.RotasInternas()

	semToken := httptest.NewRecorder()
	interno.ServeHTTP(semToken, httptest.NewRequest(http.MethodPost, "/internal/tarefas/proxima", strings.NewReader(`{"tipos":["dry_run"]}`)))
	esperarStatus(t, semToken, http.StatusUnauthorized, "sem token")

	r := httptest.NewRequest(http.MethodPost, "/internal/tarefas/proxima", strings.NewReader(`{"tipos":["dry_run"]}`))
	r.Header.Set("Authorization", "Bearer outro-token-qualquer-com-mais-de-32-caracteres")
	tokenErrado := httptest.NewRecorder()
	interno.ServeHTTP(tokenErrado, r)
	esperarStatus(t, tokenErrado, http.StatusUnauthorized, "token errado")

	r = httptest.NewRequest(http.MethodPost, "/internal/tarefas/proxima", strings.NewReader(`{"tipos":["dry_run"]}`))
	r.Header.Set("Authorization", "Bearer "+tokenWorkerTeste)
	semNome := httptest.NewRecorder()
	interno.ServeHTTP(semNome, r)
	esperarStatus(t, semNome, http.StatusBadRequest, "sem X-Worker")

	publico := httptest.NewRecorder()
	s.Rotas().ServeHTTP(publico, httptest.NewRequest(http.MethodPost, "/internal/tarefas/proxima", nil))
	esperarStatus(t, publico, http.StatusNotFound, "rota interna na porta pública")

	vazio, codigo := (&workerTeste{t: t, h: interno, nome: "w1"}).proxima()
	if codigo != http.StatusNoContent || vazio.Execucao.ID != 0 {
		t.Errorf("fila vazia: %d", codigo)
	}
}

func TestFluxoDryRunPeloWorker(t *testing.T) {
	_, n, cotas, interno := cenarioExecucoes(t)
	w1 := &workerTeste{t: t, h: interno, nome: "worker-1"}
	w2 := &workerTeste{t: t, h: interno, nome: "worker-2"}

	id := n.criarDryRun(t, idsDe(cotas))
	segunda := n.criarDryRun(t, idsDe(cotas[:1]))

	tarefa, codigo := w1.proxima()
	if codigo != http.StatusOK || tarefa.Execucao.ID != id || tarefa.Execucao.Tipo != "dry_run" || len(tarefa.Cotas) != 4 {
		t.Fatalf("tarefa: %d %+v", codigo, tarefa)
	}
	if _, codigo := w2.proxima(); codigo != http.StatusNoContent {
		t.Fatalf("segunda execução começou em paralelo com a mesma credencial: %d", codigo)
	}
	c := tarefa.Cotas
	rota := func(ec cotaExecucaoJSON, acao string) string {
		return fmt.Sprintf("/internal/execucao-cotas/%d/%s", ec.ID, acao)
	}

	esperarStatus(t, w1.post(rota(c[0], "iniciar"), nil), http.StatusNoContent, "iniciar cota 1")
	esperarStatus(t, w1.post(rota(c[0], "iniciar"), nil), http.StatusConflict, "iniciar cota 1 de novo")
	esperarStatus(t, w2.post(rota(c[1], "iniciar"), nil), http.StatusConflict, "outro worker inicia cota")

	esperarStatus(t, w1.post(rota(c[0], "screenshot"), []byte("não é png")), http.StatusUnsupportedMediaType, "screenshot inválido")
	shot := w1.post(rota(c[0], "screenshot"), pngFalso)
	esperarStatus(t, shot, http.StatusCreated, "screenshot")
	for _, proibido := range []string{"confirmada", "confirmacao_iniciada", "erro_apos_confirmar"} {
		esperarStatus(t, w1.post(rota(c[0], "concluir"), map[string]any{"status": proibido}), http.StatusUnprocessableEntity, "dry-run concluindo como "+proibido)
	}
	esperarStatus(t, w1.post(rota(c[0], "concluir"), map[string]any{"status": "verificada", "detalhes": map[string]any{"lances_nesta_assembleia": 1}}), http.StatusNoContent, "concluir verificada")

	esperarStatus(t, w1.post(rota(c[1], "iniciar"), nil), http.StatusNoContent, "iniciar cota 2")
	esperarStatus(t, w1.post(rota(c[1], "concluir"), map[string]any{"status": "erro_antes_confirmar"}), http.StatusUnprocessableEntity, "erro sem mensagem")
	esperarStatus(t, w1.post(rota(c[1], "concluir"), map[string]any{"status": "erro_antes_confirmar", "erro_tipo": "conhecido", "erro": `"2º Fixo" está desabilitado`}), http.StatusNoContent, "concluir com erro")
	for _, ec := range c[2:] {
		esperarStatus(t, w1.post(rota(ec, "iniciar"), nil), http.StatusNoContent, "iniciar")
		esperarStatus(t, w1.post(rota(ec, "concluir"), map[string]any{"status": "verificada"}), http.StatusNoContent, "concluir")
	}
	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucoes/%d/eventos", id), map[string]any{"eventos": []map[string]any{
		{"nivel": "aviso", "mensagem": "Aviso do Newcon (confirm): teste", "execucao_cota_id": c[0].ID},
		{"nivel": "info", "mensagem": "   "},
	}}), http.StatusNoContent, "eventos")
	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucoes/%d/renovar", id), nil), http.StatusOK, "renovar")

	fim := w1.post(fmt.Sprintf("/internal/execucoes/%d/finalizar", id), map[string]any{})
	esperarStatus(t, fim, http.StatusOK, "finalizar")
	if got := decodificar[map[string]string](t, fim)["status"]; got != "concluida_com_erros" {
		t.Fatalf("situação final = %q", got)
	}

	d := n.detalhe(t, id)
	if d.Execucao.Status != "concluida_com_erros" || d.Totais["verificada"] != 3 || d.Totais["erro_antes_confirmar"] != 1 {
		t.Fatalf("detalhe: %+v %+v", d.Execucao, d.Totais)
	}
	if d.Cotas[1].ErroTipo == nil || *d.Cotas[1].ErroTipo != "conhecido" || d.Cotas[0].ScreenshotID == nil {
		t.Errorf("cotas: %+v", d.Cotas[:2])
	}
	arquivo := n.req(http.MethodGet, "/arquivos/"+*d.Cotas[0].ScreenshotID, nil, nil)
	esperarStatus(t, arquivo, http.StatusOK, "baixar screenshot")
	if arquivo.Header().Get("Content-Type") != "image/png" || !bytes.Equal(arquivo.Body.Bytes(), pngFalso) {
		t.Error("screenshot diferente do enviado")
	}
	esperarStatus(t, n.req(http.MethodGet, "/arquivos/nao-e-uuid", nil, nil), http.StatusNotFound, "arquivo inválido")

	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucoes/%d/renovar", id), nil), http.StatusConflict, "renovar depois do fim")
	if tarefa2, codigo := w2.proxima(); codigo != http.StatusOK || tarefa2.Execucao.ID != segunda {
		t.Fatalf("segunda execução não começou depois da primeira: %d", codigo)
	}
}

func TestCancelarExecucao(t *testing.T) {
	s, n, cotas, interno := cenarioExecucoes(t)
	w1 := &workerTeste{t: t, h: interno, nome: "worker-1"}

	naFila := n.criarDryRun(t, idsDe(cotas))
	esperarStatus(t, n.enviar(http.MethodPost, fmt.Sprintf("/execucoes/%d/cancelar", naFila), nil, ""), http.StatusOK, "cancelar na fila")
	d := n.detalhe(t, naFila)
	if d.Execucao.Status != "cancelada" || d.Totais["cancelada"] != 4 {
		t.Fatalf("cancelada na fila: %+v %+v", d.Execucao, d.Totais)
	}
	esperarStatus(t, n.enviar(http.MethodPost, fmt.Sprintf("/execucoes/%d/cancelar", naFila), nil, ""), http.StatusConflict, "cancelar de novo")

	id := n.criarDryRun(t, idsDe(cotas))
	tarefa, _ := w1.proxima()
	c := tarefa.Cotas
	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucao-cotas/%d/iniciar", c[0].ID), nil), http.StatusNoContent, "iniciar")
	cancelar := n.enviar(http.MethodPost, fmt.Sprintf("/execucoes/%d/cancelar", id), nil, "")
	if decodificar[map[string]any](t, cancelar)["status"] != "cancelando" {
		t.Fatalf("cancelar em andamento: %s", cancelar.Body.String())
	}
	// A cota atual termina; a próxima não começa.
	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucao-cotas/%d/concluir", c[0].ID), map[string]any{"status": "verificada"}), http.StatusNoContent, "concluir a cota atual")
	proxima := w1.post(fmt.Sprintf("/internal/execucao-cotas/%d/iniciar", c[1].ID), nil)
	if proxima.Code != http.StatusConflict || !decodificar[respostaConflito](t, proxima).Cancelada {
		t.Fatalf("iniciar depois do cancelamento: %d %s", proxima.Code, proxima.Body.String())
	}
	renovar := w1.post(fmt.Sprintf("/internal/execucoes/%d/renovar", id), nil)
	if !decodificar[map[string]bool](t, renovar)["cancelamento_solicitado"] {
		t.Error("renovar não avisou o cancelamento")
	}
	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucoes/%d/finalizar", id), map[string]any{}), http.StatusOK, "finalizar")
	d = n.detalhe(t, id)
	if d.Execucao.Status != "cancelada" || d.Totais["verificada"] != 1 || d.Totais["cancelada"] != 3 {
		t.Fatalf("cancelada em andamento: %+v %+v", d.Execucao, d.Totais)
	}
	if contarAuditoria(t, s, "execucao_cancelada") != 2 {
		t.Error("cancelamentos não auditados")
	}
}

func TestTravaVencidaDevolveParaFila(t *testing.T) {
	s, n, cotas, interno := cenarioExecucoes(t)
	w1 := &workerTeste{t: t, h: interno, nome: "worker-1"}
	w2 := &workerTeste{t: t, h: interno, nome: "worker-2"}

	id := n.criarDryRun(t, idsDe(cotas))
	tarefa, _ := w1.proxima()
	c := tarefa.Cotas
	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucao-cotas/%d/iniciar", c[0].ID), nil), http.StatusNoContent, "iniciar")

	s.exec(t, "UPDATE execucoes SET trava_ate = now() - interval '1 minute' WHERE id = $1", id)

	retomada, codigo := w2.proxima()
	if codigo != http.StatusOK || retomada.Execucao.ID != id || retomada.Cotas[0].Status != "pendente" {
		t.Fatalf("retomada: %d %+v", codigo, retomada)
	}
	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucoes/%d/renovar", id), nil), http.StatusConflict, "worker antigo renova")
	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucao-cotas/%d/iniciar", c[1].ID), nil), http.StatusConflict, "worker antigo inicia")
	esperarStatus(t, w2.post(fmt.Sprintf("/internal/execucao-cotas/%d/iniciar", c[0].ID), nil), http.StatusNoContent, "worker novo inicia")
}

// A regra da Etapa 3 já vale: cota que iniciou a confirmação nunca volta a ser processada.
func TestConfirmacaoIniciadaNuncaERepetida(t *testing.T) {
	s, n, cotas, interno := cenarioExecucoes(t)
	w1 := &workerTeste{t: t, h: interno, nome: "worker-1"}
	w2 := &workerTeste{t: t, h: interno, nome: "worker-2"}

	id := n.criarDryRun(t, idsDe(cotas))
	tarefa, _ := w1.proxima()
	c := tarefa.Cotas
	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucao-cotas/%d/iniciar", c[0].ID), nil), http.StatusNoContent, "iniciar")
	// Simula o que a Etapa 3 fará antes de clicar em Confirmar.
	s.exec(t, "UPDATE execucao_cotas SET status = 'confirmacao_iniciada' WHERE id = $1", c[0].ID)
	s.exec(t, "UPDATE execucoes SET trava_ate = now() - interval '1 minute' WHERE id = $1", id)

	retomada, _ := w2.proxima()
	if retomada.Cotas[0].Status != "erro_apos_confirmar" {
		t.Fatalf("cota em confirmação depois da queda do worker: %q", retomada.Cotas[0].Status)
	}
	d := n.detalhe(t, id)
	if d.Cotas[0].Erro == nil || !strings.Contains(*d.Cotas[0].Erro, "confira no Histórico") {
		t.Errorf("erro da cota: %v", d.Cotas[0].Erro)
	}
	esperarStatus(t, w2.post(fmt.Sprintf("/internal/execucao-cotas/%d/iniciar", c[0].ID), nil), http.StatusConflict, "reiniciar cota que passou pela confirmação")

	for _, destino := range []string{"pendente", "em_andamento", "erro_antes_confirmar"} {
		_, err := s.pool.Exec(context.Background(), "UPDATE execucao_cotas SET status = $1 WHERE id = $2", destino, c[0].ID)
		if err == nil || !strings.Contains(err.Error(), "já passou pela confirmação") {
			t.Errorf("trigger deixou erro_apos_confirmar → %s: %v", destino, err)
		}
	}
	s.exec(t, "UPDATE execucao_cotas SET status = 'confirmacao_iniciada' WHERE id = $1", c[1].ID)
	_, err := s.pool.Exec(context.Background(), "UPDATE execucao_cotas SET status = 'pendente' WHERE id = $1", c[1].ID)
	if err == nil || !strings.Contains(err.Error(), "já iniciou a confirmação") {
		t.Errorf("trigger deixou confirmacao_iniciada → pendente: %v", err)
	}
}

func TestLiberarNoDesligamentoDoWorker(t *testing.T) {
	_, n, cotas, interno := cenarioExecucoes(t)
	w1 := &workerTeste{t: t, h: interno, nome: "worker-1"}
	w2 := &workerTeste{t: t, h: interno, nome: "worker-2"}

	id := n.criarDryRun(t, idsDe(cotas))
	tarefa, _ := w1.proxima()
	c := tarefa.Cotas
	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucao-cotas/%d/iniciar", c[0].ID), nil), http.StatusNoContent, "iniciar")
	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucao-cotas/%d/concluir", c[0].ID), map[string]any{"status": "verificada"}), http.StatusNoContent, "concluir")
	esperarStatus(t, w1.post(fmt.Sprintf("/internal/execucoes/%d/liberar", id), nil), http.StatusNoContent, "liberar")

	if d := n.detalhe(t, id); d.Execucao.Status != "na_fila" {
		t.Fatalf("situação depois de liberar: %q", d.Execucao.Status)
	}
	retomada, codigo := w2.proxima()
	if codigo != http.StatusOK || retomada.Cotas[0].Status != "verificada" || retomada.Cotas[1].Status != "pendente" {
		t.Fatalf("retomada: %d %+v", codigo, retomada.Cotas[:2])
	}
}

func TestEventosPorSSE(t *testing.T) {
	s, n, cotas, interno := cenarioExecucoes(t)
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	go s.hub.Rodar(ctx)
	servidor := httptest.NewServer(s.Rotas())
	defer servidor.Close()

	id := n.criarDryRun(t, idsDe(cotas[:1]))

	abrir := func(ultimo string) (<-chan string, func()) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/execucoes/%d/eventos", servidor.URL, id), nil)
		req.AddCookie(n.cookie)
		if ultimo != "" {
			req.Header.Set("Last-Event-ID", ultimo)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("SSE: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		linhas := make(chan string, 100)
		go func() {
			defer close(linhas)
			leitor := bufio.NewScanner(resp.Body)
			for leitor.Scan() {
				linhas <- leitor.Text()
			}
		}()
		return linhas, func() { resp.Body.Close() }
	}
	esperarLinha := func(linhas <-chan string, trecho string) string {
		t.Helper()
		limite := time.After(10 * time.Second)
		for {
			select {
			case l, ok := <-linhas:
				if !ok {
					t.Fatalf("stream fechou antes de %q", trecho)
				}
				if strings.Contains(l, trecho) {
					return l
				}
			case <-limite:
				t.Fatalf("não chegou %q em 10 s", trecho)
			}
		}
	}

	linhas, fechar := abrir("")
	defer fechar()
	primeiroID := strings.TrimPrefix(esperarLinha(linhas, "id: "), "id: ")
	esperarLinha(linhas, "Dry-run criado")

	w := &workerTeste{t: t, h: interno, nome: "worker-sse"}
	tarefa, _ := w.proxima()
	esperarLinha(linhas, "Worker worker-sse começou") // chega pelo NOTIFY do Postgres

	retomada, fecharRetomada := abrir(primeiroID)
	defer fecharRetomada()
	if l := esperarLinha(retomada, "data: "); strings.Contains(l, "Dry-run criado") {
		t.Error("Last-Event-ID não foi respeitado: evento repetido")
	}

	esperarStatus(t, w.post(fmt.Sprintf("/internal/execucao-cotas/%d/iniciar", tarefa.Cotas[0].ID), nil), http.StatusNoContent, "iniciar")
	esperarStatus(t, w.post(fmt.Sprintf("/internal/execucao-cotas/%d/concluir", tarefa.Cotas[0].ID), map[string]any{"status": "verificada"}), http.StatusNoContent, "concluir")
	esperarStatus(t, w.post(fmt.Sprintf("/internal/execucoes/%d/finalizar", id), map[string]any{}), http.StatusOK, "finalizar")
	esperarLinha(linhas, "Execução concluída")
	esperarLinha(linhas, "event: fim")
}
