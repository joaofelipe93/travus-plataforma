package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/assistente"
	"github.com/joaofelipe93/travus-plataforma/api/internal/atendimento"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Etapa B da issue #100: o agente do bot. Quem conversa com o cliente é o modelo, mas **quem
// decide o que sai** é esta camada — e ela roda na API, não no worker, porque é aqui que estão a
// chave da Anthropic, o laço de tool use (`internal/assistente`) e o banco.
//
// O que esta camada garante, independente do que o modelo faça:
//
//   - **bot desligado não responde** (é o único interruptor do serviço);
//   - **teto de tokens por conversa** e **teto de respostas por hora por bot**: o primeiro
//     encerra a conversa, o segundo cancela a resposta. Conversa automática dos dois lados é um
//     laço infinito à espera de acontecer, e o número é o que paga a conta;
//   - **a próxima pergunta é a primeira sem resposta** — calculada por `atendimento.Proxima`, e
//     não um estado que a conversa guarda. O modelo não decide quando "avançar": ele registra o
//     que a pessoa disse, e a lista anda sozinha.

const (
	// Histórico que vai para o modelo. Conversa de WhatsApp é curta; mais que isto é custo sem
	// ganho, e o que importa de verdade já está em `coletas`.
	historicoConversa = 40
	// Tempo de uma resposta. O worker espera por isto, então não pode ser o limite do assistente
	// (4 min): quem está do outro lado desistiu muito antes.
	tempoRespostaBot = 90 * time.Second
)

// motivos de uma resposta que não saiu; a tela mostra este texto.
const (
	motivoTetoHora = "teto de respostas por hora deste bot"
)

// resultadoAtendimento é o que aconteceu com uma mensagem recebida.
type resultadoAtendimento struct {
	ConversaID int64  `json:"conversa_id"`
	MensagemID *int64 `json:"mensagem_id,omitempty"`
	Status     string `json:"status"` // pendente | cancelada | sem_resposta
	Motivo     string `json:"motivo,omitempty"`
	Encerrada  bool   `json:"encerrada"`
	// A pergunta da vez depois desta resposta (nil quando acabaram, ou quando o bot não tem
	// perguntas). Serve ao log do worker e aos testes.
	PerguntaAtual *string `json:"pergunta_atual,omitempty"`
	TokensGasto   int32   `json:"tokens_gasto"`
}

// sessaoBot junta o que as ferramentas precisam mexer durante uma resposta. As ferramentas
// gravam no banco na hora (coleta perdida é pergunta repetida) e anotam aqui o que a camada de
// cima precisa decidir depois.
type sessaoBot struct {
	s        *Servidor
	bot      db.AtendimentoBot
	conversa db.AtendimentoConversa
	// As perguntas do bot, na ordem, e o que já foi respondido nesta conversa.
	perguntas  []db.AtendimentoPergunta
	respondido map[string]string

	encerrou bool
	motivo   string
}

// paraPrompt traduz as perguntas do banco no que o pacote atendimento precisa, já com o que foi
// respondido. Devolve também o que foi registrado e não casa com pergunta nenhuma (o modelo
// anotou algo a mais, ou a pergunta foi tirada depois).
func (z *sessaoBot) paraPrompt() ([]atendimento.PerguntaPrompt, map[string]string) {
	lista := make([]atendimento.PerguntaPrompt, 0, len(z.perguntas))
	daLista := make(map[string]bool, len(z.perguntas))
	for _, q := range z.perguntas {
		daLista[q.Campo] = true
		lista = append(lista, atendimento.PerguntaPrompt{
			Texto: q.Texto, Campo: q.Campo, Orientacao: q.Orientacao,
			Obrigatoria: q.Obrigatoria, Resposta: z.respondido[q.Campo],
		})
	}
	extras := map[string]string{}
	for campo, valor := range z.respondido {
		if !daLista[campo] {
			extras[campo] = valor
		}
	}
	return lista, extras
}

// perguntaPorCampo acha a pergunta de um campo, para a coleta apontar para ela.
func (z *sessaoBot) perguntaPorCampo(campo string) *db.AtendimentoPergunta {
	for i := range z.perguntas {
		if z.perguntas[i].Campo == campo {
			return &z.perguntas[i]
		}
	}
	return nil
}

