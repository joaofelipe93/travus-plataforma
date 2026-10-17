// Package boletoswa fala com o serviço que manda os boletos no WhatsApp
// (workers/boletos-whatsapp) pela rede interna, com o token de administração. O navegador
// nunca recebe esse token: a tela chama a API, que repassa.
//
// É um serviço diferente do notificador de check-in e com **outro número pareado**: o do
// check-in publica em grupo, este manda no privado de cada cliente.
package boletoswa

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
	ErrIndisponivel = errors.New("serviço de boletos no WhatsApp indisponível")
	// ErrDesconectado: o WhatsApp não está conectado (falta parear o número).
	ErrDesconectado = errors.New("WhatsApp desconectado")
)

type Status struct {
	Status    string  `json:"status"`
	Conectado bool    `json:"connected"`
	Conta     *string `json:"account"`
	QR        *string `json:"qr"`
	// Os dois freios do serviço: o do ambiente dele e o da configuração na tela.
	EnvioServico      bool           `json:"envio_habilitado_servico"`
	EnvioConfiguracao bool           `json:"envio_habilitado_configuracao"`
	PausaSegundos     int            `json:"pausa_envio_segundos"`
	Fila              map[string]int `json:"fila"`
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

// EnviarTeste põe uma mensagem de teste na fila, para o telefone informado (só dígitos, com
// DDI). Vai pelo mesmo caminho do envio de verdade — e por isso respeita a pausa e os freios.
func (c *Cliente) EnviarTeste(ctx context.Context, telefone string) (int64, error) {
	var r struct {
		Enfileirada int64 `json:"enfileirada"`
	}
	return r.Enfileirada, c.chamar(ctx, http.MethodPost, "/whatsapp/teste", map[string]string{"telefone": telefone}, &r)
}

func (c *Cliente) Desconectar(ctx context.Context) error {
	return c.chamar(ctx, http.MethodPost, "/whatsapp/desconectar", map[string]bool{"confirmar": true}, nil)
}

func (c *Cliente) chamar(ctx context.Context, metodo, caminho string, corpo, destino any) error {
	if c == nil {
		return fmt.Errorf("%w: BOLETOS_WHATSAPP_URL ou BOLETOS_ADMIN_TOKEN não configurados", ErrIndisponivel)
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
	case "telefone_invalido":
		return fmt.Errorf("telefone inválido: %s", falha.Detalhe)
	}
	return fmt.Errorf("%w: %s %s respondeu HTTP %d (%s)", ErrIndisponivel, metodo, caminho, resp.StatusCode, falha.Error)
}
