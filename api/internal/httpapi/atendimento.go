package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/atendimento"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Serviço de Atendimento (issue #100): cadastro dos bots e do fluxo de cada um. Quem conversa é
// o worker de WhatsApp (etapa D) e quem responde é o agente (etapa B); aqui é só o que define
// um bot.
//
// **Um bot tem um botão: ligado ou desligado** (decisão do usuário em 29/10/2026). Antes havia
// também um `envio_habilitado` só de admin, que deixava o bot conversar sem nada sair (ensaio);
// eram travas demais para o serviço falar pelo número da própria casa — quem liga o bot é quem
// responde por ele. Para conferir um bot antes de divulgar o número, mande mensagem para ele.
//
// A regra que sobra: mudar as **perguntas** com conversa aberta é recusado (409), porque mudaria
// o atendimento de quem já está conversando. Encerre as conversas ou espere elas fecharem.

// Limite do corpo: as instruções de um bot vão a 20 mil caracteres e o atendimento tem até 30
// perguntas, cada uma com orientação própria — por isso o das perguntas é maior que o das outras.
const (
	limiteCorpoBot       = 64 << 10
	limiteCorpoPerguntas = 512 << 10
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
	MaxMensagensHora      int32   `json:"max_mensagens_hora"`
	MaxTokensConversa     int32   `json:"max_tokens_conversa"`
	PausaRespostaSegundos int32   `json:"pausa_resposta_segundos"`
	// Só na lista: ajuda a tela sem um pedido por bot.
	Perguntas        *int32 `json:"perguntas,omitempty"`
	ConversasAbertas *int32 `json:"conversas_abertas,omitempty"`
}

func botParaJSON(b db.AtendimentoBot) botJSON {
	return botJSON{
		ID: b.ID, Nome: b.Nome, Slug: b.Slug, Modelo: b.Modelo, Instrucoes: b.Instrucoes,
		Saudacao: b.Saudacao, Ativo: b.Ativo,
		MaxMensagensHora: b.MaxMensagensHora, MaxTokensConversa: b.MaxTokensConversa,
		PausaRespostaSegundos: b.PausaRespostaSegundos,
	}
}

// perguntaJSON é uma pergunta do atendimento como a tela recebe.
type perguntaJSON struct {
	ID          int64   `json:"id"`
	Ordem       int32   `json:"ordem"`
	Texto       string  `json:"texto"`
	Campo       string  `json:"campo"`
	Orientacao  *string `json:"orientacao"`
	Obrigatoria bool    `json:"obrigatoria"`
}

func perguntaParaJSON(q db.AtendimentoPergunta) perguntaJSON {
	return perguntaJSON{
		ID: q.ID, Ordem: q.Ordem, Texto: q.Texto, Campo: q.Campo,
		Orientacao: q.Orientacao, Obrigatoria: q.Obrigatoria,
	}
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
			Saudacao: l.Saudacao, Ativo: l.Ativo,
			MaxMensagensHora: l.MaxMensagensHora, MaxTokensConversa: l.MaxTokensConversa,
			PausaRespostaSegundos: l.PausaRespostaSegundos,
		})
		perguntas, abertas := l.Perguntas, l.ConversasAbertas
		b.Perguntas, b.ConversasAbertas = &perguntas, &abertas
		bots = append(bots, b)
	}
	responderJSON(w, http.StatusOK, map[string]any{"bots": bots})
}

// GET /atendimento/bots/{id} — o bot com as perguntas dele.
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
	linhas, err := s.q.ListarPerguntasDoBot(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	perguntas := make([]perguntaJSON, 0, len(linhas))
	for _, l := range linhas {
		perguntas = append(perguntas, perguntaParaJSON(l))
	}
	contagem, err := s.q.ContarConversasDoBot(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{
		"bot":       botParaJSON(bot),
		"perguntas": perguntas,
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

// PUT /atendimento/bots/{id} — admin e operador. O `slug` não passa por aqui: trocá-lo perderia
// o pareamento do WhatsApp daquele bot.
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

// PUT /atendimento/bots/{id}/perguntas — as perguntas inteiras, na ordem da lista. Admin e
// operador.
//
// Grava numa transação, apagando e recriando: é previsível e a ordem é a que a tela mandou. Com
// conversa aberta é recusado — trocar as perguntas no meio mudaria o atendimento de quem já está
// conversando.
func (s *Servidor) definirPerguntasDoBot(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	var p struct {
		Perguntas []atendimento.Pergunta `json:"perguntas"`
	}
	if err := lerJSON(w, r, &p, limiteCorpoPerguntas); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	perguntas, err := atendimento.Perguntas(p.Perguntas)
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
			"este bot tem %d conversa(s) aberta(s): mudar as perguntas agora mudaria o atendimento no meio da conversa. Espere encerrarem ou desligue o bot.",
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

	if err := qtx.ApagarPerguntasDoBot(ctx, id); err != nil {
		erroInterno(w, r, err)
		return
	}
	saida := make([]perguntaJSON, 0, len(perguntas))
	for i, q := range perguntas {
		linha, err := qtx.CriarPergunta(ctx, db.CriarPerguntaParams{
			BotID: id, Ordem: int32(i + 1), Texto: q.Texto, Campo: q.Campo,
			Orientacao: q.Orientacao, Obrigatoria: q.Obrigatoria,
		})
		if err != nil {
			responderErroCadastro(w, r, err)
			return
		}
		saida = append(saida, perguntaParaJSON(linha))
	}
	if err := tx.Commit(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	s.auditar(ctx, r, &u.ID, "atendimento_perguntas_atualizadas", "atendimento_bot", idTexto(id),
		map[string]any{"perguntas": len(saida)})
	responderJSON(w, http.StatusOK, map[string]any{"perguntas": saida})
}

// GET /atendimento/pontos-de-partida — os bots pré-escritos que a tela oferece em "Novo bot".
//
// A Travus trabalha com vários segmentos, então isto é um catálogo, não um padrão: quem cria
// escolhe (inclusive "em branco") e edita o que quiser. O texto é copiado na criação.
func (s *Servidor) listarPontosDePartida(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	responderJSON(w, http.StatusOK, map[string]any{"pontos_de_partida": atendimento.PontosDePartida})
}
