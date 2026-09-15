package httpapi

// Testes de integração da Etapa 3: aprovação de lance real, confirmação pelo worker,
// reimpressão de comprovante e fila do Google Drive. Precisam de Postgres (make test).
// Nada aqui fala com o Newcon nem com o Google: o worker e o Drive são simulados.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/cripto"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
	"github.com/joaofelipe93/travus-plataforma/api/internal/drive"
)

const assembleiaTeste = "15/09/2026"

var pdfFalso = []byte("%PDF-1.4\nconteudo de teste\n%%EOF")

func (wt *workerTeste) proximaDo(tipos ...string) (tarefaJSON, int) {
	wt.t.Helper()
	rec := wt.post("/internal/tarefas/proxima", map[string][]string{"tipos": tipos})
	if rec.Code != http.StatusOK {
		return tarefaJSON{}, rec.Code
	}
	return decodificar[tarefaJSON](wt.t, rec), rec.Code
}

func rotaCota(id int64, acao string) string {
	return fmt.Sprintf("/internal/execucao-cotas/%d/%s", id, acao)
}

type detalheComLances struct {
	Execucao struct {
		ID             int64  `json:"id"`
		Tipo           string `json:"tipo"`
		Status         string `json:"status"`
		DryRunOrigemID *int64 `json:"dry_run_origem_id"`
	} `json:"execucao"`
	Cotas  []cotaExecucaoJSON  `json:"cotas"`
	Lances []lanceExecucaoJSON `json:"lances"`
}

func (n *navegador) detalheComLances(t *testing.T, id int64) detalheComLances {
	t.Helper()
	rec := n.req(http.MethodGet, fmt.Sprintf("/execucoes/%d", id), nil, nil)
	esperarStatus(t, rec, http.StatusOK, "detalhe da execução")
	return decodificar[detalheComLances](t, rec)
}

func (n *navegador) lancesDoCliente(t *testing.T, clienteID int64) []lanceClienteJSON {
	t.Helper()
	rec := n.req(http.MethodGet, fmt.Sprintf("/clientes/%d", clienteID), nil, nil)
	esperarStatus(t, rec, http.StatusOK, "cliente")
	return decodificar[struct{ Lances []lanceClienteJSON }](t, rec).Lances
}

func (n *navegador) revisao(t *testing.T, dryRun int64) revisaoJSON {
	t.Helper()
	rec := n.req(http.MethodGet, fmt.Sprintf("/execucoes/%d/revisao", dryRun), nil, nil)
	esperarStatus(t, rec, http.StatusOK, "revisão")
	return decodificar[revisaoJSON](t, rec)
}

func escolha(execucaoCotaID int64, mesmoComLance bool) map[string]any {
	return map[string]any{"execucao_cota_id": execucaoCotaID, "registrar_mesmo_com_lance": mesmoComLance}
}

func aprovar(n *navegador, dryRun int64, quantidade int, escolhas ...map[string]any) *httptest.ResponseRecorder {
	return n.enviarJSON(http.MethodPost, "/execucoes/reais", map[string]any{
		"dry_run_id": dryRun, "cotas": escolhas, "quantidade_confirmada": quantidade,
	})
}

func contarExecucoes(t *testing.T, s *Servidor, tipo string) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM execucoes WHERE tipo = $1", tipo).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func entrarComo(t *testing.T, s *Servidor, email string, perfil db.PerfilUsuario) *navegador {
	t.Helper()
	criarUsuario(t, s, email, perfil)
	n, rec := entrar(t, s.Rotas(), email, senhaTeste)
	esperarStatus(t, rec, http.StatusOK, "login "+email)
	return n
}

