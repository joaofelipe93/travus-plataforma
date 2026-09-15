// Package alerta envia os alertas da plataforma por e-mail (SMTP).
package alerta

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Mensagem struct {
	Assunto string
	Corpo   string
}

// Enviador entrega uma mensagem de alerta (e-mail de verdade ou falso nos testes).
type Enviador interface {
	Enviar(ctx context.Context, m Mensagem) error
}

// SMTP envia por STARTTLS (porta 587) ou TLS direto (porta 465). A senha nunca vai sem
// criptografia, a não ser para um servidor local (testes).
type SMTP struct {
	Host    string
	Porta   int
	Usuario string
	Senha   string
	De      string
	Para    []string
	Timeout time.Duration
}

func (c SMTP) Configurado() bool {
	return c.Host != "" && c.De != "" && len(c.Para) > 0
}

// ParaLista separa "a@x.com, b@y.com".
func ParaLista(texto string) []string {
	var out []string
	for _, p := range strings.Split(texto, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func local(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (c SMTP) Enviar(ctx context.Context, m Mensagem) error {
	if !c.Configurado() {
		return errors.New("SMTP não configurado (SMTP_HOST, ALERTA_DE e ALERTA_PARA)")
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	porta := c.Porta
	if porta == 0 {
		porta = 587
	}
	endereco := net.JoinHostPort(c.Host, strconv.Itoa(porta))
	configTLS := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	discador := &net.Dialer{Timeout: timeout}

	var conn net.Conn
	var err error
	if porta == 465 {
		conn, err = (&tls.Dialer{NetDialer: discador, Config: configTLS}).DialContext(ctx, "tcp", endereco)
	} else {
		conn, err = discador.DialContext(ctx, "tcp", endereco)
	}
	if err != nil {
		return fmt.Errorf("conectando ao SMTP %s: %w", endereco, err)
	}
	prazo := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(prazo) {
		prazo = d
	}
	_ = conn.SetDeadline(prazo)

	cli, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP %s: %w", endereco, err)
	}
	defer cli.Close()

	if porta != 465 {
		if ok, _ := cli.Extension("STARTTLS"); ok {
			if err := cli.StartTLS(configTLS); err != nil {
				return fmt.Errorf("STARTTLS com %s: %w", endereco, err)
			}
		} else if !local(c.Host) {
			return fmt.Errorf("o servidor SMTP %s não oferece STARTTLS: a senha não vai sem criptografia", endereco)
		}
	}
	if c.Usuario != "" {
		if err := cli.Auth(smtp.PlainAuth("", c.Usuario, c.Senha, c.Host)); err != nil {
			return fmt.Errorf("autenticação no SMTP: %w", err)
		}
	}
	if err := cli.Mail(c.De); err != nil {
		return fmt.Errorf("remetente %s recusado: %w", c.De, err)
	}
	for _, p := range c.Para {
		if err := cli.Rcpt(p); err != nil {
			return fmt.Errorf("destinatário %s recusado: %w", p, err)
		}
	}
	w, err := cli.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(Montar(c.De, c.Para, m, time.Now())); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("o servidor SMTP recusou a mensagem: %w", err)
	}
	return cli.Quit()
}

// Montar escreve a mensagem (texto puro, UTF-8) com os cabeçalhos.
func Montar(de string, para []string, m Mensagem, agora time.Time) []byte {
	semQuebra := strings.NewReplacer("\r", " ", "\n", " ")
	id := make([]byte, 12)
	_, _ = rand.Read(id)
	dominio := "travus"
	if i := strings.LastIndex(de, "@"); i >= 0 {
		dominio = de[i+1:]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", semQuebra.Replace(de))
	fmt.Fprintf(&b, "To: %s\r\n", semQuebra.Replace(strings.Join(para, ", ")))
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", semQuebra.Replace(m.Assunto)))
	fmt.Fprintf(&b, "Date: %s\r\n", agora.Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", hex.EncodeToString(id), dominio)
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	corpo := strings.ReplaceAll(strings.ReplaceAll(m.Corpo, "\r\n", "\n"), "\n", "\r\n")
	b.WriteString(corpo)
	if !strings.HasSuffix(corpo, "\r\n") {
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}
