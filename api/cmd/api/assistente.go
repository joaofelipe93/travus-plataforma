package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joaofelipe93/travus-plataforma/api/internal/assistente"
	"github.com/joaofelipe93/travus-plataforma/api/internal/httpapi"
)

// assistenteDoAmbiente liga o assistente da tela inicial:
//   - ANTHROPIC_API_KEY: sem ela, a tela avisa que o assistente não está configurado;
//   - ASSISTENTE_MODELO: padrão claude-opus-5;
//   - ASSISTENTE_DB_SENHA: senha do papel só leitura `assistente` (migração 00011). Sem ela, não
//     há consulta livre ao banco, só as ferramentas prontas;
//   - ASSISTENTE_PERGUNTAS_POR_HORA: limite por pessoa (padrão 60).
func assistenteDoAmbiente(ctx context.Context, principal *pgxpool.Config) (httpapi.ConfigAssistente, error) {
	var cfg httpapi.ConfigAssistente
	if v := os.Getenv("ASSISTENTE_PERGUNTAS_POR_HORA"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return cfg, fmt.Errorf("ASSISTENTE_PERGUNTAS_POR_HORA inválido: %q", v)
		}
		cfg.PerguntasPorHora = n
	}
	chave := os.Getenv("ANTHROPIC_API_KEY")
	if chave == "" {
		return cfg, nil
	}
	modelo := assistente.NovoModeloAnthropic(chave, os.Getenv("ASSISTENTE_MODELO"))
	cfg.Modelo, cfg.NomeModelo = modelo, modelo.Nome()

	senha := os.Getenv("ASSISTENTE_DB_SENHA")
	if senha == "" {
		return cfg, nil
	}
	// Mesmo host e banco da API, outro papel: o que o assistente roda não tem como sair dele.
	poolCfg := principal.Copy()
	poolCfg.ConnConfig.User = "assistente"
	poolCfg.ConnConfig.Password = senha
	poolCfg.ConnConfig.RuntimeParams["application_name"] = "travus-assistente"
	poolCfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return cfg, fmt.Errorf("configuração do banco do assistente inválida: %w", err)
	}
	cfg.Consultor = &assistente.Consultor{Pool: pool}
	return cfg, nil
}