// ferramentas do bot. São **duas**: registrar uma resposta e encerrar. Nenhuma delas fala com o
// mundo (nada de mandar e-mail, criar cliente ou consultar outro serviço) — o que sai desta
// conversa é texto para a pessoa e dado no banco, e o lead decide o que fazer com o dado.
//
// A terceira ferramenta (`avancar_etapa`) deixou de existir em 29/10/2026, quando as etapas
// viraram perguntas na ordem: não há o que avançar, a próxima pergunta é a primeira sem resposta.
func (z *sessaoBot) ferramentas(ctx context.Context) []assistente.Ferramenta {
	return []assistente.Ferramenta{
		{
			Nome: "registrar_resposta",
			Descricao: "Guarda o que a pessoa informou. Chame assim que ela disser, uma vez por dado. " +
				"Use os nomes de dado listados nas perguntas do atendimento.",
			Parametros: map[string]any{
				"campo": map[string]any{"type": "string", "description": "O nome do dado, como está entre colchetes na lista de perguntas."},
				"valor": map[string]any{"type": "string", "description": "O que a pessoa informou, em texto, sem reformatar."},
			},
			Obrigatorio: []string{"campo", "valor"},
			Rotulo:      "Registrando o que a pessoa informou",
			Executar:    z.registrarResposta,
		},
		{
			Nome: "encerrar",
			Descricao: "Encerra o atendimento. Use quando as perguntas terminaram, quando a pessoa desistiu, " +
				"quando ela pediu para falar com uma pessoa da equipe, ou quando o assunto não é o seu.",
			Parametros: map[string]any{
				"motivo": map[string]any{
					"type": "string", "enum": atendimento.MotivosEncerramento,
					"description": "concluido, fora_do_escopo, desistiu ou pediu_humano.",
				},
			},
			Obrigatorio: []string{"motivo"},
			Rotulo:      "Encerrando o atendimento",
			Executar:    z.encerrar,
		},
	}
}

func (z *sessaoBot) registrarResposta(ctx context.Context, entrada json.RawMessage) (string, error) {
	var p struct {
		Campo string `json:"campo"`
		Valor string `json:"valor"`
	}
	if err := json.Unmarshal(entrada, &p); err != nil {
		return "", fmt.Errorf("entrada inválida: %w", err)
	}
	campo := strings.TrimSpace(strings.ToLower(p.Campo))
	valor := strings.TrimSpace(p.Valor)
	if campo == "" || valor == "" {
		return "", errors.New("campo e valor são obrigatórios")
	}
	// Campo que não é de pergunta nenhuma é recusado quando o bot TEM perguntas: senão o modelo
	// inventaria chave e a tela mostraria dado que ninguém pediu. A mensagem diz o que vale, e o
	// modelo corrige sozinho. Bot sem perguntas aceita qualquer campo — ali o dado solto é o
	// único jeito de anotar algo.
	pergunta := z.perguntaPorCampo(campo)
	if pergunta == nil && len(z.perguntas) > 0 {
		nomes := make([]string, 0, len(z.perguntas))
		for _, q := range z.perguntas {
			nomes = append(nomes, q.Campo)
		}
		return "", fmt.Errorf("dado %q não existe neste atendimento (os dados são: %s)", campo, strings.Join(nomes, ", "))
	}
	var perguntaID *int64
	if pergunta != nil {
		perguntaID = &pergunta.ID
	}
	if err := z.s.q.SalvarResposta(ctx, db.SalvarRespostaParams{
		ConversaID: z.conversa.ID, PerguntaID: perguntaID, Campo: campo, Valor: valor,
	}); err != nil {
		return "", err
	}
	z.respondido[campo] = valor

	lista, _ := z.paraPrompt()
	if proxima := atendimento.Proxima(lista); proxima != nil {
		return fmt.Sprintf(`{"ok":true,"proxima_pergunta":%q}`, proxima.Texto), nil
	}
	if faltam := atendimento.Pendentes(lista); len(faltam) > 0 {
		return fmt.Sprintf(`{"ok":true,"ainda_faltam":%q}`, strings.Join(faltam, ", ")), nil
	}
	return `{"ok":true,"todas_respondidas":true}`, nil
}

func (z *sessaoBot) encerrar(_ context.Context, entrada json.RawMessage) (string, error) {
	var p struct {
		Motivo string `json:"motivo"`
	}
	if err := json.Unmarshal(entrada, &p); err != nil {
		return "", fmt.Errorf("entrada inválida: %w", err)
	}
	motivo := strings.TrimSpace(strings.ToLower(p.Motivo))
	if !atendimento.MotivoValido(motivo) {
		return "", fmt.Errorf("motivo %q não existe (use %s)", p.Motivo, strings.Join(atendimento.MotivosEncerramento, ", "))
	}
	// Encerrar de verdade fica para depois da resposta: a última mensagem do bot ainda precisa
	// sair, e encerrar agora cancelaria a fila dela.
	z.encerrou, z.motivo = true, motivo
	return `{"ok":true}`, nil
}

