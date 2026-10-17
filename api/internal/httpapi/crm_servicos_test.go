package httpapi

// Ativação de serviço por cliente (migração 00018): o cadastro é um só para a plataforma e o
// que separa um serviço do outro é quem está ativo em qual. Aqui se garante que desligar tira
// o cliente da fila daquele serviço — e só daquele.

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

type respostaClienteComServicos struct {
	Cliente  clienteJSON            `json:"cliente"`
	Cotas    []cotaJSON             `json:"cotas"`
	Servicos []servicoDoClienteJSON `json:"servicos"`
}

func ativoEm(servicos []servicoDoClienteJSON, id string) bool {
	for _, s := range servicos {
		if s.ID == id {
			return s.Ativo
		}
	}
	return false
}

func TestClienteNasceAtivoNosServicos(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	// Sem a lista, o cliente novo entra em todos: é como a plataforma se comportava antes.
	rec := n.enviarJSON(http.MethodPost, "/clientes", map[string]any{"nome": "CLIENTE TODOS"})
	esperarStatus(t, rec, http.StatusCreated, "cadastrar sem escolher serviços")
	criado := decodificar[respostaClienteComServicos](t, rec)
	if len(criado.Servicos) != 2 || !ativoEm(criado.Servicos, "canopus") || !ativoEm(criado.Servicos, "boletos") {
		t.Fatalf("cliente novo devia nascer ativo nos dois serviços: %+v", criado.Servicos)
	}

	// Com a lista, só o que foi escolhido. O catálogo inteiro volta mesmo assim, para a tela
	// poder desenhar o interruptor desligado.
	rec = n.enviarJSON(http.MethodPost, "/clientes", map[string]any{"nome": "CLIENTE SO LANCES", "servicos": []string{"canopus"}})
	esperarStatus(t, rec, http.StatusCreated, "cadastrar escolhendo um serviço")
	soLances := decodificar[respostaClienteComServicos](t, rec)
	if len(soLances.Servicos) != 2 {
		t.Fatalf("o catálogo inteiro devia voltar: %+v", soLances.Servicos)
	}
	if !ativoEm(soLances.Servicos, "canopus") || ativoEm(soLances.Servicos, "boletos") {
		t.Errorf("servicos = %+v, quer canopus ativo e boletos não", soLances.Servicos)
	}

	esperarStatus(t, n.enviarJSON(http.MethodPost, "/clientes",
		map[string]any{"nome": "CLIENTE INVENTADO", "servicos": []string{"reservas"}}),
		http.StatusBadRequest, "serviço que não existe")
}

func TestLigarEDesligarServicoDoCliente(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	criarUsuario(t, s, "leitura@exemplo.com", db.PerfilUsuarioLeitura)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	cliente := n.cadastrarCliente(t, map[string]any{
		"nome": "CLIENTE DOS DOIS", "cpf_cnpj": "111.222.333-96",
		"cotas": []map[string]any{cotaFicticia("1234", "56", nil)},
	})
	caminho := fmt.Sprintf("/clientes/%d/servicos/boletos", cliente.Cliente.ID)

	// Quem só lê não mexe na carteira de ninguém.
	leitor, _ := entrar(t, h, "leitura@exemplo.com", senhaTeste)
	esperarStatus(t, leitor.enviarJSON(http.MethodPut, caminho, map[string]any{"ativo": false}),
		http.StatusForbidden, "perfil leitura desligando serviço")

	rec := n.enviarJSON(http.MethodPut, caminho, map[string]any{"ativo": false})
	esperarStatus(t, rec, http.StatusOK, "desligar boletos")
	depois := decodificar[respostaClienteComServicos](t, rec)
	if ativoEm(depois.Servicos, "boletos") || !ativoEm(depois.Servicos, "canopus") {
		t.Errorf("desligar boletos não devia mexer no canopus: %+v", depois.Servicos)
	}
	// A cota continua lá: desligar um serviço não apaga cadastro.
	if len(depois.Cotas) != 1 || !depois.Cotas[0].Ativa {
		t.Errorf("a cota devia continuar ativa: %+v", depois.Cotas)
	}
	if contarAuditoria(t, s, "cliente_servico_alterado") != 1 {
		t.Error("alterar serviço devia ser auditado")
	}

	// Desligado some do filtro por serviço, mas continua na carteira.
	lista := func(consulta string) []clienteResumoJSON {
		return decodificar[struct {
			Clientes []clienteResumoJSON `json:"clientes"`
		}](t, n.req(http.MethodGet, "/clientes"+consulta, nil, nil)).Clientes
	}
	if todos := lista(""); len(todos) != 1 || len(todos[0].Servicos) != 1 || todos[0].Servicos[0] != "canopus" {
		t.Errorf("a lista devia trazer só canopus como ativo: %+v", todos)
	}
	if len(lista("?servico=boletos")) != 0 {
		t.Error("cliente desligado não devia aparecer no filtro de boletos")
	}
	if len(lista("?servico=canopus")) != 1 {
		t.Error("cliente ligado devia aparecer no filtro de canopus")
	}
	esperarStatus(t, n.req(http.MethodGet, "/clientes?servico=inventado", nil, nil),
		http.StatusBadRequest, "filtro com serviço que não existe")

	// Ligar de volta é o mesmo caminho.
	esperarStatus(t, n.enviarJSON(http.MethodPut, caminho, map[string]any{"ativo": true}), http.StatusOK, "religar boletos")
	if !ativoEm(decodificar[respostaClienteComServicos](t, n.req(http.MethodGet, fmt.Sprintf("/clientes/%d", cliente.Cliente.ID), nil, nil)).Servicos, "boletos") {
		t.Error("boletos devia voltar a ficar ativo")
	}
	esperarStatus(t, n.enviarJSON(http.MethodPut, fmt.Sprintf("/clientes/%d/servicos/inventado", cliente.Cliente.ID),
		map[string]any{"ativo": true}), http.StatusBadRequest, "serviço que não existe")
	esperarStatus(t, n.enviarJSON(http.MethodPut, caminho, map[string]any{}),
		http.StatusBadRequest, "sem dizer se liga ou desliga")
}

