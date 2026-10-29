package httpapi

// Etapa C da issue #100: o que as telas precisam ler. Três rotas, todas de leitura — o painel do
// serviço, a lista de conversas e uma conversa aberta.
//
// **Quem vê o quê**: o painel é de todo perfil (são contagens), mas a conversa em si é de admin e
// operador. Uma conversa traz o telefone de quem escreveu e o que a pessoa disse — é o mesmo dado
// que fez a tela de Reservas ficar sem o perfil leitura. Contar conversas não é ler conversas.

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Teto da lista: a tela mostra as mais recentes e filtra por bot. Conversa é linha pequena, mas
// não existe motivo para despejar o histórico inteiro num GET.
const maxConversasPorPagina = 200

// GET /atendimento/resumo — os números do painel do serviço.
func (s *Servidor) resumoAtendimento(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	// "Hoje" e "no mês" são no fuso de Brasília, como o resto da plataforma: quem olha a tela
	// às 22h de um dia 31 não quer ver o mês seguinte começando.
	agora := time.Now().In(fusoCanopus)
	inicioDoDia := time.Date(agora.Year(), agora.Month(), agora.Day(), 0, 0, 0, 0, fusoCanopus)
	inicioDoMes := time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, fusoCanopus)

	// DesdeMes é ponteiro porque o sqlc o compara com `encerrada_em`, que é nula.
	resumo, err := s.q.ResumoAtendimento(r.Context(), db.ResumoAtendimentoParams{
		DesdeHoje: inicioDoDia, DesdeMes: &inicioDoMes,
	})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	// Leads (etapa E): é o que o serviço produziu de aproveitável.
	leads, err := s.q.ContarLeads(r.Context(), inicioDoMes)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{
		"bots": map[string]int64{
			"total": resumo.Bots, "ativos": resumo.BotsAtivos, "com_envio": resumo.BotsComEnvio,
		},
		"conversas": map[string]int64{
			"abertas": resumo.ConversasAbertas, "hoje": resumo.ConversasHoje,
			"concluidas_mes": resumo.ConcluidasMes, "pediram_humano_mes": resumo.PediramHumanoMes,
		},
		"fila": map[string]any{
			"pendentes": resumo.FilaPendentes, "falharam": resumo.FilaFalharam,
			"canceladas_mes": resumo.CanceladasMes, "proxima": resumo.ProximaDaFila,
		},
		"leads": map[string]int64{
			"novos": leads.Novos, "aproveitados": leads.Aproveitados,
			"descartados": leads.Descartados, "no_mes": leads.NoMes,
		},
		"dados_coletados_mes": resumo.DadosMes,
		"tokens_mes":          resumo.TokensMes,
		// A chave do servidor: desligada, o serviço inteiro está em ensaio, e é isso que a tela
		// precisa dizer em primeiro lugar — não importa quantos bots estejam ligados.
		"envio_habilitado_servico": s.cfg.AtendimentoEnvioHabilitado,
		// Sem a chave da Anthropic nenhum bot responde. A tela avisa em vez de deixar a pessoa
		// montar um fluxo que nunca vai rodar.
		"modelo_disponivel": s.cfg.ModeloAtendimento != nil,
	})
}

// GET /atendimento/conversas?bot=<id>&situacao=abertas|encerradas|todas&limite=<n>
func (s *Servidor) listarConversas(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	p := db.ListarConversasParams{Limite: 60}
	if v := strings.TrimSpace(r.URL.Query().Get("bot")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			responderErro(w, http.StatusBadRequest, "bot inválido")
			return
		}
		p.BotID = &id
	}
	switch situacao := r.URL.Query().Get("situacao"); situacao {
	case "", "todas":
	case "abertas":
		aberta := "aberta"
		p.Status = &aberta
	case "encerradas":
		encerrada := "encerrada"
		p.Status = &encerrada
	default:
		responderErro(w, http.StatusBadRequest, "situação inválida: use abertas, encerradas ou todas")
		return
	}
	if v := strings.TrimSpace(r.URL.Query().Get("limite")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxConversasPorPagina {
			responderErro(w, http.StatusBadRequest, "limite inválido")
			return
		}
		p.Limite = int32(n)
	}

	linhas, err := s.q.ListarConversas(r.Context(), p)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	type conversaJSON struct {
		ID            int64      `json:"id"`
		BotID         int64      `json:"bot_id"`
		BotNome       string     `json:"bot_nome"`
		BotSlug       string     `json:"bot_slug"`
		Telefone      string     `json:"telefone"`
		ContatoNome   *string    `json:"contato_nome"`
		Status        string     `json:"status"`
		Motivo        *string    `json:"motivo"`
		EtapaNome     *string    `json:"etapa_nome"`
		Mensagens     int64      `json:"mensagens"`
		Dados         int64      `json:"dados"`
		UltimoTexto   string     `json:"ultimo_texto"`
		UltimaDirecao string     `json:"ultima_direcao"`
		Tokens        int32      `json:"tokens"`
		IniciadaEm    time.Time  `json:"iniciada_em"`
		UltimaEm      time.Time  `json:"ultima_em"`
		EncerradaEm   *time.Time `json:"encerrada_em"`
	}
	conversas := make([]conversaJSON, 0, len(linhas))
	for _, l := range linhas {
		conversas = append(conversas, conversaJSON{
			ID: l.ID, BotID: l.BotID, BotNome: l.BotNome, BotSlug: l.BotSlug,
			Telefone: l.Telefone, ContatoNome: l.ContatoNome, Status: l.Status, Motivo: l.Motivo,
			EtapaNome: l.EtapaNome, Mensagens: l.Mensagens, Dados: l.Dados,
			UltimoTexto: l.UltimoTexto, UltimaDirecao: l.UltimaDirecao,
			Tokens:     l.TokensEntrada + l.TokensSaida,
			IniciadaEm: l.IniciadaEm, UltimaEm: l.UltimaEm, EncerradaEm: l.EncerradaEm,
		})
	}
	responderJSON(w, http.StatusOK, map[string]any{"conversas": conversas})
}

