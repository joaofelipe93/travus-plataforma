package httpapi

// Testes de integração do serviço de boletos: criar rodada, o worker percorrer os clientes e o
// resultado aparecer na tela. Precisam de Postgres (make test). Nada aqui fala com o Boleto
// Fácil — o worker é um dublê que chama as rotas internas, como o de verdade faria.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

type rodadaDetalhe struct {
	Rodada   rodadaJSON          `json:"rodada"`
	Clientes []rodadaClienteJSON `json:"clientes"`
	Parcelas []parcelaJSON       `json:"parcelas"`
	Eventos  []eventoBoletoJSON  `json:"eventos"`
}

// cenarioBoletos: operador logado e dois clientes com documento (e um sem, que fica de fora).
func cenarioBoletos(t *testing.T) (*Servidor, *navegador, http.Handler) {
	t.Helper()
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	n.cadastrarCliente(t, map[string]any{
		"nome": "CLIENTE COM DOCUMENTO", "cpf_cnpj": "111.222.333-96", "telefone": "(11) 90000-0000",
		"cotas": []map[string]any{cotaFicticia("6650", "0924", nil), cotaFicticia("6650", "0236", nil)},
	})
	n.cadastrarCliente(t, map[string]any{
		"nome": "EMPRESA COM CNPJ", "cpf_cnpj": "11222333000181",
		"cotas": []map[string]any{cotaFicticia("6620", "1372", nil)},
	})
	// Sem documento: o Boleto Fácil só responde por CPF/CNPJ, então não entra na rodada.
	n.cadastrarCliente(t, map[string]any{
		"nome":  "CLIENTE SEM DOCUMENTO",
		"cotas": []map[string]any{cotaFicticia("5678", "12", nil)},
	})
	return s, n, s.RotasInternas()
}

func (n *navegador) criarRodada(t *testing.T, tipo string) int64 {
	t.Helper()
	rec := n.enviarJSON(http.MethodPost, "/boletos/rodadas", map[string]any{"tipo": tipo})
	esperarStatus(t, rec, http.StatusCreated, "criar rodada de "+tipo)
	return decodificar[rodadaDetalhe](t, rec).Rodada.ID
}

func (n *navegador) rodada(t *testing.T, id int64) rodadaDetalhe {
	t.Helper()
	rec := n.req(http.MethodGet, fmt.Sprintf("/boletos/rodadas/%d", id), nil, nil)
	esperarStatus(t, rec, http.StatusOK, "detalhe da rodada")
	return decodificar[rodadaDetalhe](t, rec)
}