func TestClienteDesligadoDoCanopusFicaForaDasExecucoes(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	dentro := n.cadastrarCliente(t, map[string]any{
		"nome": "CLIENTE ATIVO", "cotas": []map[string]any{cotaFicticia("1234", "56", nil)},
	})
	fora := n.cadastrarCliente(t, map[string]any{
		"nome": "CLIENTE DESLIGADO", "cotas": []map[string]any{cotaFicticia("1234", "99", nil)},
	})
	esperarStatus(t, n.enviarJSON(http.MethodPut, fmt.Sprintf("/clientes/%d/servicos/canopus", fora.Cliente.ID),
		map[string]any{"ativo": false}), http.StatusOK, "desligar o canopus")

	// A cota do desligado não entra num dry-run pedido na tela: a execução inteira é recusada,
	// com a contagem, em vez de a cota sumir em silêncio.
	esperarStatus(t, n.enviarJSON(http.MethodPost, "/execucoes", map[string]any{
		"tipo": "dry_run", "cota_ids": []int64{dentro.Cotas[0].ID, fora.Cotas[0].ID},
	}), http.StatusUnprocessableEntity, "dry-run com cota de cliente desligado")

	// Só com a cota de quem está ativo, o dry-run entra na fila normalmente.
	esperarStatus(t, n.enviarJSON(http.MethodPost, "/execucoes", map[string]any{
		"tipo": "dry_run", "cota_ids": []int64{dentro.Cotas[0].ID},
	}), http.StatusCreated, "dry-run só com cliente ativo")

	// O dry-run automático monta a lista pelo banco: a cota do desligado também não entra lá.
	ids, err := s.q.CotasAtivasIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != dentro.Cotas[0].ID {
		t.Errorf("cotas do agendamento = %v, quer só a do cliente ativo (%d)", ids, dentro.Cotas[0].ID)
	}

	// A tela de novo dry-run pede a lista já filtrada, para não oferecer o que seria recusado.
	canopus := "canopus"
	ativa := true
	cotas, err := s.q.ListarCotas(context.Background(), db.ListarCotasParams{Ativa: &ativa, Servico: &canopus})
	if err != nil {
		t.Fatal(err)
	}
	if len(cotas) != 1 || cotas[0].ID != dentro.Cotas[0].ID {
		t.Errorf("cotas do filtro por serviço = %+v, quer só a do cliente ativo", cotas)
	}
	// Sem o filtro, a tela de Cotas continua mostrando a carteira inteira.
	todas, err := s.q.ListarCotas(context.Background(), db.ListarCotasParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(todas) != 2 {
		t.Errorf("sem filtro, as duas cotas deviam aparecer: %+v", todas)
	}
}

func TestClienteDesligadoDosBoletosFicaForaDaRodada(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	// Os dois têm documento e cota: antes da 00018 era só isso que decidia quem entrava.
	dentro := n.cadastrarCliente(t, map[string]any{
		"nome": "CLIENTE COBRADO", "cpf_cnpj": "111.222.333-96",
		"cotas": []map[string]any{cotaFicticia("1234", "56", nil)},
	})
	fora := n.cadastrarCliente(t, map[string]any{
		"nome": "CLIENTE SEM COBRANCA", "cpf_cnpj": "11.222.333/0001-81",
		"cotas": []map[string]any{cotaFicticia("1234", "99", nil)},
	})
	esperarStatus(t, n.enviarJSON(http.MethodPut, fmt.Sprintf("/clientes/%d/servicos/boletos", fora.Cliente.ID),
		map[string]any{"ativo": false}), http.StatusOK, "desligar os boletos")

	clientes, err := s.q.ClientesParaBoletos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(clientes) != 1 || clientes[0].ID != dentro.Cliente.ID {
		t.Fatalf("clientes da rodada = %+v, quer só %q", clientes, dentro.Cliente.Nome)
	}
}
