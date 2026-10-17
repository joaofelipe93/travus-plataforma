package httpapi

// CPF/CNPJ do cliente: é o que o Boleto Fácil do Canopus pede para listar as cotas em aberto
// (docs/canopus-boleto-facil.md). Documentos fictícios, com dígito verificador certo.

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

func TestCadastroCpfCnpj(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	// Guardado só com dígitos, venha como vier do formulário.
	um := n.cadastrarCliente(t, map[string]any{"nome": "CLIENTE UM", "cpf_cnpj": "111.222.333-96"})
	if um.Cliente.CpfCnpj == nil || *um.Cliente.CpfCnpj != "11122233396" {
		t.Fatalf("cpf_cnpj gravado = %v, quer 11122233396", um.Cliente.CpfCnpj)
	}
	dois := n.cadastrarCliente(t, map[string]any{"nome": "CLIENTE DOIS", "cpf_cnpj": "11.222.333/0001-81"})
	if dois.Cliente.CpfCnpj == nil || *dois.Cliente.CpfCnpj != "11222333000181" {
		t.Fatalf("cnpj gravado = %v", dois.Cliente.CpfCnpj)
	}
	// Sem documento continua valendo: a maior parte do cadastro veio da planilha, que não tinha CPF.
	tres := n.cadastrarCliente(t, map[string]any{"nome": "CLIENTE TRES"})
	if tres.Cliente.CpfCnpj != nil {
		t.Errorf("cliente sem documento devia ficar nulo: %v", tres.Cliente.CpfCnpj)
	}

	// O mesmo documento em outro cliente é a mesma pessoa cadastrada duas vezes.
	esperarStatus(t, n.enviarJSON(http.MethodPost, "/clientes",
		map[string]any{"nome": "CLIENTE QUATRO", "cpf_cnpj": "11122233396"}),
		http.StatusConflict, "documento repetido")
	esperarStatus(t, n.enviarJSON(http.MethodPatch, fmt.Sprintf("/clientes/%d", dois.Cliente.ID),
		map[string]any{"nome": "CLIENTE DOIS", "cpf_cnpj": "11122233396"}),
		http.StatusConflict, "documento de outro cliente")

	// Dígito verificador errado não chega ao site do Canopus (lá viraria lista vazia e captcha).
	for nome, documento := range map[string]string{
		"dígito errado":    "111.222.333-00",
		"dígitos iguais":   "111.111.111-11",
		"curto":            "1234567890",
		"cnpj inválido":    "11.222.333/0001-00",
		"sem dígito algum": "abc",
	} {
		esperarStatus(t, n.enviarJSON(http.MethodPost, "/clientes",
			map[string]any{"nome": "CLIENTE CINCO", "cpf_cnpj": documento}),
			http.StatusBadRequest, nome)
	}

	// O formulário manda tudo: documento em branco apaga o que estava lá.
	caminho := fmt.Sprintf("/clientes/%d", um.Cliente.ID)
	rec := n.enviarJSON(http.MethodPatch, caminho, map[string]any{"nome": "CLIENTE UM", "cpf_cnpj": ""})
	esperarStatus(t, rec, http.StatusOK, "apagar o documento")
	if c := decodificar[respostaCadastro](t, rec).Cliente; c.CpfCnpj != nil {
		t.Errorf("documento em branco devia apagar: %v", c.CpfCnpj)
	}
	// Apagado, o documento pode ser usado por quem é dono dele de verdade.
	rec = n.enviarJSON(http.MethodPatch, fmt.Sprintf("/clientes/%d", dois.Cliente.ID),
		map[string]any{"nome": "CLIENTE DOIS", "cpf_cnpj": "111.222.333-96"})
	esperarStatus(t, rec, http.StatusOK, "reaproveitar documento liberado")

	clientes := decodificar[struct{ Clientes []clienteResumoJSON }](t, n.req(http.MethodGet, "/clientes", nil, nil)).Clientes
	if len(clientes) != 3 {
		t.Errorf("clientes = %d, quer 3 (nenhum documento inválido devia ter cadastrado)", len(clientes))
	}
}