func TestRodadaDeEnvioDoComecoAoFim(t *testing.T) {
	s, n, interno := cenarioBoletos(t)
	wt := &workerTeste{t: t, h: interno, nome: "worker-boletos-teste"}

	id := n.criarRodada(t, "envio")
	inicial := n.rodada(t, id)
	if inicial.Rodada.Status != "na_fila" || len(inicial.Clientes) != 2 {
		t.Fatalf("rodada criada: status %q com %d cliente(s), quer na_fila com 2 (quem não tem documento fica fora)",
			inicial.Rodada.Status, len(inicial.Clientes))
	}

	// O worker pega a rodada.
	rec := wt.post("/internal/boletos/rodadas/proxima", nil)
	esperarStatus(t, rec, http.StatusOK, "reservar rodada")
	tarefa := decodificar[rodadaTarefaJSON](t, rec)
	if tarefa.Rodada.ID != id || tarefa.Rodada.Tipo != "envio" || tarefa.Rodada.Referencia == "" {
		t.Fatalf("tarefa = %+v", tarefa.Rodada)
	}
	// Uma rodada em andamento por vez: a segunda chamada não entrega nada.
	outra := n.criarRodada(t, "envio")
	esperarStatus(t, wt.post("/internal/boletos/rodadas/proxima", nil), http.StatusNoContent, "segunda rodada na fila")

	// Primeiro cliente: duas cotas, uma com parcela em aberto.
	rec = wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/proximo-cliente", id), nil)
	esperarStatus(t, rec, http.StatusOK, "próximo cliente")
	primeiro := decodificar[clienteBoletoJSON](t, rec).Cliente
	if primeiro.Documento != "11122233396" || len(primeiro.Cotas) != 2 {
		t.Fatalf("cliente entregue ao worker = %+v", primeiro)
	}

	rec = wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/parcelas", primeiro.ID), map[string]any{
		"parcelas": []map[string]any{{
			"cota_id": primeiro.Cotas[0].ID, "parcela": "018", "vencimento": "2026-11-16",
			"valor_centavos": 64462, "tipo": "overdue", "charge_id": 39871, "transaction_id": 179831728,
		}},
	})
	esperarStatus(t, rec, http.StatusCreated, "registrar parcela")
	parcelaIDs := decodificar[struct{ IDs []int64 }](t, rec).IDs
	if len(parcelaIDs) != 1 {
		t.Fatalf("ids das parcelas = %v", parcelaIDs)
	}
	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/parcelas/%d/pdf", parcelaIDs[0]), pdfFalso),
		http.StatusCreated, "enviar PDF do boleto")
	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/concluir", primeiro.ID),
		map[string]any{"status": "consultado", "cotas_no_site": 4, "cotas_casadas": 2}),
		http.StatusNoContent, "concluir primeiro cliente")

	// Segundo cliente: o site não devolveu nada em aberto.
	rec = wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/proximo-cliente", id), nil)
	esperarStatus(t, rec, http.StatusOK, "segundo cliente")
	segundo := decodificar[clienteBoletoJSON](t, rec).Cliente
	if segundo.Documento != "11222333000181" {
		t.Errorf("documento do segundo cliente = %q", segundo.Documento)
	}
	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/concluir", segundo.ID),
		map[string]any{"status": "sem_boleto", "cotas_no_site": 1, "cotas_casadas": 1}),
		http.StatusNoContent, "concluir segundo cliente")

	// Não há mais ninguém.
	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/proximo-cliente", id), nil),
		http.StatusNoContent, "fim dos clientes")
	rec = wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/finalizar", id), nil)
	esperarStatus(t, rec, http.StatusOK, "finalizar rodada")
	if fim := decodificar[struct{ Status string }](t, rec).Status; fim != "concluida" {
		t.Errorf("status final = %q, quer concluida", fim)
	}

	final := n.rodada(t, id)
	if final.Rodada.Status != "concluida" || len(final.Parcelas) != 1 {
		t.Fatalf("rodada final: %q com %d parcela(s)", final.Rodada.Status, len(final.Parcelas))
	}
	p := final.Parcelas[0]
	if p.Parcela != "018" || p.Vencimento != "2026-11-16" || p.ValorCentavos != 64462 || p.PdfID == nil {
		t.Errorf("parcela gravada = %+v", p)
	}
	// Grupo, cota e versão vêm do cadastro (pelo cota_id), não do worker: ele só manda o id.
	usada := primeiro.Cotas[0]
	if p.CotaID != usada.ID || p.Grupo != usada.Grupo || p.Cota != usada.Cota || p.Versao != usada.Versao {
		t.Errorf("identificação da cota = %s-%s-%s (cota %d), quer %s-%s-%s (cota %d)",
			p.Grupo, p.Cota, p.Versao, p.CotaID, usada.Grupo, usada.Cota, usada.Versao, usada.ID)
	}
	// E com os zeros à esquerda do cadastro, não como o site devolve (cota sem zeros).
	if len(p.Grupo) != 6 || len(p.Cota) != 4 {
		t.Errorf("grupo/cota sem o preenchimento do cadastro: %q/%q", p.Grupo, p.Cota)
	}
	if final.Clientes[0].Status != "consultado" || final.Clientes[1].Status != "sem_boleto" {
		t.Errorf("situações dos clientes = %q, %q", final.Clientes[0].Status, final.Clientes[1].Status)
	}
	if len(final.Eventos) == 0 {
		t.Error("a rodada devia ter log")
	}

	// O PDF do boleto só sai pela API, com sessão.
	caminho := "/arquivos/" + *p.PdfID
	esperarStatus(t, n.req(http.MethodGet, caminho, nil, nil), http.StatusOK, "baixar o boleto com sessão")
	anonimo := &navegador{t: t, h: s.Rotas()}
	esperarStatus(t, anonimo.req(http.MethodGet, caminho, nil, nil), http.StatusUnauthorized, "baixar o boleto sem sessão")

	// Terminada a primeira, o worker pega a próxima da fila.
	rec = wt.post("/internal/boletos/rodadas/proxima", nil)
	esperarStatus(t, rec, http.StatusOK, "rodada seguinte")
	if decodificar[rodadaTarefaJSON](t, rec).Rodada.ID != outra {
		t.Error("a rodada seguinte da fila devia ser a segunda criada")
	}
}

