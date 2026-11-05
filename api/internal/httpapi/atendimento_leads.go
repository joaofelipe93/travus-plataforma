package httpapi

// Etapa E da issue #100: o lead. A conversa que colheu algo vira uma linha que alguém revisa e
// promove a cliente no cadastro — ou descarta com motivo.
//
// **Quem promove é gente, não o bot** (decisão do usuário em 29/10/2026, junto com a de que os
// dados ficam na própria plataforma). O bot conversa e colhe; transformar aquilo num cliente é
// decisão de quem atende, porque o modelo pode ter entendido o nome errado, a pessoa pode ser
// alguém que já está no cadastro, e cliente criado por engano suja o cadastro de todos os
// serviços.
//
// A revisão é de **admin e operador**: o lead é dado pessoal de quem escreveu (nome, telefone,
// e-mail), a mesma régua da conversa e da tela de Reservas.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/cadastro"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
	"github.com/joaofelipe93/travus-plataforma/api/internal/servicos"
)

// Campos que, se o bot colheu, já entram preenchidos na revisão. O resto das coletas vai para
// `dados` e aparece na tela do mesmo jeito — só não tem campo próprio.
//
// Os nomes são os que os pontos de partida geram (internal/atendimento/pontos_de_partida.go) e
// os que a tela sugere ao digitar a pergunta: se alguém chamar o dado de outra coisa, ele não se
// perde — só não vem pré-preenchido na revisão.
var (
	// Nome do lead: em venda para empresa (plano de saúde empresarial, por exemplo), o cliente
	// do cadastro **é a empresa** — por isso `nome_da_empresa` conta como nome.
	camposDeNome     = []string{"nome", "nome_completo", "nome_do_cliente", "nome_da_empresa", "razao_social"}
	camposDeTelefone = []string{"telefone", "celular", "whatsapp", "telefone_de_contato", "melhor_telefone", "melhor_contato"}
	camposDeEmail    = []string{"email", "e_mail", "email_de_contato"}
)

func primeiroPreenchido(dados map[string]string, nomes []string) *string {
	for _, n := range nomes {
		if v := strings.TrimSpace(dados[n]); v != "" {
			copia := v
			return &copia
		}
	}
	return nil
}

