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
//   - **nada sai sem as duas chaves ligadas** (a do servidor e a do bot). Desligadas, a resposta
//     é composta e gravada como `cancelada` com o motivo: é o modo ensaio, e é assim que se vê o
//     que o bot diria sem mandar nada a ninguém;
//   - **teto de tokens por conversa** e **teto de respostas por hora por bot**: o primeiro
//     encerra a conversa, o segundo cancela a resposta. Conversa automática dos dois lados é um
//     laço infinito à espera de acontecer, e o número é o que paga a conta;
//   - **a etapa só avança quando os campos obrigatórios estão coletados** — quem confere é
//     `atendimento.CamposPendentes`, não o modelo.

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
	ConversaID  int64   `json:"conversa_id"`
	MensagemID  *int64  `json:"mensagem_id,omitempty"`
	Status      string  `json:"status"` // pendente | cancelada | sem_resposta
	Motivo      string  `json:"motivo,omitempty"`
	Encerrada   bool    `json:"encerrada"`
	EtapaAtual  *string `json:"etapa_atual,omitempty"`
	TokensGasto int32   `json:"tokens_gasto"`
}

// sessaoBot junta o que as ferramentas precisam mexer durante uma resposta. As ferramentas
// gravam no banco na hora (coleta perdida é pergunta repetida) e anotam aqui o que a camada de
// cima precisa decidir depois.
type sessaoBot struct {
	s        *Servidor
	bot      db.AtendimentoBot
	conversa db.AtendimentoConversa
	etapas   []db.AtendimentoEtapa
	// Índice da etapa atual em `etapas`; -1 quando a conversa não está em etapa nenhuma.
	indice   int
	coletado map[string]string

	avancou  bool
	encerrou bool
	motivo   string
}

// etapaAtual devolve a etapa da conversa, ou nil quando o bot não tem fluxo (ou ele acabou).
func (z *sessaoBot) etapaAtual() *db.AtendimentoEtapa {
	if z.indice < 0 || z.indice >= len(z.etapas) {
		return nil
	}
	return &z.etapas[z.indice]
}

// paraPrompt traduz a etapa do banco no que o pacote atendimento precisa para montar o texto.
func (z *sessaoBot) paraPrompt() (*atendimento.EtapaPrompt, error) {
	e := z.etapaAtual()
	if e == nil {
		return nil, nil
	}
	campos := []atendimento.Campo{}
	if len(e.Campos) > 0 {
		if err := json.Unmarshal(e.Campos, &campos); err != nil {
			return nil, fmt.Errorf("campos da etapa %d: %w", e.ID, err)
		}
	}
	return &atendimento.EtapaPrompt{
		Nome: e.Nome, Objetivo: e.Objetivo, Instrucoes: e.Instrucoes, Campos: campos,
		Posicao: z.indice + 1, Total: len(z.etapas),
	}, nil
}