// dryRunRevisado: dry-run das 3 primeiras cotas processado pelo worker. Na ordem da execução:
// cota 1 pronta; cota 2 pronta, mas já com lance nesta assembleia no Histórico; cota 3 com erro.
func dryRunRevisado(t *testing.T, n *navegador, interno http.Handler, cotas []cotaJSON) (int64, []cotaExecucaoJSON) {
	t.Helper()
	w := &workerTeste{t: t, h: interno, nome: "worker-dry"}
	id := n.criarDryRun(t, idsDe(cotas[:3]))
	tarefa, codigo := w.proxima()
	if codigo != http.StatusOK || tarefa.Execucao.ID != id {
		t.Fatalf("dry-run não saiu da fila: %d", codigo)
	}
	detalhes := func(lances int) map[string]any {
		d := map[string]any{
			"assembleia_data": assembleiaTeste, "assembleia_numero": "0123", "percentual_segundo_fixo": "25,0000",
			"historico_lido": true, "lances_nesta_assembleia": lances, "lances": []map[string]any{},
		}
		if lances > 0 {
			d["lances"] = []map[string]any{{"protocolo": "1788714", "assembleia": assembleiaTeste}}
		}
		return d
	}
	for i, ec := range tarefa.Cotas {
		esperarStatus(t, w.post(rotaCota(ec.ID, "iniciar"), nil), http.StatusNoContent, "iniciar")
		switch i {
		case 0:
			// Dry-run nunca confirma: as rotas do lance real recusam.
			esperarStatus(t, w.post(rotaCota(ec.ID, "confirmacao-iniciada"), map[string]string{"assembleia_data": assembleiaTeste}), http.StatusUnprocessableEntity, "dry-run marcando confirmação")
			esperarStatus(t, w.post(rotaCota(ec.ID, "concluir-confirmacao"), map[string]any{"status": "confirmada", "protocolo": "1"}), http.StatusUnprocessableEntity, "dry-run concluindo confirmação")
			esperarStatus(t, w.post(rotaCota(ec.ID, "pdf"), pdfFalso), http.StatusUnprocessableEntity, "dry-run enviando PDF")
			esperarStatus(t, w.post(rotaCota(ec.ID, "concluir"), map[string]any{"status": "verificada", "detalhes": detalhes(0)}), http.StatusNoContent, "verificada")
		case 1:
			esperarStatus(t, w.post(rotaCota(ec.ID, "concluir"), map[string]any{"status": "verificada", "detalhes": detalhes(1)}), http.StatusNoContent, "verificada com lance")
		default:
			esperarStatus(t, w.post(rotaCota(ec.ID, "concluir"), map[string]any{"status": "erro_antes_confirmar", "erro_tipo": "conhecido", "erro": "grupo sem assembleia"}), http.StatusNoContent, "erro")
		}
	}
	esperarStatus(t, w.post(fmt.Sprintf("/internal/execucoes/%d/finalizar", id), map[string]any{}), http.StatusOK, "finalizar dry-run")
	return id, tarefa.Cotas
}