// criarLeadSePreciso é chamado quando uma conversa encerra. Cria o lead se ela colheu alguma
// coisa; conversa que não rendeu nada não vira lead (senão a tela de leads viraria a lista de
// toda conversa que passou).
//
// Falha aqui **não derruba o atendimento**: a conversa já encerrou e a pessoa já foi respondida.
// O lead é trabalho interno — o erro vai para o log.
func (s *Servidor) criarLeadSePreciso(ctx context.Context, bot db.AtendimentoBot, conversaID int64, motivo string) {
	coletas, err := s.q.ColetasDaConversa(ctx, conversaID)
	if err != nil {
		slog.Error("atendimento: não consegui ler as coletas para o lead", "conversa_id", conversaID, "erro", err)
		return
	}
	if len(coletas) == 0 {
		return
	}
	dados := make(map[string]string, len(coletas))
	for _, c := range coletas {
		dados[c.Campo] = c.Valor
	}
	bruto, err := json.Marshal(dados)
	if err != nil {
		slog.Error("atendimento: não consegui montar os dados do lead", "conversa_id", conversaID, "erro", err)
		return
	}

	// O telefone do WhatsApp é o que temos de mais confiável: a pessoa escreveu dele. Um
	// telefone que ela ditou na conversa entra como observação, não substitui este.
	conversa, err := s.q.BuscarConversa(ctx, conversaID)
	if err != nil {
		slog.Error("atendimento: não consegui ler a conversa para o lead", "conversa_id", conversaID, "erro", err)
		return
	}
	telefone := conversa.Telefone
	nome := primeiroPreenchido(dados, camposDeNome)
	if nome == nil && conversa.ContatoNome != nil && strings.TrimSpace(*conversa.ContatoNome) != "" {
		// Nome do WhatsApp: serve de ponto de partida, e a revisão confirma.
		nome = conversa.ContatoNome
	}
	var observacoes *string
	if outro := primeiroPreenchido(dados, camposDeTelefone); outro != nil && *outro != telefone {
		texto := fmt.Sprintf("Na conversa, a pessoa informou outro telefone: %s", *outro)
		observacoes = &texto
	}

	lead, err := s.q.CriarLeadDaConversa(ctx, db.CriarLeadDaConversaParams{
		ConversaID: conversaID, BotID: bot.ID,
		Nome: nome, Telefone: &telefone, Email: primeiroPreenchido(dados, camposDeEmail),
		Observacoes: observacoes, Dados: bruto,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Já havia lead desta conversa (ela encerrou duas vezes, ou alguém reabriu): não é erro.
		return
	}
	if err != nil {
		slog.Error("atendimento: falha ao criar o lead", "conversa_id", conversaID, "erro", err)
		return
	}
	slog.Info("atendimento: lead criado", "lead_id", lead.ID, "bot", bot.Slug,
		"conversa_id", conversaID, "motivo", motivo, "dados", len(dados))
}

type leadJSON struct {
	ID          int64             `json:"id"`
	ConversaID  int64             `json:"conversa_id"`
	BotID       int64             `json:"bot_id"`
	BotNome     string            `json:"bot_nome"`
	BotSlug     string            `json:"bot_slug"`
	Status      string            `json:"status"`
	Nome        *string           `json:"nome"`
	Telefone    *string           `json:"telefone"`
	Email       *string           `json:"email"`
	Observacoes *string           `json:"observacoes"`
	Dados       map[string]string `json:"dados"`
	// Da conversa que o originou, para a revisão ter contexto sem abrir outra tela.
	ContatoNome      *string `json:"contato_nome"`
	ConversaTelefone string  `json:"conversa_telefone"`
	ConversaStatus   string  `json:"conversa_status"`
	ConversaMotivo   *string `json:"conversa_motivo"`
	Mensagens        int64   `json:"mensagens"`
	UltimaEm         string  `json:"ultima_em"`
	// Da decisão.
	ClienteID       *int64  `json:"cliente_id"`
	ClienteNome     *string `json:"cliente_nome"`
	MotivoDescarte  *string `json:"motivo_descarte"`
	RevisadoPorNome *string `json:"revisado_por_nome"`
	RevisadoEm      *string `json:"revisado_em"`
	CriadoEm        string  `json:"criado_em"`
}

func dadosDoLead(bruto []byte) map[string]string {
	dados := map[string]string{}
	if len(bruto) > 0 {
		_ = json.Unmarshal(bruto, &dados)
	}
	return dados
}

func horaOuNil(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

func leadDaLinha(l db.ListarLeadsRow) leadJSON {
	return leadJSON{
		ID: l.ID, ConversaID: l.ConversaID, BotID: l.BotID, BotNome: l.BotNome, BotSlug: l.BotSlug,
		Status: l.Status, Nome: l.Nome, Telefone: l.Telefone, Email: l.Email, Observacoes: l.Observacoes,
		Dados:       dadosDoLead(l.Dados),
		ContatoNome: l.ContatoNome, ConversaTelefone: l.ConversaTelefone,
		ConversaStatus: l.ConversaStatus, ConversaMotivo: l.ConversaMotivo,
		Mensagens: l.Mensagens, UltimaEm: l.UltimaEm.Format(time.RFC3339),
		ClienteID: l.ClienteID, ClienteNome: l.ClienteNome, MotivoDescarte: l.MotivoDescarte,
		RevisadoPorNome: l.RevisadoPorNome, RevisadoEm: horaOuNil(l.RevisadoEm),
		CriadoEm: l.CriadoEm.Format(time.RFC3339),
	}
}

// GET /atendimento/leads?situacao=novos|aproveitados|descartados|todos&bot=<id>&limite=<n>
func (s *Servidor) listarLeads(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	p := db.ListarLeadsParams{Limite: 100}
	switch situacao := r.URL.Query().Get("situacao"); situacao {
	case "", "todos":
	case "novos":
		v := "novo"
		p.Status = &v
	case "aproveitados":
		v := "aproveitado"
		p.Status = &v
	case "descartados":
		v := "descartado"
		p.Status = &v
	default:
		responderErro(w, http.StatusBadRequest, "situação inválida: use novos, aproveitados, descartados ou todos")
		return
	}
	if v := strings.TrimSpace(r.URL.Query().Get("bot")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			responderErro(w, http.StatusBadRequest, "bot inválido")
			return
		}
		p.BotID = &id
	}
	if v := strings.TrimSpace(r.URL.Query().Get("limite")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 300 {
			responderErro(w, http.StatusBadRequest, "limite inválido")
			return
		}
		p.Limite = int32(n)
	}

	linhas, err := s.q.ListarLeads(r.Context(), p)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	leads := make([]leadJSON, 0, len(linhas))
	for _, l := range linhas {
		leads = append(leads, leadDaLinha(l))
	}
	contagem, err := s.q.ContarLeads(r.Context(), inicioDoMesBrasilia())
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{
		"leads": leads,
		"contagem": map[string]int64{
			"novos": contagem.Novos, "aproveitados": contagem.Aproveitados,
			"descartados": contagem.Descartados, "no_mes": contagem.NoMes,
		},
	})
}