func TestRodadaDeCobrancaExigeEnvio(t *testing.T) {
	_, n, interno := cenarioBoletos(t)
	wt := &workerTeste{t: t, h: interno, nome: "worker-boletos-teste"}

	// Sem envio concluído no mês, a cobrança não tem com o que comparar: cobraria quem nunca
	// recebeu boleto da plataforma.
	esperarStatus(t, n.enviarJSON(http.MethodPost, "/boletos/rodadas", map[string]any{"tipo": "cobranca"}),
		http.StatusConflict, "cobrança sem envio")

	// Um envio que achou uma parcela libera a cobrança do mês.
	id := n.criarRodada(t, "envio")
	esperarStatus(t, wt.post("/internal/boletos/rodadas/proxima", nil), http.StatusOK, "reservar envio")
	rec := wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/proximo-cliente", id), nil)
	esperarStatus(t, rec, http.StatusOK, "cliente do envio")
	c := decodificar[clienteBoletoJSON](t, rec).Cliente
	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/parcelas", c.ID), map[string]any{
		"parcelas": []map[string]any{{
			"cota_id": c.Cotas[0].ID, "parcela": "018", "vencimento": "2026-11-16",
			"valor_centavos": 64462, "tipo": "overdue",
		}},
	}), http.StatusCreated, "parcela do envio")
	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/concluir", c.ID),
		map[string]any{"status": "consultado"}), http.StatusNoContent, "concluir cliente do envio")
	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/finalizar", id), nil),
		http.StatusOK, "finalizar envio")

	esperarStatus(t, n.enviarJSON(http.MethodPost, "/boletos/rodadas", map[string]any{"tipo": "cobranca"}),
		http.StatusCreated, "cobrança depois do envio")
}

