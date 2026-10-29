package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/atendimento"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Serviço de Atendimento (issue #100): cadastro dos bots e do fluxo de cada um. Quem conversa é
// o worker de WhatsApp (etapa D) e quem responde é o agente (etapa B); aqui é só o que define
// um bot — e as duas regras que protegem quem está do outro lado:
//
//   - `envio_habilitado` (mandar de verdade) **só admin** muda. Operador liga e desliga o bot,
//     escreve instruções e monta o fluxo, mas não autoriza a plataforma a falar com cliente —
//     na tela, lance real também é só de admin, e configuração não pode ser a porta dos fundos.
//   - mudar o **fluxo** com conversa aberta é recusado (409): a pessoa ficaria numa etapa que
//     deixou de existir. Encerre as conversas ou espere elas fecharem.

// Limite do corpo: as instruções de um bot vão a 20 mil caracteres e o fluxo tem até 30 etapas,
// cada uma com instruções próprias — por isso o do fluxo é maior que o de qualquer outra rota.
const (
	limiteCorpoBot   = 64 << 10
	limiteCorpoFluxo = 512 << 10
)

// botJSON é o bot como a tela recebe.
type botJSON struct {
	ID                    int64   `json:"id"`
	Nome                  string  `json:"nome"`
	Slug                  string  `json:"slug"`
	Modelo                string  `json:"modelo"`
	Instrucoes            string  `json:"instrucoes"`
	Saudacao              *string `json:"saudacao"`
	Ativo                 bool    `json:"ativo"`
	EnvioHabilitado       bool    `json:"envio_habilitado"`
	MaxMensagensHora      int32   `json:"max_mensagens_hora"`
	MaxTokensConversa     int32   `json:"max_tokens_conversa"`
	PausaRespostaSegundos int32   `json:"pausa_resposta_segundos"`
	// Só na lista: ajuda a tela sem um pedido por bot.
	Etapas           *int32 `json:"etapas,omitempty"`
	ConversasAbertas *int32 `json:"conversas_abertas,omitempty"`
}

func botParaJSON(b db.AtendimentoBot) botJSON {
	return botJSON{
		ID: b.ID, Nome: b.Nome, Slug: b.Slug, Modelo: b.Modelo, Instrucoes: b.Instrucoes,
		Saudacao: b.Saudacao, Ativo: b.Ativo, EnvioHabilitado: b.EnvioHabilitado,
		MaxMensagensHora: b.MaxMensagensHora, MaxTokensConversa: b.MaxTokensConversa,
		PausaRespostaSegundos: b.PausaRespostaSegundos,
	}
}

type etapaJSON struct {
	ID         int64               `json:"id"`
	Ordem      int32               `json:"ordem"`
	Nome       string              `json:"nome"`
	Objetivo   string              `json:"objetivo"`
	Instrucoes *string             `json:"instrucoes"`
	Campos     []atendimento.Campo `json:"campos"`
}

func etapaParaJSON(e db.AtendimentoEtapa) (etapaJSON, error) {
	campos := []atendimento.Campo{}
	if len(e.Campos) > 0 {
		if err := json.Unmarshal(e.Campos, &campos); err != nil {
			return etapaJSON{}, fmt.Errorf("campos da etapa %d: %w", e.ID, err)
		}
	}
	return etapaJSON{
		ID: e.ID, Ordem: e.Ordem, Nome: e.Nome, Objetivo: e.Objetivo,
		Instrucoes: e.Instrucoes, Campos: campos,
	}, nil
}

// GET /atendimento/modelos — o catálogo, para a tela montar a escolha do modelo.
func (s *Servidor) listarModelosAtendimento(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	responderJSON(w, http.StatusOK, map[string]any{
		"modelos": atendimento.Modelos,
		"padrao":  atendimento.ModeloPadrao,
	})
}