// responderAtendimento é o caminho de uma mensagem recebida: abre ou continua a conversa, chama o
// modelo e enfileira a resposta (ou a grava cancelada, com o motivo). O worker chama isto pela
// rota interna e não decide nada.
func (s *Servidor) responderAtendimento(ctx context.Context, bot db.AtendimentoBot, telefone, contato, texto string) (resultadoAtendimento, error) {
	if s.cfg.ModeloAtendimento == nil {
		return resultadoAtendimento{}, errors.New("atendimento indisponível: falta ANTHROPIC_API_KEY no servidor")
	}
	// Bot desligado não abre conversa: o worker nem deveria estar conectado nele.
	if !bot.Ativo {
		return resultadoAtendimento{Status: "sem_resposta", Motivo: "o bot está desligado"}, nil
	}

	perguntas, err := s.q.ListarPerguntasDoBot(ctx, bot.ID)
	if err != nil {
		return resultadoAtendimento{}, err
	}
	conversa, err := s.q.AbrirOuContinuarConversa(ctx, db.AbrirOuContinuarConversaParams{
		BotID: bot.ID, Telefone: telefone, ContatoNome: textoOuNil(contato),
	})
	if err != nil {
		return resultadoAtendimento{}, err
	}
	if _, err := s.q.RegistrarMensagemRecebida(ctx, db.RegistrarMensagemRecebidaParams{
		ConversaID: conversa.ID, Texto: texto,
	}); err != nil {
		return resultadoAtendimento{}, err
	}

	res := resultadoAtendimento{ConversaID: conversa.ID, TokensGasto: conversa.TokensEntrada + conversa.TokensSaida}

	// Teto de tokens: a conversa que passou do limite é encerrada em vez de virar conta sem fim.
	if res.TokensGasto >= bot.MaxTokensConversa {
		if _, err := s.q.EncerrarConversa(ctx, db.EncerrarConversaParams{
			ID: conversa.ID, Status: "encerrada", Motivo: textoOuNil("teto de tokens da conversa"),
		}); err != nil {
			return res, err
		}
		res.Status, res.Motivo, res.Encerrada = "sem_resposta", "teto de tokens da conversa", true
		slog.Warn("atendimento: conversa encerrada pelo teto de tokens",
			"bot", bot.Slug, "conversa_id", conversa.ID, "tokens", res.TokensGasto)
		// Encerrou pelo teto, mas o que ela já tinha colhido não se joga fora (etapa E).
		s.criarLeadSePreciso(ctx, bot, conversa.ID, "teto_tokens")
		return res, nil
	}

	z := &sessaoBot{s: s, bot: bot, conversa: conversa, perguntas: perguntas, respondido: map[string]string{}}
	respostas, err := s.q.ColetasDaConversa(ctx, conversa.ID)
	if err != nil {
		return res, err
	}
	for _, r := range respostas {
		z.respondido[r.Campo] = r.Valor
	}
	lista, extras := z.paraPrompt()
	historico, err := s.historicoParaModelo(ctx, conversa.ID)
	if err != nil {
		return res, err
	}

	agente := &assistente.Agente{
		Modelo: s.cfg.ModeloAtendimento(bot.Modelo),
		Instrucoes: atendimento.Prompt(
			atendimento.BotPrompt{Nome: bot.Nome, Instrucoes: bot.Instrucoes}, lista, extras),
		Contexto:    atendimento.Contexto(s.agora(), valorOuVazio(conversa.ContatoNome)),
		Ferramentas: z.ferramentas(ctx),
	}

	ctxModelo, cancelar := context.WithTimeout(ctx, tempoRespostaBot)
	defer cancelar()
	var resposta strings.Builder
	uso, err := agente.Responder(ctxModelo, historico, func(e assistente.Evento) {
		if e.Tipo == "texto" {
			resposta.WriteString(e.Texto)
		}
	})
	// O gasto é somado mesmo quando dá erro no meio: o token já foi cobrado.
	if uso.TokensEntrada > 0 || uso.TokensSaida > 0 {
		if soma, errSoma := s.q.SomarTokensDaConversa(ctx, db.SomarTokensDaConversaParams{
			ID: conversa.ID, TokensEntrada: int32(uso.TokensEntrada), TokensSaida: int32(uso.TokensSaida),
		}); errSoma == nil {
			res.TokensGasto = soma.TokensEntrada + soma.TokensSaida
		}
	}
	if err != nil {
		return res, err
	}

	// A pergunta da vez, depois do que esta resposta registrou.
	if depois, _ := z.paraPrompt(); true {
		if proxima := atendimento.Proxima(depois); proxima != nil {
			texto := proxima.Texto
			res.PerguntaAtual = &texto
		}
	}

	textoResposta := strings.TrimSpace(resposta.String())
	if textoResposta != "" {
		status, motivo, err := s.decidirEnvio(ctx, bot)
		if err != nil {
			return res, err
		}
		msg, err := s.q.EnfileirarResposta(ctx, db.EnfileirarRespostaParams{
			ConversaID: conversa.ID, Texto: textoResposta, Status: status, Motivo: textoOuNil(motivo),
		})
		if err != nil {
			return res, err
		}
		res.MensagemID, res.Status, res.Motivo = &msg.ID, status, motivo
	} else {
		res.Status = "sem_resposta"
		if !z.encerrou {
			// Modelo que não escreve nada e não encerra é comportamento estranho: fica no log
			// para alguém olhar, mas não derruba a conversa.
			slog.Warn("atendimento: o modelo não respondeu nada", "bot", bot.Slug, "conversa_id", conversa.ID)
		}
	}

	if z.encerrou {
		if _, err := s.q.EncerrarConversa(ctx, db.EncerrarConversaParams{
			ID: conversa.ID, Status: "encerrada", Motivo: textoOuNil(z.motivo),
		}); err != nil {
			return res, err
		}
		res.Encerrada = true
		if res.Motivo == "" {
			res.Motivo = z.motivo
		}
		// Conversa encerrada que colheu algo vira lead para alguém revisar (etapa E). Falha aqui
		// não derruba o atendimento: a pessoa já foi respondida.
		s.criarLeadSePreciso(ctx, bot, conversa.ID, z.motivo)
	}
	return res, nil
}

