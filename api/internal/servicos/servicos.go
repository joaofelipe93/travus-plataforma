// Package servicos lista os serviços da plataforma que agem sobre um cliente do cadastro.
//
// O cadastro de clientes é um só (migração 00018): o mesmo cliente serve a todos os serviços e
// o que muda é a ativação de cada um. Esta lista é a fonte da verdade da API — a tela monta os
// interruptores a partir do que `GET /clientes/{id}` devolve, e o banco tem o mesmo conjunto
// no CHECK de cliente_servicos. Serviço novo: entra aqui, numa migração e no CHECK.
//
// Reservas não está aqui de propósito: hóspede não é cliente do cadastro.
package servicos

// ID de um serviço, como ele aparece no banco e na API.
const (
	Lances  = "lances"
	Boletos = "boletos"
)

// Servico é o que a API conta sobre um serviço para quem monta a tela.
type Servico struct {
	ID   string `json:"id"`
	Nome string `json:"nome"`
	// Uma frase: o que ligar isto faz com o cliente.
	Descricao string `json:"descricao"`
}

// Todos, na ordem em que a tela mostra.
var Todos = []Servico{
	{ID: Lances, Nome: "Lances", Descricao: "As cotas do cliente entram nos dry-runs e nos lances do Newcon."},
	{ID: Boletos, Nome: "Boletos", Descricao: "As rodadas do mês buscam os boletos do cliente e mandam no WhatsApp dele."},
}

// Valido diz se o id é de um serviço que a plataforma conhece.
func Valido(id string) bool {
	for _, s := range Todos {
		if s.ID == id {
			return true
		}
	}
	return false
}

// IDs de todos os serviços, na mesma ordem.
func IDs() []string {
	ids := make([]string, 0, len(Todos))
	for _, s := range Todos {
		ids = append(ids, s.ID)
	}
	return ids
}