// ferramentas do bot. São três de propósito: coletar, avançar e encerrar. Nenhuma delas fala com
// o mundo (nada de mandar e-mail, criar cliente ou consultar outro serviço) — o que sai desta
// conversa é texto para a pessoa e dado no banco, e a etapa E decide o que fazer com o dado.
func (z *sessaoBot) ferramentas(ctx context.Context) []assistente.Ferramenta {
	return []assistente.Ferramenta{
		{
			Nome: "registrar_dado",
			Descricao: "Guarda um dado que a pessoa informou. Chame assim que ela disser, uma vez por dado. " +
				"Use só os identificadores de campo listados nas instruções da etapa.",
			Parametros: map[string]any{
				"campo": map[string]any{"type": "string", "description": "O identificador do campo, como está nas instruções da etapa."},
				"valor": map[string]any{"type": "string", "description": "O que a pessoa informou, em texto, sem reformatar."},
			},
			Obrigatorio: []string{"campo", "valor"},
			Rotulo:      "Registrando o que a pessoa informou",
			Executar:    z.registrarDado,
		},
		{
			Nome: "avancar_etapa",
			Descricao: "Passa para a próxima etapa do atendimento. Só funciona quando os campos obrigatórios " +
				"da etapa atual já foram registrados; se faltar algum, a resposta diz quais.",
			Parametros:  map[string]any{},
			Obrigatorio: nil,
			Rotulo:      "Avançando o atendimento",
			Executar:    z.avancarEtapa,
		},
		{
			Nome: "encerrar",
			Descricao: "Encerra o atendimento. Use quando o fluxo terminou, quando a pessoa desistiu, quando " +
				"ela pediu para falar com uma pessoa da equipe, ou quando o assunto não é o seu.",
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

func (z *sessaoBot) registrarDado(ctx context.Context, entrada json.RawMessage) (string, error) {
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
	// Campo de fora da etapa é recusado: o modelo inventaria chave e a tela mostraria dado que
	// ninguém pediu. A mensagem diz o que vale, e o modelo corrige sozinho.
	prompt, err := z.paraPrompt()
	if err != nil {
		return "", err
	}
	if prompt != nil {
		conhecido := false
		nomes := make([]string, 0, len(prompt.Campos))
		for _, c := range prompt.Campos {
			nomes = append(nomes, c.ID)
			if c.ID == campo {
				conhecido = true
			}
		}
		if !conhecido {
			return "", fmt.Errorf("campo %q não existe nesta etapa (os desta etapa são: %s)", campo, strings.Join(nomes, ", "))
		}
	}
	var etapaID *int64
	if e := z.etapaAtual(); e != nil {
		etapaID = &e.ID
	}
	if err := z.s.q.SalvarColeta(ctx, db.SalvarColetaParams{
		ConversaID: z.conversa.ID, EtapaID: etapaID, Campo: campo, Valor: valor,
	}); err != nil {
		return "", err
	}
	z.coletado[campo] = valor
	faltam := atendimento.CamposPendentes(prompt, z.coletado)
	if len(faltam) > 0 {
		return fmt.Sprintf(`{"ok":true,"ainda_faltam":%q}`, strings.Join(faltam, ", ")), nil
	}
	return `{"ok":true,"etapa_completa":true}`, nil
}

func (z *sessaoBot) avancarEtapa(ctx context.Context, _ json.RawMessage) (string, error) {
	prompt, err := z.paraPrompt()
	if err != nil {
		return "", err
	}
	if faltam := atendimento.CamposPendentes(prompt, z.coletado); len(faltam) > 0 {
		// Não é erro de sistema: é o modelo tentando correr. A mensagem volta para ele e a
		// conversa continua na mesma etapa.
		return "", fmt.Errorf("ainda faltam estes dados obrigatórios: %s", strings.Join(faltam, ", "))
	}
	if z.indice < 0 || z.indice+1 >= len(z.etapas) {
		// Última etapa: avançar é terminar.
		z.encerrou, z.motivo = true, "concluido"
		return `{"ok":true,"fim_do_fluxo":true}`, nil
	}
	z.indice++
	z.avancou = true
	nova := z.etapas[z.indice]
	if _, err := z.s.q.DefinirEtapaDaConversa(ctx, db.DefinirEtapaDaConversaParams{
		ID: z.conversa.ID, EtapaID: &nova.ID,
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf(`{"ok":true,"etapa":%q,"objetivo":%q}`, nova.Nome, nova.Objetivo), nil
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

	etapas, err := s.q.ListarEtapasDoBot(ctx, bot.ID)
	if err != nil {
		return resultadoAtendimento{}, err
	}
	var primeira *int64
	if len(etapas) > 0 {
		primeira = &etapas[0].ID
	}
	conversa, err := s.q.AbrirOuContinuarConversa(ctx, db.AbrirOuContinuarConversaParams{
		BotID: bot.ID, Telefone: telefone, ContatoNome: textoOuNil(contato), EtapaID: primeira,
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

	z := &sessaoBot{s: s, bot: bot, conversa: conversa, etapas: etapas, indice: -1, coletado: map[string]string{}}
	for i, e := range etapas {
		if conversa.EtapaID != nil && e.ID == *conversa.EtapaID {
			z.indice = i
		}
	}
	coletas, err := s.q.ColetasDaConversa(ctx, conversa.ID)
	if err != nil {
		return res, err
	}
	for _, c := range coletas {
		z.coletado[c.Campo] = c.Valor
	}
	prompt, err := z.paraPrompt()
	if err != nil {
		return res, err
	}
	historico, err := s.historicoParaModelo(ctx, conversa.ID)
	if err != nil {
		return res, err
	}

	agente := &assistente.Agente{
		Modelo: s.cfg.ModeloAtendimento(bot.Modelo),
		Instrucoes: atendimento.Prompt(
			atendimento.BotPrompt{Nome: bot.Nome, Instrucoes: bot.Instrucoes}, prompt, z.coletado),
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

	if e := z.etapaAtual(); e != nil {
		nome := e.Nome
		res.EtapaAtual = &nome
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
