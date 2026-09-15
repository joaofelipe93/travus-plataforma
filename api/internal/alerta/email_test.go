package alerta

import (
	"bufio"
	"context"
	"encoding/base64"
	"mime"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// servidorSMTPFalso aceita uma mensagem (sem STARTTLS) e devolve o que recebeu.
func servidorSMTPFalso(t *testing.T, endereco string) (string, <-chan map[string]string) {
	t.Helper()
	ln, err := net.Listen("tcp", endereco)
	if err != nil {
		t.Skipf("não foi possível abrir %s: %v", endereco, err)
	}
	t.Cleanup(func() { ln.Close() })
	recebido := make(chan map[string]string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		escrever := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		dados := map[string]string{}
		escrever("220 teste ESMTP")
		for {
			linha, err := r.ReadString('\n')
			if err != nil {
				recebido <- dados
				return
			}
			linha = strings.TrimRight(linha, "\r\n")
			comando := strings.ToUpper(strings.SplitN(linha, " ", 2)[0])
			switch comando {
			case "EHLO", "HELO":
				escrever("250-teste")
				escrever("250 AUTH PLAIN")
			case "AUTH":
				partes := strings.Fields(linha)
				bruto, _ := base64.StdEncoding.DecodeString(partes[len(partes)-1])
				dados["auth"] = string(bruto)
				escrever("235 ok")
			case "MAIL":
				dados["mail"] = linha
				escrever("250 ok")
			case "RCPT":
				dados["rcpt"] += linha + ";"
				escrever("250 ok")
			case "DATA":
				escrever("354 envie")
				var corpo strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					corpo.WriteString(l)
				}
				dados["data"] = corpo.String()
				escrever("250 recebido")
			case "QUIT":
				escrever("221 tchau")
				recebido <- dados
				return
			default:
				escrever("250 ok")
			}
		}
	}()
	return ln.Addr().String(), recebido
}

func TestEnviarEmail(t *testing.T) {
	endereco, recebido := servidorSMTPFalso(t, "127.0.0.1:0")
	host, portaTexto, _ := net.SplitHostPort(endereco)
	porta, _ := strconv.Atoi(portaTexto)
	c := SMTP{Host: host, Porta: porta, Usuario: "alertas@exemplo.com", Senha: "senha-de-teste", De: "alertas@exemplo.com", Para: ParaLista(" ana@exemplo.com , bia@exemplo.com"), Timeout: 5 * time.Second}

	assunto := "[Travus] ALERTA: Backup do Postgres não rodou"
	if err := c.Enviar(context.Background(), Mensagem{Assunto: assunto, Corpo: "Linha 1\nLinha 2"}); err != nil {
		t.Fatal(err)
	}
	d := <-recebido
	if !strings.Contains(d["auth"], "senha-de-teste") || !strings.Contains(d["mail"], "alertas@exemplo.com") {
		t.Errorf("autenticação ou remetente: %+v", d)
	}
	if !strings.Contains(d["rcpt"], "ana@exemplo.com") || !strings.Contains(d["rcpt"], "bia@exemplo.com") {
		t.Errorf("destinatários: %q", d["rcpt"])
	}
	for _, esperado := range []string{"To: ana@exemplo.com, bia@exemplo.com", "charset=UTF-8", "Linha 1\r\nLinha 2\r\n"} {
		if !strings.Contains(d["data"], esperado) {
			t.Errorf("mensagem sem %q:\n%s", esperado, d["data"])
		}
	}
	// Assunto com acento vai codificado (RFC 2047) e volta igual.
	var cabecalho string
	for _, l := range strings.Split(d["data"], "\r\n") {
		if strings.HasPrefix(l, "Subject: ") {
			cabecalho = strings.TrimPrefix(l, "Subject: ")
		}
	}
	decodificado, err := new(mime.WordDecoder).DecodeHeader(cabecalho)
	if !strings.HasPrefix(cabecalho, "=?utf-8?") || err != nil || decodificado != assunto {
		t.Errorf("assunto %q decodificado como %q (%v)", cabecalho, decodificado, err)
	}
}

func TestSenhaNuncaVaiSemCriptografia(t *testing.T) {
	endereco, _ := servidorSMTPFalso(t, "127.0.0.2:0")
	host, portaTexto, _ := net.SplitHostPort(endereco)
	porta, _ := strconv.Atoi(portaTexto)
	c := SMTP{Host: host, Porta: porta, Usuario: "u", Senha: "s", De: "a@exemplo.com", Para: []string{"b@exemplo.com"}, Timeout: 5 * time.Second}
	err := c.Enviar(context.Background(), Mensagem{Assunto: "x", Corpo: "y"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("servidor sem STARTTLS aceito: %v", err)
	}
}

func TestMontarNaoDeixaInjetarCabecalho(t *testing.T) {
	msg := string(Montar("a@exemplo.com", []string{"b@exemplo.com"}, Mensagem{Assunto: "oi\r\nBcc: intruso@exemplo.com", Corpo: "c"}, time.Unix(0, 0)))
	cabecalhos := msg[:strings.Index(msg, "\r\n\r\n")]
	for _, l := range strings.Split(cabecalhos, "\r\n") {
		if strings.HasPrefix(strings.ToLower(l), "bcc:") {
			t.Fatalf("cabeçalho injetado: %q", msg)
		}
	}
	if (SMTP{}).Configurado() || len(ParaLista(" , ")) != 0 {
		t.Error("configuração vazia considerada válida")
	}
}
