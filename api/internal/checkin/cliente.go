// Package checkin fala com o notificador de check-in (workers/checkin-whatsapp) pela rede
// interna, com o token de administração. O navegador nunca recebe esse token: a tela
// WhatsApp chama a API, que repassa.
package checkin

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
	// ErrIndisponivel: notificador desligado, sem configuração ou respondendo erro.
	ErrIndisponivel = errors.New("notificador de check-in indisponível")
	// ErrDesconectado: o WhatsApp não está conectado (falta parear).
	ErrDesconectado = errors.New("WhatsApp desconectado")
	// ErrGrupoNaoEncontrado: o número pareado não participa do grupo.
	ErrGrupoNaoEncontrado = errors.New("grupo não encontrado")
	// ErrSemGrupo: nenhum grupo de destino escolhido.
	ErrSemGrupo = errors.New("nenhum grupo escolhido")
)

type Grupo struct {
	JID    string  `json:"jid"`
	Nome   *string `json:"nome"`
	Origem string  `json:"origem"`
}

type Status struct {
	Status    string         `json:"status"`
	Conectado bool           `json:"connected"`
	Conta     *string        `json:"account"`
	QR        *string        `json:"qr"`
	Grupo     *Grupo         `json:"group"`
	Fila      map[string]int `json:"outbox"`
}

type GrupoDoNumero struct {
	JID           string `json:"jid"`
	Nome          string `json:"subject"`
	Participantes int    `json:"participants"`
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
	return &Cliente{url: strings.TrimRight(url, "/"), token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Cliente) Status(ctx context.Context) (*Status, error) {
	var s Status
	return &s, c.chamar(ctx, http.MethodGet, "/whatsapp/status", nil, &s)
}

func (c *Cliente) Grupos(ctx context.Context) ([]GrupoDoNumero, error) {
	var r struct {
		Groups []GrupoDoNumero `json:"groups"`
	}
	return r.Groups, c.chamar(ctx, http.MethodGet, "/whatsapp/groups", nil, &r)
}

func (c *Cliente) DefinirGrupo(ctx context.Context, jid string) (*Grupo, error) {
	var r struct {
		Group *Grupo `json:"group"`
	}
	return r.Group, c.chamar(ctx, http.MethodPut, "/whatsapp/grupo", map[string]string{"jid": jid}, &r)
}

func (c *Cliente) EnviarTeste(ctx context.Context) (*Grupo, error) {
	var r struct {
		Group *Grupo `json:"group"`
	}
	return r.Group, c.chamar(ctx, http.MethodPost, "/whatsapp/test", map[string]string{}, &r)
}

func (c *Cliente) Desconectar(ctx context.Context) error {
	return c.chamar(ctx, http.MethodPost, "/whatsapp/desconectar", map[string]string{}, nil)
}

func (c *Cliente) chamar(ctx context.Context, metodo, caminho string, corpo, destino any) error {
	if c == nil {
		return fmt.Errorf("%w: CHECKIN_URL ou CHECKIN_ADMIN_TOKEN não configurados", ErrIndisponivel)
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
		Error string `json:"error"`
	}
	_ = json.Unmarshal(bruto, &falha)
	switch falha.Error {
	case "not_connected":
		return ErrDesconectado
	case "group_not_found":
		return ErrGrupoNaoEncontrado
	case "group_not_configured":
		return ErrSemGrupo
	}
	// 401 aqui é configuração errada (tokens diferentes), não sessão do usuário.
	return fmt.Errorf("%w: %s %s respondeu HTTP %d (%s)", ErrIndisponivel, metodo, caminho, resp.StatusCode, falha.Error)
}
