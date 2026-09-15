package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"

	"github.com/joaofelipe93/travus-plataforma/api/internal/auth"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const usoUsuario = `uso:
  api usuario criar --email EMAIL --nome "NOME" --perfil admin|operador|leitura
  api usuario senha --email EMAIL       troca a senha e encerra as sessões
  api usuario desativar --email EMAIL   bloqueia o login e encerra as sessões
  api usuario ativar --email EMAIL
  api usuario listar
A senha é pedida no terminal (sem aparecer) ou lida da entrada padrão.`

// Não existe cadastro público: usuários só são criados por este comando.
func usuario(args []string) error {
	if len(args) == 0 {
		return errors.New(usoUsuario)
	}
	fs := flag.NewFlagSet("api usuario "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	email := fs.String("email", "", "")
	nome := fs.String("nome", "", "")
	perfil := fs.String("perfil", "", "")
	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("%v\n%s", err, usoUsuario)
	}
	*email = strings.TrimSpace(*email)

	dbURL, err := requiredEnv("DATABASE_URL")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	q := db.New(pool)

	switch args[0] {
	case "criar":
		p := db.PerfilUsuario(*perfil)
		if *email == "" || strings.TrimSpace(*nome) == "" || !p.Valid() {
			return errors.New(usoUsuario)
		}
		senha, err := lerSenha()
		if err != nil {
			return err
		}
		hash, err := auth.GerarHashSenha(senha)
		if err != nil {
			return err
		}
		u, err := q.CriarUsuario(ctx, db.CriarUsuarioParams{Email: *email, Nome: strings.TrimSpace(*nome), SenhaHash: hash, Perfil: p})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("já existe usuário com o e-mail %s", *email)
		}
		if err != nil {
			return err
		}
		auditarCLI(ctx, q, "usuario_criado", u.Email, map[string]any{"perfil": u.Perfil})
		fmt.Printf("Usuário criado: %s <%s>, perfil %s\n", u.Nome, u.Email, u.Perfil)

	case "senha":
		if *email == "" {
			return errors.New(usoUsuario)
		}
		senha, err := lerSenha()
		if err != nil {
			return err
		}
		hash, err := auth.GerarHashSenha(senha)
		if err != nil {
			return err
		}
		n, err := q.AtualizarSenhaUsuario(ctx, db.AtualizarSenhaUsuarioParams{SenhaHash: hash, Email: *email})
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("usuário %s não encontrado", *email)
		}
		sessoes, err := q.ApagarSessoesDoUsuario(ctx, *email)
		if err != nil {
			return err
		}
		auditarCLI(ctx, q, "usuario_senha_trocada", *email, map[string]any{"sessoes_encerradas": sessoes})
		fmt.Printf("Senha trocada para %s; %d sessão(ões) encerrada(s)\n", *email, sessoes)

	case "desativar", "ativar":
		if *email == "" {
			return errors.New(usoUsuario)
		}
		ativo := args[0] == "ativar"
		n, err := q.DefinirUsuarioAtivo(ctx, db.DefinirUsuarioAtivoParams{Ativo: ativo, Email: *email})
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("usuário %s não encontrado", *email)
		}
		var sessoes int64
		if !ativo {
			if sessoes, err = q.ApagarSessoesDoUsuario(ctx, *email); err != nil {
				return err
			}
		}
		participio := map[bool]string{true: "ativado", false: "desativado"}[ativo]
		auditarCLI(ctx, q, "usuario_"+participio, *email, map[string]any{"sessoes_encerradas": sessoes})
		fmt.Printf("Usuário %s %s\n", *email, participio)

	case "listar":
		us, err := q.ListarUsuarios(ctx)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "E-MAIL\tNOME\tPERFIL\tATIVO\tCRIADO EM")
		for _, u := range us {
			ativo := "sim"
			if !u.Ativo {
				ativo = "não"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", u.Email, u.Nome, u.Perfil, ativo, u.CriadoEm.Local().Format("02/01/2006 15:04"))
		}
		return tw.Flush()

	default:
		return errors.New(usoUsuario)
	}
	return nil
}

// lerSenha pede a senha duas vezes no terminal, ou lê uma linha da entrada padrão.
func lerSenha() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		linha, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && linha == "" {
			return "", errors.New("senha não informada na entrada padrão")
		}
		senha := strings.TrimRight(linha, "\r\n")
		return senha, auth.ValidarNovaSenha(senha)
	}

	fmt.Fprintf(os.Stderr, "Senha (mínimo %d caracteres, não aparece ao digitar): ", auth.SenhaTamanhoMinimo)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	senha := string(b)
	if err := auth.ValidarNovaSenha(senha); err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Repita a senha: ")
	b, err = term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(b) != senha {
		return "", errors.New("as senhas não conferem")
	}
	return senha, nil
}

func auditarCLI(ctx context.Context, q *db.Queries, acao, email string, detalhes map[string]any) {
	detalhes["via"] = "linha de comando"
	bruto, _ := json.Marshal(detalhes)
	entidade := "usuario"
	if err := q.RegistrarAuditoria(ctx, db.RegistrarAuditoriaParams{
		Acao: acao, Entidade: &entidade, EntidadeID: &email, Detalhes: bruto,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "aviso: não foi possível registrar na auditoria:", err)
	}
}