func TestRodadaRecusasEPermissoes(t *testing.T) {
	s, n, interno := cenarioBoletos(t)
	h := s.Rotas()
	wt := &workerTeste{t: t, h: interno, nome: "worker-boletos-teste"}

	esperarStatus(t, n.enviarJSON(http.MethodPost, "/boletos/rodadas", map[string]any{"tipo": "qualquer"}),
		http.StatusBadRequest, "tipo inválido")
	esperarStatus(t, n.enviarJSON(http.MethodPost, "/boletos/rodadas", map[string]any{"tipo": "envio", "referencia": "2026/11"}),
		http.StatusBadRequest, "referência fora do formato")

	// Leitura acompanha, mas não cria nem cancela.
	criarUsuario(t, s, "leitura@exemplo.com", db.PerfilUsuarioLeitura)
	leitura, _ := entrar(t, h, "leitura@exemplo.com", senhaTeste)
	esperarStatus(t, leitura.req(http.MethodGet, "/boletos/rodadas", nil, nil), http.StatusOK, "leitura lista rodadas")
	esperarStatus(t, leitura.enviarJSON(http.MethodPost, "/boletos/rodadas", map[string]any{"tipo": "envio"}),
		http.StatusForbidden, "leitura cria rodada")

	id := n.criarRodada(t, "envio")
	esperarStatus(t, leitura.enviarJSON(http.MethodPost, fmt.Sprintf("/boletos/rodadas/%d/cancelar", id), nil),
		http.StatusForbidden, "leitura cancela rodada")

	// Cancelar na fila cancela na hora, e os clientes pendentes não serão processados.
	esperarStatus(t, n.enviarJSON(http.MethodPost, fmt.Sprintf("/boletos/rodadas/%d/cancelar", id), nil),
		http.StatusOK, "cancelar na fila")
	cancelada := n.rodada(t, id)
	if cancelada.Rodada.Status != "cancelada" {
		t.Errorf("status = %q, quer cancelada", cancelada.Rodada.Status)
	}
	for _, c := range cancelada.Clientes {
		if c.Status != "cancelado" {
			t.Errorf("cliente %q ficou %q, quer cancelado", c.ClienteNome, c.Status)
		}
	}
	esperarStatus(t, wt.post("/internal/boletos/rodadas/proxima", nil), http.StatusNoContent, "rodada cancelada não vai para o worker")

	// Parcela de cota que não é do cliente é recusada (o worker manda cota_id).
	nova := n.criarRodada(t, "envio")
	esperarStatus(t, wt.post("/internal/boletos/rodadas/proxima", nil), http.StatusOK, "reservar rodada nova")
	rec := wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/proximo-cliente", nova), nil)
	esperarStatus(t, rec, http.StatusOK, "primeiro cliente")
	primeiro := decodificar[clienteBoletoJSON](t, rec).Cliente
	rec = wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/proximo-cliente", nova), nil)
	esperarStatus(t, rec, http.StatusOK, "segundo cliente")
	segundo := decodificar[clienteBoletoJSON](t, rec).Cliente

	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/parcelas", primeiro.ID), map[string]any{
		"parcelas": []map[string]any{{
			"cota_id": segundo.Cotas[0].ID, "parcela": "001", "vencimento": "2026-11-16",
			"valor_centavos": 1000, "tipo": "overdue",
		}},
	}), http.StatusBadRequest, "parcela de cota de outro cliente")

	// Validações de parcela.
	for nome, linha := range map[string]map[string]any{
		"vencimento em outro formato": {"cota_id": primeiro.Cotas[0].ID, "parcela": "1", "vencimento": "16/11/2026", "valor_centavos": 1000, "tipo": "overdue"},
		"valor zero":                  {"cota_id": primeiro.Cotas[0].ID, "parcela": "1", "vencimento": "2026-11-16", "valor_centavos": 0, "tipo": "overdue"},
		"parcela sem número":          {"cota_id": primeiro.Cotas[0].ID, "parcela": "", "vencimento": "2026-11-16", "valor_centavos": 1000, "tipo": "overdue"},
		"sem tipo":                    {"cota_id": primeiro.Cotas[0].ID, "parcela": "1", "vencimento": "2026-11-16", "valor_centavos": 1000, "tipo": ""},
	} {
		esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/parcelas", primeiro.ID),
			map[string]any{"parcelas": []map[string]any{linha}}), http.StatusBadRequest, nome)
	}

	// Erro conhecido de um cliente deixa a rodada "concluida_com_erros".
	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/concluir", primeiro.ID),
		map[string]any{"status": "erro", "erro_tipo": "conhecido", "erro": "o site não respondeu"}),
		http.StatusNoContent, "concluir com erro")
	esperarStatus(t, wt.post(fmt.Sprintf("/internal/boletos/clientes/%d/concluir", segundo.ID),
		map[string]any{"status": "sem_boleto"}), http.StatusNoContent, "concluir sem boleto")
	rec = wt.post(fmt.Sprintf("/internal/boletos/rodadas/%d/finalizar", nova), nil)
	esperarStatus(t, rec, http.StatusOK, "finalizar com erro")
	if fim := decodificar[struct{ Status string }](t, rec).Status; fim != "concluida_com_erros" {
		t.Errorf("status final = %q, quer concluida_com_erros", fim)
	}

	// PDF que não é PDF é recusado.
	esperarStatus(t, wt.post("/internal/boletos/parcelas/1/pdf", []byte("nao sou pdf")),
		http.StatusUnsupportedMediaType, "arquivo que não é PDF")
}

// As rotas do serviço de boletos ficam atrás do mesmo token de serviço das outras internas.
func TestRotasInternasDeBoletosExigemToken(t *testing.T) {
	s := novoServidorTeste(t)
	interno := s.RotasInternas()

	semToken := httptest.NewRecorder()
	interno.ServeHTTP(semToken, httptest.NewRequest(http.MethodPost, "/internal/boletos/rodadas/proxima", nil))
	esperarStatus(t, semToken, http.StatusUnauthorized, "sem token")

	r := httptest.NewRequest(http.MethodPost, "/internal/boletos/rodadas/proxima", nil)
	r.Header.Set("Authorization", "Bearer outro-token-qualquer-com-mais-de-32-caracteres")
	tokenErrado := httptest.NewRecorder()
	interno.ServeHTTP(tokenErrado, r)
	esperarStatus(t, tokenErrado, http.StatusUnauthorized, "token errado")

	r = httptest.NewRequest(http.MethodPost, "/internal/boletos/rodadas/proxima", nil)
	r.Header.Set("Authorization", "Bearer "+tokenWorkerTeste)
	semNome := httptest.NewRecorder()
	interno.ServeHTTP(semNome, r)
	esperarStatus(t, semNome, http.StatusBadRequest, "sem X-Worker")
}