// buscarLeadJSON carrega um lead pelo id do caminho. Devolve false já tendo respondido.
func (s *Servidor) leadDoCaminho(w http.ResponseWriter, r *http.Request) (db.BuscarLeadRow, bool) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusBadRequest, "id inválido")
		return db.BuscarLeadRow{}, false
	}
	l, err := s.q.BuscarLead(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusNotFound, "lead não encontrado")
		return db.BuscarLeadRow{}, false
	}
	if err != nil {
		erroInterno(w, r, err)
		return db.BuscarLeadRow{}, false
	}
	return l, true
}

// GET /atendimento/leads/{id}
func (s *Servidor) buscarLeadNaTela(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	l, ok := s.leadDoCaminho(w, r)
	if !ok {
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{"lead": leadDaLinha(db.ListarLeadsRow(l))})
}

// PATCH /atendimento/leads/{id} — a correção da revisão (quem revisa tem a conversa ao lado).
func (s *Servidor) atualizarLead(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	l, ok := s.leadDoCaminho(w, r)
	if !ok {
		return
	}
	if l.Status != "novo" {
		responderErro(w, http.StatusConflict, "este lead já foi decidido: não se edita mais")
		return
	}
	var p struct {
		Nome        string `json:"nome"`
		Telefone    string `json:"telefone"`
		Email       string `json:"email"`
		Observacoes string `json:"observacoes"`
	}
	if err := lerJSON(w, r, &p, 16<<10); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	// Mesma validação do cadastro: o e-mail torto que o modelo entendeu é recusado aqui, e não
	// na hora de criar o cliente.
	email, err := cadastro.Email(p.Email)
	if err != nil {
		responderErroCadastro(w, r, err)
		return
	}
	atualizado, err := s.q.AtualizarLead(r.Context(), db.AtualizarLeadParams{
		ID: l.ID, Nome: textoOuNil(p.Nome), Telefone: textoOuNil(p.Telefone),
		Email: email, Observacoes: textoOuNil(p.Observacoes),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusConflict, "este lead já foi decidido: não se edita mais")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	s.auditar(r.Context(), r, &u.ID, "lead_atualizado", "lead", idTexto(atualizado.ID), nil)
	s.responderLead(w, r, atualizado.ID)
}

// POST /atendimento/leads/{id}/aproveitar — vira cliente no cadastro.
//
// Dois caminhos: criar um cliente novo (o padrão) ou **apontar para um cliente que já existe**
// (`cliente_id`), que é o caso comum de alguém da carteira escrevendo no WhatsApp. Sem o segundo,
// a tela empurraria todo mundo a criar cliente repetido — e o cadastro recusa nome repetido com
// 409, o que viraria um beco sem saída.
//
// O cliente criado aqui nasce **ativo em nenhum serviço**: um lead de atendimento não entra em
// dry-run de lance nem em rodada de boletos por ter sido cadastrado. Quem liga cada serviço é
// uma pessoa, na tela do cliente.
func (s *Servidor) aproveitarLead(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	l, ok := s.leadDoCaminho(w, r)
	if !ok {
		return
	}
	if l.Status != "novo" {
		responderErro(w, http.StatusConflict, "este lead já foi decidido")
		return
	}
	var p struct {
		// Quando vem, o lead é ligado a este cliente e nada é criado.
		ClienteID *int64 `json:"cliente_id"`
	}
	if err := lerJSON(w, r, &p, 4<<10); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	var clienteID int64
	var clienteNome string
	criado := false

	if p.ClienteID != nil {
		cliente, err := s.q.BuscarCliente(ctx, *p.ClienteID)
		if errors.Is(err, pgx.ErrNoRows) {
			responderErro(w, http.StatusBadRequest, "cliente não encontrado")
			return
		}
		if err != nil {
			erroInterno(w, r, err)
			return
		}
		clienteID, clienteNome = cliente.ID, cliente.Nome
	} else {
		nome, normalizado, err := cadastro.Nome(valorOuVazio(l.Nome))
		if err != nil {
			responderErroCadastro(w, r, err)
			return
		}
		email, err := cadastro.Email(valorOuVazio(l.Email))
		if err != nil {
			responderErroCadastro(w, r, err)
			return
		}
		telefone, err := cadastro.Texto("telefone", valorOuVazio(l.Telefone))
		if err != nil {
			responderErroCadastro(w, r, err)
			return
		}

		tx, err := s.pool.Begin(ctx)
		if err != nil {
			erroInterno(w, r, err)
			return
		}
		defer tx.Rollback(ctx) //nolint:errcheck // rollback depois do commit é no-op
		qtx := s.q.WithTx(tx)

		cliente, err := qtx.CriarClienteCadastro(ctx, db.CriarClienteCadastroParams{
			Nome: nome, NomeNormalizado: normalizado, Telefone: telefone, Email: email,
		})
		if err != nil {
			// Nome repetido vira 409 com a mensagem do cadastro: a tela oferece ligar ao
			// cliente existente em vez de insistir.
			responderErroCadastro(w, r, err)
			return
		}
		// Ativo em nenhum serviço: ver o comentário do handler.
		for _, cat := range servicos.Todos {
			if _, err := qtx.DefinirServicoDoCliente(ctx, db.DefinirServicoDoClienteParams{
				ClienteID: cliente.ID, Servico: cat.ID, Ativo: false, AlteradoPor: &u.ID,
			}); err != nil {
				erroInterno(w, r, err)
				return
			}
		}
		if err := tx.Commit(ctx); err != nil {
			erroInterno(w, r, err)
			return
		}
		clienteID, clienteNome, criado = cliente.ID, cliente.Nome, true
		s.auditar(ctx, r, &u.ID, "cliente_criado", "cliente", idTexto(cliente.ID), map[string]any{
			"nome": cliente.Nome, "origem": "lead_atendimento", "lead": l.ID, "servicos": []string{},
		})
	}

	lead, err := s.q.AproveitarLead(ctx, db.AproveitarLeadParams{
		ID: l.ID, ClienteID: &clienteID, RevisadoPor: &u.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusConflict, "este lead já foi decidido")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	s.auditar(ctx, r, &u.ID, "lead_aproveitado", "lead", idTexto(lead.ID), map[string]any{
		"cliente": clienteNome, "cliente_id": clienteID, "cliente_criado": criado,
	})
	slog.Info("atendimento: lead aproveitado", "lead_id", lead.ID, "cliente_id", clienteID,
		"cliente_criado", criado, "por", u.ID)
	s.responderLead(w, r, lead.ID)
}

// POST /atendimento/leads/{id}/descartar — com motivo, porque "por que isto não virou cliente?"
// é a pergunta que alguém faz um mês depois.
func (s *Servidor) descartarLead(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	l, ok := s.leadDoCaminho(w, r)
	if !ok {
		return
	}
	if l.Status != "novo" {
		responderErro(w, http.StatusConflict, "este lead já foi decidido")
		return
	}
	var p struct {
		Motivo string `json:"motivo"`
	}
	if err := lerJSON(w, r, &p, 4<<10); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	motivo := strings.TrimSpace(p.Motivo)
	if motivo == "" {
		responderErro(w, http.StatusBadRequest, "diga por que está descartando")
		return
	}
	if len(motivo) > 500 {
		motivo = motivo[:500]
	}
	lead, err := s.q.DescartarLead(r.Context(), db.DescartarLeadParams{
		ID: l.ID, MotivoDescarte: &motivo, RevisadoPor: &u.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		responderErro(w, http.StatusConflict, "este lead já foi decidido")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	s.auditar(r.Context(), r, &u.ID, "lead_descartado", "lead", idTexto(lead.ID),
		map[string]any{"motivo": motivo})
	s.responderLead(w, r, lead.ID)
}

// responderLead devolve o lead recarregado, com o nome do cliente e de quem revisou.
func (s *Servidor) responderLead(w http.ResponseWriter, r *http.Request, id int64) {
	l, err := s.q.BuscarLead(r.Context(), id)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{"lead": leadDaLinha(db.ListarLeadsRow(l))})
}

// inicioDoMesBrasilia é o começo do mês no fuso em que a plataforma pensa.
func inicioDoMesBrasilia() time.Time {
	agora := time.Now().In(fusoCanopus)
	return time.Date(agora.Year(), agora.Month(), 1, 0, 0, 0, 0, fusoCanopus)
}