// GET /atendimento/conversas/{id} — a conversa inteira: as mensagens nos dois sentidos (inclusive
// a que não saiu, com o motivo) e os dados que saíram dela.
func (s *Servidor) buscarConversa(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	ctx := r.Context()
	c, err := s.q.BuscarConversa(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "conversa não encontrada")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	mensagens, err := s.q.MensagensDaConversa(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	dados, err := s.q.DadosDaConversa(ctx, id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}

	type mensagemJSON struct {
		ID         int64      `json:"id"`
		Direcao    string     `json:"direcao"`
		Texto      string     `json:"texto"`
		Status     string     `json:"status"`
		Motivo     *string    `json:"motivo"`
		Tentativas int32      `json:"tentativas"`
		UltimoErro *string    `json:"ultimo_erro"`
		CriadaEm   time.Time  `json:"criada_em"`
		EnviadaEm  *time.Time `json:"enviada_em"`
	}
	msgs := make([]mensagemJSON, 0, len(mensagens))
	for _, m := range mensagens {
		msgs = append(msgs, mensagemJSON{
			ID: m.ID, Direcao: m.Direcao, Texto: m.Texto, Status: m.Status, Motivo: m.Motivo,
			Tentativas: m.Tentativas, UltimoErro: m.UltimoErro, CriadaEm: m.CriadaEm, EnviadaEm: m.EnviadaEm,
		})
	}
	type dadoJSON struct {
		Campo        string    `json:"campo"`
		Valor        string    `json:"valor"`
		EtapaNome    *string   `json:"etapa_nome"`
		CriadoEm     time.Time `json:"criado_em"`
		AtualizadoEm time.Time `json:"atualizado_em"`
	}
	coletados := make([]dadoJSON, 0, len(dados))
	for _, d := range dados {
		coletados = append(coletados, dadoJSON{
			Campo: d.Campo, Valor: d.Valor, EtapaNome: d.EtapaNome,
			CriadoEm: d.CriadoEm, AtualizadoEm: d.AtualizadoEm,
		})
	}

	// O lead que esta conversa originou (etapa E), para a tela pular direto para a revisão.
	var lead any
	if l, err := s.q.BuscarLeadDaConversa(ctx, id); err == nil {
		lead = map[string]any{"id": l.ID, "status": l.Status, "cliente_id": l.ClienteID}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		erroInterno(w, r, err)
		return
	}

	responderJSON(w, http.StatusOK, map[string]any{
		"lead": lead,
		"conversa": map[string]any{
			"id": c.ID, "bot_id": c.BotID, "bot_nome": c.BotNome, "bot_slug": c.BotSlug,
			"telefone": c.Telefone, "contato_nome": c.ContatoNome,
			"status": c.Status, "motivo": c.Motivo,
			"etapa_nome": c.EtapaNome, "etapa_ordem": c.EtapaOrdem, "etapas_do_bot": c.EtapasDoBot,
			"tokens_entrada": c.TokensEntrada, "tokens_saida": c.TokensSaida,
			"iniciada_em": c.IniciadaEm, "ultima_em": c.UltimaEm, "encerrada_em": c.EncerradaEm,
		},
		"mensagens": msgs,
		"dados":     coletados,
	})
}
