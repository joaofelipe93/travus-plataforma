package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joaofelipe93/travus-plataforma/api/internal/alerta"
)

// smtpDoAmbiente lê SMTP_HOST, SMTP_PORTA, SMTP_USUARIO, SMTP_SENHA, ALERTA_DE e ALERTA_PARA.
func smtpDoAmbiente() alerta.SMTP {
	porta, err := strconv.Atoi(envOr("SMTP_PORTA", "587"))
	if err != nil {
		porta = 587
	}
	return alerta.SMTP{
		Host: os.Getenv("SMTP_HOST"), Porta: porta, Usuario: os.Getenv("SMTP_USUARIO"), Senha: os.Getenv("SMTP_SENHA"),
		De: os.Getenv("ALERTA_DE"), Para: alerta.ParaLista(os.Getenv("ALERTA_PARA")),
	}
}

// alertaCLI: api alerta testar (make alerta-teste).
func alertaCLI(args []string) error {
	if len(args) != 1 || args[0] != "testar" {
		return errors.New("uso: api alerta testar   (envia um e-mail de teste com SMTP_* e ALERTA_* do deploy/.env)")
	}
	c := smtpDoAmbiente()
	if !c.Configurado() {
		return errors.New("defina SMTP_HOST, ALERTA_DE e ALERTA_PARA em deploy/.env")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	origem := envOr("APP_ORIGIN", "http://app.localhost")
	err := c.Enviar(ctx, alerta.Mensagem{
		Assunto: "[Travus] Teste de alerta",
		Corpo:   "E-mail de teste da Travus Plataforma (make alerta-teste).\nSe chegou, os alertas do vigia chegam aqui também.\n\nPlataforma: " + origem + "\n",
	})
	if err != nil {
		return err
	}
	fmt.Printf("E-mail de teste enviado para %s.\n", strings.Join(c.Para, ", "))
	return nil
}