func TestAprovacaoDeLanceReal(t *testing.T) {
	s, operador, cotas, interno := cenarioExecucoes(t)
	admin := entrarComo(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	dryRun, c := dryRunRevisado(t, operador, interno, cotas)

	rv := operador.revisao(t, dryRun)
	if rv.PodeAprovar || rv.LanceRealHabilitado || !strings.Contains(rv.Bloqueio, "LANCE_REAL_HABILITADO") || len(rv.Cotas) != 2 {
		t.Fatalf("revisão com a chave desligada: %+v", rv)
	}
	if rv.Cotas[0].ExigeAutorizacao || !rv.Cotas[1].ExigeAutorizacao || rv.Cotas[0].AssembleiaData == nil || *rv.Cotas[0].AssembleiaData != assembleiaTeste {
		t.Errorf("cotas da revisão: %+v", rv.Cotas)
	}

	pedido := []map[string]any{escolha(c[0].ID, false), escolha(c[1].ID, true)}
	esperarStatus(t, aprovar(operador, dryRun, 2, pedido...), http.StatusForbidden, "operador aprova lance real")
	desligada := aprovar(admin, dryRun, 2, pedido...)
	esperarStatus(t, desligada, http.StatusForbidden, "aprovação com a chave desligada")
	if !strings.Contains(desligada.Body.String(), "LANCE_REAL_HABILITADO") {
		t.Errorf("mensagem da chave desligada: %s", desligada.Body.String())
	}

	s.cfg.LanceRealHabilitado = true
	if rv := admin.revisao(t, dryRun); !rv.PodeAprovar || rv.Bloqueio != "" {
		t.Fatalf("revisão com a chave ligada: %+v", rv)
	}
	casos := []struct {
		nome       string
		quantidade int
		escolhas   []map[string]any
	}{
		{"quantidade digitada errada", 3, pedido},
		{"sem cotas", 0, nil},
		{"lance existente sem autorização", 2, []map[string]any{escolha(c[0].ID, false), escolha(c[1].ID, false)}},
		{"cota com erro no dry-run", 1, []map[string]any{escolha(c[2].ID, false)}},
		{"cota repetida", 2, []map[string]any{escolha(c[0].ID, false), escolha(c[0].ID, false)}},
	}
	for _, caso := range casos {
		esperarStatus(t, aprovar(admin, dryRun, caso.quantidade, caso.escolhas...), http.StatusUnprocessableEntity, caso.nome)
	}
	if n := contarExecucoes(t, s, "real"); n != 0 {
		t.Fatalf("aprovação recusada deixou %d execução(ões) real(is)", n)
	}

	criada := aprovar(admin, dryRun, 2, pedido...)
	esperarStatus(t, criada, http.StatusCreated, "aprovar")
	realID := decodificar[struct{ ID int64 }](t, criada).ID
	d := admin.detalheComLances(t, realID)
	if d.Execucao.Tipo != "real" || d.Execucao.Status != "na_fila" || d.Execucao.DryRunOrigemID == nil || *d.Execucao.DryRunOrigemID != dryRun || len(d.Cotas) != 2 {
		t.Fatalf("execução real: %+v", d)
	}
	if d.Cotas[0].AssembleiaAprovada == nil || *d.Cotas[0].AssembleiaAprovada != assembleiaTeste || d.Cotas[0].PermitirLanceExistente || !d.Cotas[1].PermitirLanceExistente {
		t.Errorf("cotas da execução real: %+v", d.Cotas)
	}
	esperarStatus(t, aprovar(admin, dryRun, 2, pedido...), http.StatusConflict, "aprovar o mesmo dry-run de novo")
	if contarAuditoria(t, s, "execucao_real_aprovada") != 1 {
		t.Error("aprovação não auditada")
	}

	// Prazo: dry-run terminado há mais de 2 h não pode ser aprovado.
	velho, cv := dryRunRevisado(t, operador, interno, cotas)
	s.exec(t, "UPDATE execucoes SET finalizada_em = now() - interval '3 hours' WHERE id = $1", velho)
	expirado := aprovar(admin, velho, 1, escolha(cv[0].ID, false))
	esperarStatus(t, expirado, http.StatusConflict, "dry-run fora do prazo")
	if !strings.Contains(expirado.Body.String(), "prazo") {
		t.Errorf("mensagem do prazo: %s", expirado.Body.String())
	}
}

func TestLanceRealPeloWorker(t *testing.T) {
	s, operador, cotas, interno := cenarioExecucoes(t)
	admin := entrarComo(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	s.cfg.LanceRealHabilitado = true
	dryRun, c := dryRunRevisado(t, operador, interno, cotas)
	criada := aprovar(admin, dryRun, 2, escolha(c[0].ID, false), escolha(c[1].ID, true))
	esperarStatus(t, criada, http.StatusCreated, "aprovar")
	realID := decodificar[struct{ ID int64 }](t, criada).ID

	w := &workerTeste{t: t, h: interno, nome: "worker-real"}
	s.cfg.LanceRealHabilitado = false
	if _, codigo := w.proximaDo("dry_run", "reimpressao", "real"); codigo != http.StatusNoContent {
		t.Fatalf("a API entregou execução real com a chave desligada: %d", codigo)
	}
	s.cfg.LanceRealHabilitado = true
	tarefa, codigo := w.proximaDo("real")
	if codigo != http.StatusOK || tarefa.Execucao.ID != realID || tarefa.Execucao.Tipo != "real" || len(tarefa.Cotas) != 2 {
		t.Fatalf("tarefa real: %d %+v", codigo, tarefa)
	}
	r0, r1 := tarefa.Cotas[0], tarefa.Cotas[1]
	if r0.AssembleiaAprovada == nil || *r0.AssembleiaAprovada != assembleiaTeste || !r1.PermitirLanceExistente {
		t.Errorf("cotas entregues ao worker: %+v", tarefa.Cotas)
	}
	marcar := func(ec cotaExecucaoJSON, assembleia string) *httptest.ResponseRecorder {
		return w.post(rotaCota(ec.ID, "confirmacao-iniciada"), map[string]string{"assembleia_data": assembleia})
	}

	esperarStatus(t, marcar(r0, assembleiaTeste), http.StatusConflict, "marcar confirmação sem iniciar a cota")
	esperarStatus(t, w.post(rotaCota(r0.ID, "iniciar"), nil), http.StatusNoContent, "iniciar")
	esperarStatus(t, w.post(rotaCota(r0.ID, "concluir"), map[string]any{"status": "verificada"}), http.StatusUnprocessableEntity, "real concluindo como verificada")
	esperarStatus(t, marcar(r0, "22/09/2026"), http.StatusConflict, "assembleia diferente da aprovada")
	s.cfg.LanceRealHabilitado = false
	esperarStatus(t, marcar(r0, assembleiaTeste), http.StatusConflict, "chave desligada no meio da execução")
	s.cfg.LanceRealHabilitado = true
	esperarStatus(t, marcar(r0, assembleiaTeste), http.StatusNoContent, "marcar confirmação")
	esperarStatus(t, marcar(r0, assembleiaTeste), http.StatusConflict, "marcar confirmação de novo")
	esperarStatus(t, w.post(rotaCota(r0.ID, "iniciar"), nil), http.StatusConflict, "reiniciar cota em confirmação")

	esperarStatus(t, w.post(rotaCota(r0.ID, "pdf"), []byte("não é pdf")), http.StatusUnsupportedMediaType, "PDF inválido")
	pdf := w.post(rotaCota(r0.ID, "pdf"), pdfFalso)
	esperarStatus(t, pdf, http.StatusCreated, "PDF")
	pdfID := decodificar[map[string]string](t, pdf)["id"]

	esperarStatus(t, w.post(rotaCota(r0.ID, "concluir-confirmacao"), map[string]any{"status": "confirmada"}), http.StatusUnprocessableEntity, "confirmada sem protocolo")
	esperarStatus(t, w.post(rotaCota(r0.ID, "concluir-confirmacao"), map[string]any{"status": "confirmada", "protocolo": "18 89"}), http.StatusUnprocessableEntity, "protocolo inválido")
	confirmada := map[string]any{
		"status": "confirmada", "protocolo": "1889071", "texto_protocolo": "Anote os números dos protocolos: 2º Lance Fixo (Automático): 1889071",
		"parcelas_em_atraso": true, "pdf_id": pdfID, "assembleia_numero": "0123", "percentual": "25,0000",
	}
	esperarStatus(t, w.post(rotaCota(r0.ID, "concluir-confirmacao"), confirmada), http.StatusNoContent, "concluir confirmação")
	esperarStatus(t, w.post(rotaCota(r0.ID, "concluir-confirmacao"), confirmada), http.StatusConflict, "concluir confirmação de novo")

	// Cota 2: o worker clica em Confirmar, mas o resultado não chega à plataforma.
	esperarStatus(t, w.post(rotaCota(r1.ID, "iniciar"), nil), http.StatusNoContent, "iniciar cota 2")
	esperarStatus(t, marcar(r1, assembleiaTeste), http.StatusNoContent, "marcar confirmação da cota 2")
	fim := w.post(fmt.Sprintf("/internal/execucoes/%d/finalizar", realID), map[string]any{})
	esperarStatus(t, fim, http.StatusOK, "finalizar")
	if got := decodificar[map[string]string](t, fim)["status"]; got != "concluida_com_erros" {
		t.Fatalf("situação final = %q", got)
	}

	d := admin.detalheComLances(t, realID)
	if d.Cotas[0].Status != "confirmada" || d.Cotas[0].Protocolo == nil || *d.Cotas[0].Protocolo != "1889071" {
		t.Errorf("cota confirmada: %+v", d.Cotas[0])
	}
	if d.Cotas[1].Status != "erro_apos_confirmar" || d.Cotas[1].Erro == nil || !strings.Contains(*d.Cotas[1].Erro, "confira no Histórico") {
		t.Errorf("cota sem resultado: %+v", d.Cotas[1])
	}
	if len(d.Lances) != 1 || d.Lances[0].Protocolo != "1889071" || d.Lances[0].DriveStatus != "pendente" || d.Lances[0].PdfID == nil || !d.Lances[0].ParcelasEmAtraso {
		t.Fatalf("lances da execução: %+v", d.Lances)
	}
	var clienteID int64
	for _, q := range cotas {
		if q.ID == r0.CotaID {
			clienteID = q.ClienteID
		}
	}
	lances := admin.lancesDoCliente(t, clienteID)
	if len(lances) != 1 || lances[0].Origem != "plataforma" || lances[0].Modalidade != "2º Lance Fixo" || lances[0].AssembleiaData == nil || *lances[0].AssembleiaData != assembleiaTeste {
		t.Errorf("histórico do cliente: %+v", lances)
	}
	if contarAuditoria(t, s, "lance_confirmacao_iniciada") != 2 || contarAuditoria(t, s, "lance_registrado") != 1 {
		t.Error("confirmação não auditada")
	}

	_, err := s.pool.Exec(context.Background(), "UPDATE execucao_cotas SET status = 'pendente' WHERE id = $1", r0.ID)
	if err == nil || !strings.Contains(err.Error(), "já passou pela confirmação") {
		t.Errorf("trigger deixou confirmada → pendente: %v", err)
	}

	// Um novo dry-run da mesma cota mostra o lance registrado pela plataforma e exige autorização.
	novo, _ := dryRunRevisado(t, operador, interno, cotas)
	rv := admin.revisao(t, novo)
	if len(rv.Cotas[0].LancesPlataforma) != 1 || rv.Cotas[0].LancesPlataforma[0] != "1889071" || !rv.Cotas[0].ExigeAutorizacao {
		t.Errorf("revisão depois do lance: %+v", rv.Cotas[0])
	}
}

type enviadorFalso struct {
	falhas   int
	chamadas int
	nomes    []string
	pdfs     [][]byte
}

func (f *enviadorFalso) Enviar(_ context.Context, nome string, pdf []byte) (drive.Arquivo, error) {
	f.chamadas++
	if f.falhas > 0 {
		f.falhas--
		return drive.Arquivo{}, errors.New("falha simulada do Google Drive")
	}
	f.nomes = append(f.nomes, nome)
	f.pdfs = append(f.pdfs, pdf)
	return drive.Arquivo{ID: fmt.Sprintf("arquivo-%d", f.chamadas), Nome: nome, Link: "https://drive.exemplo/arquivo"}, nil
}

func situacaoDoDrive(t *testing.T, s *Servidor, lanceID int64) (string, string) {
	t.Helper()
	var status string
	var erro *string
	if err := s.pool.QueryRow(context.Background(), "SELECT drive_status, drive_erro FROM lances WHERE id = $1", lanceID).Scan(&status, &erro); err != nil {
		t.Fatal(err)
	}
	return status, valorOuVazio(erro)
}

// reimprimir pede a reimpressão e simula o worker: Histórico, PDF e conclusão.
func reimprimir(t *testing.T, n *navegador, interno http.Handler, cota cotaJSON, protocolo string, enviarDrive bool) int64 {
	t.Helper()
	criada := n.enviarJSON(http.MethodPost, "/execucoes/reimpressoes", map[string]any{"cota_id": cota.ID, "protocolo": protocolo, "enviar_drive": enviarDrive})
	esperarStatus(t, criada, http.StatusCreated, "pedir reimpressão")
	id := decodificar[struct{ ID int64 }](t, criada).ID

	w := &workerTeste{t: t, h: interno, nome: "worker-reimpressao"}
	tarefa, codigo := w.proximaDo("dry_run", "reimpressao")
	if codigo != http.StatusOK || tarefa.Execucao.ID != id || tarefa.Execucao.Tipo != "reimpressao" || len(tarefa.Cotas) != 1 ||
		tarefa.Cotas[0].Protocolo == nil || *tarefa.Cotas[0].Protocolo != protocolo {
		t.Fatalf("tarefa de reimpressão: %d %+v", codigo, tarefa)
	}
	ec := tarefa.Cotas[0]
	esperarStatus(t, w.post(rotaCota(ec.ID, "iniciar"), nil), http.StatusNoContent, "iniciar")
	esperarStatus(t, w.post(rotaCota(ec.ID, "confirmacao-iniciada"), map[string]string{"assembleia_data": assembleiaTeste}), http.StatusUnprocessableEntity, "reimpressão marcando confirmação")
	esperarStatus(t, w.post(rotaCota(ec.ID, "concluir"), map[string]any{"status": "verificada"}), http.StatusUnprocessableEntity, "reimpressão concluindo como verificada")
	pdf := w.post(rotaCota(ec.ID, "pdf"), pdfFalso)
	esperarStatus(t, pdf, http.StatusCreated, "PDF")
	conclusao := map[string]any{
		"status": "reimpressa", "protocolo": "999", "pdf_id": decodificar[map[string]string](t, pdf)["id"],
		"assembleia_data": assembleiaTeste, "credenciamento": "10/09/2026 10:11:12", "modalidade": "2º Lance Fixo", "percentual": "25,0000",
	}
	esperarStatus(t, w.post(rotaCota(ec.ID, "concluir-reimpressao"), conclusao), http.StatusUnprocessableEntity, "protocolo diferente do pedido")
	conclusao["protocolo"] = protocolo
	esperarStatus(t, w.post(rotaCota(ec.ID, "concluir-reimpressao"), conclusao), http.StatusNoContent, "concluir reimpressão")
	fim := w.post(fmt.Sprintf("/internal/execucoes/%d/finalizar", id), map[string]any{})
	if got := decodificar[map[string]string](t, fim)["status"]; got != "concluida" {
		t.Fatalf("reimpressão terminou como %q", got)
	}
	d := n.detalheComLances(t, id)
	if len(d.Lances) != 1 || d.Lances[0].Protocolo != protocolo {
		t.Fatalf("lance da reimpressão: %+v", d.Lances)
	}
	return d.Lances[0].ID
}

func TestReimpressaoDeComprovante(t *testing.T) {
	s, operador, cotas, interno := cenarioExecucoes(t)
	leitura := entrarComo(t, s, "leitura@exemplo.com", db.PerfilUsuarioLeitura)
	cota := cotas[0]

	esperarStatus(t, leitura.enviarJSON(http.MethodPost, "/execucoes/reimpressoes", map[string]any{"cota_id": cota.ID, "protocolo": "1788714"}), http.StatusForbidden, "leitura pede reimpressão")
	esperarStatus(t, operador.enviarJSON(http.MethodPost, "/execucoes/reimpressoes", map[string]any{"cota_id": cota.ID, "protocolo": "17-88"}), http.StatusUnprocessableEntity, "protocolo inválido")
	esperarStatus(t, operador.enviarJSON(http.MethodPost, "/execucoes/reimpressoes", map[string]any{"cota_id": 999999, "protocolo": "1788714"}), http.StatusNotFound, "cota inexistente")

	lanceID := reimprimir(t, operador, interno, cota, "1788714", false)
	lances := operador.lancesDoCliente(t, cota.ClienteID)
	if len(lances) != 1 || lances[0].Origem != "historico" || lances[0].DriveStatus != "nao_enviar" || lances[0].PdfID == nil || lances[0].RegistradoEm == nil {
		t.Fatalf("lance do Histórico: %+v", lances)
	}
	if contarAuditoria(t, s, "comprovante_reimpresso") != 1 {
		t.Error("reimpressão não auditada")
	}
	if outro := reimprimir(t, operador, interno, cota, "1788714", false); outro != lanceID {
		t.Errorf("reimprimir o mesmo protocolo criou outro lance: %d e %d", lanceID, outro)
	}

	// Sem pedido de envio, a fila do Drive não pega o comprovante.
	ctx := context.Background()
	falso := &enviadorFalso{}
	s.enviadorDrive = falso
	if n := s.processarFilaDrive(ctx); n != 0 || falso.chamadas != 0 {
		t.Fatalf("a fila enviou comprovante sem pedido: %d", falso.chamadas)
	}

	// Na tela dá para enviar depois.
	enviar := fmt.Sprintf("/lances/%d/reenviar-drive", lanceID)
	esperarStatus(t, leitura.enviar(http.MethodPost, enviar, nil, ""), http.StatusForbidden, "leitura envia ao Drive")
	esperarStatus(t, operador.enviar(http.MethodPost, "/lances/999999/reenviar-drive", nil, ""), http.StatusNotFound, "lance inexistente")
	esperarStatus(t, operador.enviar(http.MethodPost, enviar, nil, ""), http.StatusOK, "enviar ao Drive")
	esperarStatus(t, operador.enviar(http.MethodPost, enviar, nil, ""), http.StatusConflict, "enviar de novo o que já está na fila")
	if n := s.processarFilaDrive(ctx); n != 1 {
		t.Fatalf("enviados = %d", n)
	}
	sufixo := fmt.Sprintf(" - %s-%s-%s - 2026-09-10.pdf", cota.Grupo, cota.Cota, cota.Versao)
	if !strings.HasSuffix(falso.nomes[0], sufixo) || !bytes.Equal(falso.pdfs[0], pdfFalso) {
		t.Errorf("arquivo enviado: %q", falso.nomes[0])
	}
	if status, _ := situacaoDoDrive(t, s, lanceID); status != "enviado" {
		t.Errorf("situação no Drive = %q", status)
	}
}

func TestFilaDoDriveComFalha(t *testing.T) {
	s, operador, cotas, interno := cenarioExecucoes(t)
	lanceID := reimprimir(t, operador, interno, cotas[1], "1788715", true)
	ctx := context.Background()

	// Drive não configurado (sem cofre): o comprovante espera na fila.
	if n := s.processarFilaDrive(ctx); n != 0 {
		t.Fatalf("enviou sem Drive configurado: %d", n)
	}
	if status, _ := situacaoDoDrive(t, s, lanceID); status != "pendente" {
		t.Fatalf("situação sem Drive = %q", status)
	}

	falso := &enviadorFalso{falhas: 1}
	s.enviadorDrive = falso
	if n := s.processarFilaDrive(ctx); n != 0 {
		t.Fatalf("enviados com falha = %d", n)
	}
	if status, erro := situacaoDoDrive(t, s, lanceID); status != "erro" || !strings.Contains(erro, "falha simulada") {
		t.Fatalf("depois da falha: %q %q", status, erro)
	}
	// A nova tentativa espera (1 min depois da primeira falha).
	if n := s.processarFilaDrive(ctx); n != 0 || falso.chamadas != 1 {
		t.Fatalf("tentou de novo sem esperar: %d chamadas", falso.chamadas)
	}
	s.exec(t, "UPDATE lances SET atualizado_em = now() - interval '2 minutes' WHERE id = $1", lanceID)
	if n := s.processarFilaDrive(ctx); n != 1 {
		t.Fatalf("nova tentativa: enviados = %d", n)
	}
	if status, erro := situacaoDoDrive(t, s, lanceID); status != "enviado" || erro != "" {
		t.Fatalf("depois da nova tentativa: %q %q", status, erro)
	}

	// Envio interrompido (API caiu no meio): volta para a fila depois de 10 minutos.
	s.exec(t, "UPDATE lances SET drive_status = 'enviando', atualizado_em = now() - interval '11 minutes' WHERE id = $1", lanceID)
	if n := s.processarFilaDrive(ctx); n != 1 {
		t.Fatalf("envio travado não voltou para a fila: %d", n)
	}
}

func TestTokenDoDriveCifradoNoBanco(t *testing.T) {
	s := novoServidorTeste(t)
	ctx := context.Background()
	cofre, err := cripto.NovoCofre(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	token := []byte(`{"access_token":"acesso-de-teste","refresh_token":"renovacao-de-teste","scope":"https://www.googleapis.com/auth/drive.file","token_type":"Bearer","expiry_date":1757952000000}`)
	if _, err := ImportarTokenDrive(ctx, s.q, cofre, token); err != nil {
		t.Fatal(err)
	}
	var cifrado []byte
	if err := s.pool.QueryRow(ctx, "SELECT dados_cifrados FROM integracoes WHERE nome = $1", IntegracaoGoogleDrive).Scan(&cifrado); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cifrado, []byte("acesso-de-teste")) || bytes.Contains(cifrado, []byte("renovacao-de-teste")) {
		t.Fatal("token gravado sem cifra")
	}
	if _, err := ImportarTokenDrive(ctx, s.q, cofre, []byte(`{"scope":"x"}`)); err == nil {
		t.Error("aceitou token sem access_token nem refresh_token")
	}

	s.cfg.Google = ConfigGoogle{ClientID: "client-teste", ClientSecret: "segredo-teste", PastaDrive: "pasta-teste"}
	if e, motivo, _ := s.clienteDrive(ctx); e != nil || !strings.Contains(motivo, "CHAVE_CRIPTOGRAFIA") {
		t.Errorf("sem cofre: %v %q", e, motivo)
	}
	s.cfg.Cofre = cofre
	if e, motivo, err := s.clienteDrive(ctx); e == nil || err != nil {
		t.Fatalf("cliente do Drive: %v %q %v", e, motivo, err)
	}
	outra, _ := cripto.NovoCofre(strings.Repeat("cd", 32))
	s.cfg.Cofre, s.driveCliente = outra, nil
	if _, _, err := s.clienteDrive(ctx); err == nil || !strings.Contains(err.Error(), "não decifra") {
		t.Errorf("chave trocada: %v", err)
	}
}