// decidirEnvio diz se a resposta vai para a fila ou nasce cancelada.
//
// Antes eram três perguntas (chave do servidor, chave do bot e teto da hora). Hoje é uma: o teto
// de respostas por hora — **bot ligado responde** (decisão do usuário em 29/10/2026: um botão
// só). Quem não devia responder já não chegou aqui: conversa de bot desligado não abre.
func (s *Servidor) decidirEnvio(ctx context.Context, bot db.AtendimentoBot) (status, motivo string, err error) {
	naHora, err := s.q.ContarRespostasDaHora(ctx, bot.ID)
	if err != nil {
		return "", "", err
	}
	if naHora >= bot.MaxMensagensHora {
		slog.Warn("atendimento: teto de respostas por hora atingido", "bot", bot.Slug, "na_hora", naHora)
		return "cancelada", motivoTetoHora, nil
	}
	return "pendente", "", nil
}

// historicoParaModelo lê as últimas mensagens e as devolve na ordem da conversa. "recebida" é a
// pessoa (usuário) e "enviada" é o bot (assistente).
func (s *Servidor) historicoParaModelo(ctx context.Context, conversaID int64) ([]assistente.Mensagem, error) {
	linhas, err := s.q.HistoricoDaConversa(ctx, db.HistoricoDaConversaParams{
		ConversaID: conversaID, Limite: historicoConversa,
	})
	if err != nil {
		return nil, err
	}
	msgs := make([]assistente.Mensagem, 0, len(linhas))
	for i := len(linhas) - 1; i >= 0; i-- {
		papel := "usuario"
		if linhas[i].Direcao == "enviada" {
			papel = "assistente"
		}
		msgs = append(msgs, assistente.Mensagem{Papel: papel, Texto: linhas[i].Texto})
	}
	return msgs, nil
}

// botDoSlug é o que a rota interna usa: o worker conhece o bot pelo slug (o nome da pasta da
// sessão dele), não pelo id.
func (s *Servidor) botDoSlug(ctx context.Context, slug string) (db.AtendimentoBot, bool, error) {
	bot, err := s.q.BuscarBotPorSlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.AtendimentoBot{}, false, nil
	}
	if err != nil {
		return db.AtendimentoBot{}, false, err
	}
	return bot, true, nil
}
