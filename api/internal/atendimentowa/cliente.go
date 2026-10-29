// Package atendimentowa fala com o serviço que conecta os WhatsApps dos bots
// (workers/atendimento-whatsapp) pela rede interna, com o token de administração. O navegador
// nunca recebe esse token: a tela chama a API, que repassa.
//
// A diferença em relação aos outros dois serviços de WhatsApp da plataforma é que este tem
// **N sessões num processo** — uma por bot. Por isso tudo aqui é por slug de bot, e o status é
// uma lista, não um estado só.
package atendimentowa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	// ErrIndisponivel: serviço desligado, sem configuração ou respondendo erro.
	ErrIndisponivel = errors.New("serviço de atendimento no WhatsApp indisponível")
	// ErrDesconectado: aquele bot não tem sessão aberta (falta parear o número).
	ErrDesconectado = errors.New("WhatsApp do bot desconectado")
	// ErrSemSessao: o worker não conhece esse bot (ele não está ligado, ou acabou de ser criado).
	ErrSemSessao = errors.New("o serviço não tem sessão para este bot")
)

// Sessao é o estado do WhatsApp de um bot no worker.
type Sessao struct {
	Bot       string  `json:"bot"` // o slug
	Estado    string  `json:"estado"`
	Conectado bool    `json:"conectado"`
	Numero    *string `json:"numero"`
	// String crua do QR (a tela desenha a imagem). Troca a cada ~20s enquanto não pareia.
	QR *string `json:"qr"`
	// Desde quando a sessão está aberta, em ms (para a tela dizer "conectado há 3 h").
	AbertaHaMs *int64  `json:"aberta_ha_ms"`
	UltimoErro *string `json:"ultimo_erro"`
}

type Status struct {
	// Freio de mão do próprio worker, independente da API.
	EnvioServico bool     `json:"envio_habilitado_servico"`
	Sessoes      []Sessao `json:"sessoes"`
}

// Da devolve a sessão de um bot, se o worker a conhece.
func (s *Status) Da(slug string) *Sessao {
	for i := range s.Sessoes {
		if s.Sessoes[i].Bot == slug {
			return &s.Sessoes[i]
		}
	}
	return nil
}

type Cliente struct {
	url   string
	token string
	http  *http.Client
}

// NovoCliente devolve nil sem URL ou token: as rotas respondem "indisponível".
func NovoCliente(url, token string) *Cliente {
	if url == "" || token == "" {
		return nil
	}
	return &Cliente{url: strings.TrimRight(url, "/"), token: token, http: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Cliente) Status(ctx context.Context) (*Status, error) {
	var s Status
	return &s, c.chamar(ctx, http.MethodGet, "/whatsapp/status", nil, &s)
}

// EnviarTeste manda uma mensagem de teste pelo número de um bot. **Sai para um telefone de
// verdade**, mesmo em modo ensaio: é com ela que se confere o pareamento (e por isso a tela
// pede confirmação e só admin chega aqui).
func (c *Cliente) EnviarTeste(ctx context.Context, slug, telefone string) error {
	return c.chamar(ctx, http.MethodPost, "/whatsapp/"+slug+"/teste", map[string]string{"telefone": telefone}, nil)
}

// Desconectar desvincula o aparelho daquele bot, apaga a sessão dele e gera QR novo. As
// respostas do bot ficam na fila até alguém parear de novo.
func (c *Cliente) Desconectar(ctx context.Context, slug string) error {
	return c.chamar(ctx, http.MethodPost, "/whatsapp/"+slug+"/desconectar", map[string]bool{"confirmar": true}, nil)
}

func (c *Cliente) chamar(ctx context.Context, metodo, caminho string, corpo, destino any) error {
	if c == nil {
		return fmt.Errorf("%w: ATENDIMENTO_WHATSAPP_URL ou ATENDIMENTO_ADMIN_TOKEN não configurados", ErrIndisponivel)
	}
	var leitor io.Reader
	if corpo != nil {
		b, err := json.Marshal(corpo)
		if err != nil {
			return err
		}
		leitor = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, c.url+caminho, leitor)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrIndisponivel, err)
	}
	defer resp.Body.Close()
	bruto, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrIndisponivel, err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if destino == nil {
			return nil
		}
		if err := json.Unmarshal(bruto, destino); err != nil {
			return fmt.Errorf("%w: resposta inválida: %v", ErrIndisponivel, err)
		}
		return nil
	}
	var falha struct {
		Error   string `json:"error"`
		Detalhe string `json:"detalhe"`
	}
	_ = json.Unmarshal(bruto, &falha)
	switch falha.Error {
	case "not_connected":
		return ErrDesconectado
	case "sem_sessao":
		return ErrSemSessao
	case "telefone_invalido":
		return fmt.Errorf("telefone inválido: %s", falha.Detalhe)
	}
	return fmt.Errorf("%w: %s %s respondeu HTTP %d (%s)", ErrIndisponivel, metodo, caminho, resp.StatusCode, falha.Error)
}
