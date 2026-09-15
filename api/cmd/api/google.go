package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joaofelipe93/travus-plataforma/api/internal/cripto"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
	"github.com/joaofelipe93/travus-plataforma/api/internal/httpapi"
)

const usoGoogle = `uso:
  api google importar-token   lê o token.json (googleapis) da entrada padrão e grava cifrado no banco
  api google status           mostra o que está configurado para o envio ao Google Drive
No compose: make google-token (importa workers/canopus/token.json) e make google-status.`

func google(args []string) error {
	if len(args) != 1 {
		return errors.New(usoGoogle)
	}
	dbURL, err := requiredEnv("DATABASE_URL")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	q := db.New(pool)

	faltando := func() []string {
		var f []string
		for _, nome := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GOOGLE_DRIVE_PASTA_ID"} {
			if os.Getenv(nome) == "" {
				f = append(f, nome)
			}
		}
		return f
	}

	switch args[0] {
	case "importar-token":
		cofre, err := cripto.NovoCofre(os.Getenv("CHAVE_CRIPTOGRAFIA"))
		if err != nil {
			return err
		}
		bruto, err := io.ReadAll(io.LimitReader(os.Stdin, 64<<10))
		if err != nil {
			return err
		}
		if len(bruto) == 0 {
			return errors.New("envie o token.json pela entrada padrão (make google-token)")
		}
		tn, err := httpapi.ImportarTokenDrive(ctx, q, cofre, bruto)
		if err != nil {
			return err
		}
		detalhes, _ := json.Marshal(map[string]string{"via": "linha de comando", "escopo": tn.Scope})
		nome := "integracao"
		entidade := httpapi.IntegracaoGoogleDrive
		if err := q.RegistrarAuditoria(ctx, db.RegistrarAuditoriaParams{Acao: "google_token_importado", Entidade: &nome, EntidadeID: &entidade, Detalhes: detalhes}); err != nil {
			fmt.Fprintln(os.Stderr, "aviso: não foi possível registrar na auditoria:", err)
		}
		fmt.Println("Token do Google Drive importado e cifrado no banco.")
		if f := faltando(); len(f) > 0 {
			fmt.Printf("Aviso: o envio ao Drive só começa com %v definidos no deploy/.env.\n", f)
		}
		return nil

	case "status":
		integ, err := q.BuscarIntegracao(ctx, httpapi.IntegracaoGoogleDrive)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			fmt.Println("Token: não importado (make google-token)")
		case err != nil:
			return err
		default:
			fmt.Printf("Token: importado (atualizado em %s)\n", integ.AtualizadoEm.Local().Format("02/01/2006 15:04"))
		}
		if f := faltando(); len(f) > 0 {
			fmt.Printf("Faltando no ambiente: %v\n", f)
		} else {
			fmt.Println("Client OAuth e pasta: configurados")
		}
		fmt.Printf("LANCE_REAL_HABILITADO=%s\n", envOr("LANCE_REAL_HABILITADO", "false"))
		return nil
	}
	return errors.New(usoGoogle)
}