// GET /atendimento/bots
func (s *Servidor) listarBots(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	linhas, err := s.q.ListarBots(r.Context())
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	bots := make([]botJSON, 0, len(linhas))
	for _, l := range linhas {
		b := botParaJSON(db.AtendimentoBot{
			ID: l.ID, Nome: l.Nome, Slug: l.Slug, Modelo: l.Modelo, Instrucoes: l.Instrucoes,
			Saudacao: l.Saudacao, Ativo: l.Ativo, EnvioHabilitado: l.EnvioHabilitado,
			MaxMensagensHora: l.MaxMensagensHora, MaxTokensConversa: l.MaxTokensConversa,
			PausaRespostaSegundos: l.PausaRespostaSegundos,
		})
		etapas, abertas := l.Etapas, l.ConversasAbertas
		b.Etapas, b.ConversasAbertas = &etapas, &abertas
		bots = append(bots, b)
	}
	responderJSON(w, http.StatusOK, map[string]any{"bots": bots})
}

// GET /atendimento/bots/{id} — o bot com o fluxo dele.
func (s *Servidor) buscarBot(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	ctx := r.Context()
	bot, err := s.q.BuscarBot(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "bot não encontrado")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	linhas, err := s.q.ListarEtapasDoBot(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	etapas := make([]etapaJSON, 0, len(linhas))
	for _, l := range linhas {
		e, err := etapaParaJSON(l)
		if err != nil {
			erroInterno(w, r, err)
			return
		}
		etapas = append(etapas, e)
	}
	contagem, err := s.q.ContarConversasDoBot(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{
		"bot":    botParaJSON(bot),
		"etapas": etapas,
		"conversas": map[string]int32{
			"total": contagem.Total, "abertas": contagem.Abertas,
		},
	})
}

// O formulário do bot. A tela manda tudo; campo ausente vira o padrão, e saudação em branco
// apaga a que estava lá (mesma regra do cadastro de clientes).
type pedidoBot struct {
	Nome                  string `json:"nome"`
	Slug                  string `json:"slug"`
	Modelo                string `json:"modelo"`
	Instrucoes            string `json:"instrucoes"`
	Saudacao              string `json:"saudacao"`
	Ativo                 *bool  `json:"ativo"`
	MaxMensagensHora      *int   `json:"max_mensagens_hora"`
	MaxTokensConversa     *int   `json:"max_tokens_conversa"`
	PausaRespostaSegundos *int   `json:"pausa_resposta_segundos"`
}

type botValidado struct {
	Nome, Modelo, Instrucoes string
	Saudacao                 *string
	Ativo                    bool
	MaxMensagensHora         int32
	MaxTokensConversa        int32
	PausaRespostaSegundos    int32
}

func validarBot(p pedidoBot) (botValidado, error) {
	var v botValidado
	var err error
	if v.Nome, err = atendimento.Nome(p.Nome); err != nil {
		return v, err
	}
	if v.Modelo, err = atendimento.ModeloDoBot(p.Modelo); err != nil {
		return v, err
	}
	if v.Instrucoes, err = atendimento.Instrucoes(p.Instrucoes); err != nil {
		return v, err
	}
	if v.Saudacao, err = atendimento.Saudacao(p.Saudacao); err != nil {
		return v, err
	}
	mensagens, err := atendimento.Teto("o teto de mensagens por hora", p.MaxMensagensHora, 60, 1, 1000)
	if err != nil {
		return v, err
	}
	tokens, err := atendimento.Teto("o teto de tokens por conversa", p.MaxTokensConversa, 50000, 1000, 2000000)
	if err != nil {
		return v, err
	}
	pausa, err := atendimento.Teto("a pausa antes de responder", p.PausaRespostaSegundos, 3, 0, 120)
	if err != nil {
		return v, err
	}
	// Pausa aceita zero, que o Teto trataria como "não informado": o padrão 3 só vale quando o
	// campo não vem. Zero explícito é respeitado.
	if p.PausaRespostaSegundos != nil && *p.PausaRespostaSegundos == 0 {
		pausa = 0
	}
	v.MaxMensagensHora, v.MaxTokensConversa, v.PausaRespostaSegundos = int32(mensagens), int32(tokens), int32(pausa)
	v.Ativo = p.Ativo != nil && *p.Ativo
	return v, nil
}

// POST /atendimento/bots — admin e operador.
func (s *Servidor) criarBot(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoBot
	if err := lerJSON(w, r, &p, limiteCorpoBot); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	slug, err := atendimento.Slug(p.Slug)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	v, err := validarBot(p)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	bot, err := s.q.CriarBot(r.Context(), db.CriarBotParams{
		Nome: v.Nome, Slug: slug, Modelo: v.Modelo, Instrucoes: v.Instrucoes, Saudacao: v.Saudacao,
		MaxMensagensHora: v.MaxMensagensHora, MaxTokensConversa: v.MaxTokensConversa,
		PausaRespostaSegundos: v.PausaRespostaSegundos, Por: &u.ID,
	})
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	s.auditar(r.Context(), r, &u.ID, "atendimento_bot_criado", "atendimento_bot", idTexto(bot.ID),
		map[string]any{"nome": bot.Nome, "slug": bot.Slug, "modelo": bot.Modelo})
	responderJSON(w, http.StatusCreated, map[string]any{"bot": botParaJSON(bot)})
}

// PUT /atendimento/bots/{id} — admin e operador. `slug` e `envio_habilitado` não passam por aqui.
func (s *Servidor) atualizarBot(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	var p pedidoBot
	if err := lerJSON(w, r, &p, limiteCorpoBot); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.Slug != "" {
		antes, err := s.q.BuscarBot(r.Context(), id)
		if err == nil && p.Slug != antes.Slug {
			responderErro(w, http.StatusConflict,
				"o identificador do bot não muda: o pareamento do WhatsApp está gravado numa pasta com esse nome. Crie outro bot.")
			return
		}
	}
	v, err := validarBot(p)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	bot, err := s.q.AtualizarBot(r.Context(), db.AtualizarBotParams{
		ID: id, Nome: v.Nome, Modelo: v.Modelo, Instrucoes: v.Instrucoes, Saudacao: v.Saudacao,
		Ativo: v.Ativo, MaxMensagensHora: v.MaxMensagensHora, MaxTokensConversa: v.MaxTokensConversa,
		PausaRespostaSegundos: v.PausaRespostaSegundos, Por: &u.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "bot não encontrado")
		return
	}
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	s.auditar(r.Context(), r, &u.ID, "atendimento_bot_atualizado", "atendimento_bot", idTexto(bot.ID),
		map[string]any{"nome": bot.Nome, "modelo": bot.Modelo, "ativo": bot.Ativo})
	responderJSON(w, http.StatusOK, map[string]any{"bot": botParaJSON(bot)})
}

// PUT /atendimento/bots/{id}/envio — **só admin**: é o que autoriza a plataforma a falar com
// cliente de verdade. Com isto desligado o bot fica em ensaio.
func (s *Servidor) definirEnvioDoBot(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	var p struct {
		EnvioHabilitado *bool `json:"envio_habilitado"`
	}
	if err := lerJSON(w, r, &p, 1<<10); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	if p.EnvioHabilitado == nil {
		responderErro(w, http.StatusBadRequest, "informe envio_habilitado (true ou false)")
		return
	}
	bot, err := s.q.DefinirEnvioDoBot(r.Context(), db.DefinirEnvioDoBotParams{
		ID: id, EnvioHabilitado: *p.EnvioHabilitado, Por: &u.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "bot não encontrado")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	s.auditar(r.Context(), r, &u.ID, "atendimento_envio_alterado", "atendimento_bot", idTexto(bot.ID),
		map[string]any{"slug": bot.Slug, "envio_habilitado": bot.EnvioHabilitado})
	responderJSON(w, http.StatusOK, map[string]any{"bot": botParaJSON(bot)})
}

// DELETE /atendimento/bots/{id} — admin e operador. Bot com conversa é histórico: não sai.
func (s *Servidor) excluirBot(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	ctx := r.Context()
	contagem, err := s.q.ContarConversasDoBot(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if contagem.Total > 0 {
		responderErro(w, http.StatusConflict, fmt.Sprintf(
			"este bot tem %d conversa(s) registrada(s) e não pode ser excluído: desative-o para ele parar de atender.",
			contagem.Total))
		return
	}
	linhas, err := s.q.ExcluirBot(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if linhas == 0 {
		responderErro(w, http.StatusNotFound, "bot não encontrado")
		return
	}
	s.auditar(ctx, r, &u.ID, "atendimento_bot_excluido", "atendimento_bot", idTexto(id), nil)
	w.WriteHeader(http.StatusNoContent)
}

// PUT /atendimento/bots/{id}/etapas — o fluxo inteiro, na ordem da lista. Admin e operador.
//
// Grava numa transação, apagando e recriando: é previsível e a ordem é a que a tela mandou. Com
// conversa aberta é recusado — mudar o fluxo no meio deixaria a pessoa numa etapa que não existe.
func (s *Servidor) definirEtapasDoBot(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	var p struct {
		Etapas []struct {
			Nome       string              `json:"nome"`
			Objetivo   string              `json:"objetivo"`
			Instrucoes *string             `json:"instrucoes"`
			Campos     []atendimento.Campo `json:"campos"`
		} `json:"etapas"`
	}
	if err := lerJSON(w, r, &p, limiteCorpoFluxo); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	brutas := make([]atendimento.Etapa, 0, len(p.Etapas))
	for _, e := range p.Etapas {
		brutas = append(brutas, atendimento.Etapa{
			Nome: e.Nome, Objetivo: e.Objetivo, Instrucoes: e.Instrucoes, Campos: e.Campos,
		})
	}
	etapas, err := atendimento.Etapas(brutas)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}

	ctx := r.Context()
	if _, err := s.q.BuscarBot(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "bot não encontrado")
		return
	} else if err != nil {
		erroInterno(w, r, err)
		return
	}
	contagem, err := s.q.ContarConversasDoBot(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if contagem.Abertas > 0 {
		responderErro(w, http.StatusConflict, fmt.Sprintf(
			"este bot tem %d conversa(s) aberta(s): mudar o fluxo agora deixaria essas pessoas numa etapa que não existe mais. Espere encerrarem ou desative o bot.",
			contagem.Abertas))
		return
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
	qtx := s.q.WithTx(tx)

	if err := qtx.ApagarEtapasDoBot(ctx, id); err != nil {
		erroInterno(w, r, err)
		return
	}
	saida := make([]etapaJSON, 0, len(etapas))
	for i, e := range etapas {
		campos, err := json.Marshal(e.Campos)
		if err != nil {
			erroInterno(w, r, err)
			return
		}
		linha, err := qtx.CriarEtapa(ctx, db.CriarEtapaParams{
			BotID: id, Ordem: int32(i + 1), Nome: e.Nome, Objetivo: e.Objetivo,
			Instrucoes: e.Instrucoes, Campos: campos,
		})
		if err != nil {
			responderErroCadastro(w, r, err)
			return
		}
		j, err := etapaParaJSON(linha)
		if err != nil {
			erroInterno(w, r, err)
			return
		}
		saida = append(saida, j)
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	s.auditar(ctx, r, &u.ID, "atendimento_fluxo_atualizado", "atendimento_bot", idTexto(id),
		map[string]any{"etapas": len(saida)})
	responderJSON(w, http.StatusOK, map[string]any{"etapas": saida})
}
