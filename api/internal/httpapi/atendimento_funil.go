package httpapi

// O lead do atendimento sobe para o **Funil de Vendas** (o CRM da Smart) — issue #128, item 3.
//
// Decisão do usuário em 06/11/2026: o envio é **automático quando a conversa encerra**. Foi dito
// a ele que isso tira a triagem da etapa E (o lead ia para uma pessoa revisar antes de virar
// cliente) e que o modelo erra — "sorvete" já virou nome de empresa num teste —, e ele confirmou.
// O que dá para fazer sem contrariar a decisão, e está aqui:
//
//   - **cada lead sobe uma vez só**: o id dele vai no `codigo_interno` da oportunidade, e o
//     estado do envio fica no próprio lead;
//   - **nada sobe retroativo**: o lead entra na fila quando nasce, com a integração já ligada;
//   - **interruptor** na tela de Conexões, que nasce desligado;
//   - **o que o CRM respondeu fica guardado**, inclusive a recusa, porque envio que falha calado
//     é a forma mais cara de perder lead.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
	"github.com/joaofelipe93/travus-plataforma/api/internal/funil"
)

// Quantos leads por ciclo. O CRM é de terceiros: fila grande de uma vez é pedir bloqueio.
const leadsPorCicloFunil = 5

func (s *Servidor) avisarFunil() {
	select {
	case s.acordarFunil <- struct{}{}:
	default:
	}
}

// configFunil lê a linha única; banco fora do ar devolve a integração desligada, que é o padrão
// seguro (não mandar nada).
func (s *Servidor) configFunil(ctx context.Context) db.AtendimentoIntegracaoFunil {
	c, err := s.lerConfigFunil(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("funil: não consegui ler a configuração", "erro", err)
		}
		return db.AtendimentoIntegracaoFunil{}
	}
	return c
}

// lerConfigFunil recria a linha se ela tiver sumido, como a configuração do Canopus.
func (s *Servidor) lerConfigFunil(ctx context.Context) (db.AtendimentoIntegracaoFunil, error) {
	c, err := s.q.ConfiguracaoFunil(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.q.GarantirConfiguracaoFunil(ctx)
	}
	return c, err
}

func codigosDoFunil(c db.AtendimentoIntegracaoFunil) funil.Codigos {
	return funil.Codigos{
		Vendedor: valorOuZero(c.CodigoVendedor), Funil: valorOuZero(c.CodigoFunil),
		Etapa: valorOuZero(c.CodigoEtapa), Canal: valorOuZero(c.CodigoCanal),
		Segmento: valorOuZero(c.CodigoSegmento),
	}
}

func valorOuZero(v *int32) int32 {
	if v == nil {
		return 0
	}
	return *v
}

// marcarLeadParaOFunil põe o lead na fila, se a integração estiver ligada e configurada. Chamada
// no nascimento do lead; falhar aqui não derruba o atendimento (a pessoa já foi respondida).
func (s *Servidor) marcarLeadParaOFunil(ctx context.Context, leadID int64) {
	c := s.configFunil(ctx)
	if !c.Habilitado {
		return
	}
	if !codigosDoFunil(c).Completos() {
		slog.Warn("funil: integração ligada mas sem todos os códigos da conta; lead não entra na fila",
			"lead_id", leadID)
		return
	}
	if s.cfg.FunilChave == "" {
		slog.Warn("funil: integração ligada mas sem a chave no servidor; lead não entra na fila",
			"lead_id", leadID)
		return
	}
	if err := s.q.MarcarLeadParaFunil(ctx, leadID); err != nil {
		slog.Error("funil: falha ao pôr o lead na fila", "lead_id", leadID, "erro", err)
		return
	}
	s.avisarFunil()
}

func (s *Servidor) rodarFilaFunil(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.processarFilaFunil(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.acordarFunil:
		}
	}
}

// processarFilaFunil manda até `leadsPorCicloFunil` leads. Devolve quantos subiram.
func (s *Servidor) processarFilaFunil(ctx context.Context) int {
	c := s.configFunil(ctx)
	if !c.Habilitado || s.cfg.FunilChave == "" {
		return 0
	}
	codigos := codigosDoFunil(c)
	if !codigos.Completos() {
		return 0
	}
	if _, err := s.q.RecuperarEnviosFunilTravados(ctx); err != nil {
		slog.Warn("funil: falha ao recuperar envios travados", "erro", err)
	}
	ids, err := s.q.ReservarLeadsParaFunil(ctx, leadsPorCicloFunil)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("funil: falha ao reservar leads", "erro", err)
		}
		return 0
	}

	cliente := s.clienteFunil()
	enviados := 0
	for _, id := range ids {
		l, err := s.q.DadosDoLeadParaFunil(ctx, id)
		if err != nil {
			s.marcarErroFunil(ctx, id, err)
			continue
		}
		corpo, err := funil.Montar(funil.Lead{
			ID: l.ID, Nome: valorOuVazio(l.Nome), Telefone: valorOuVazio(l.Telefone),
			Email: valorOuVazio(l.Email), Dados: dadosDoLead(l.Dados),
		}, codigos)
		if err != nil {
			s.marcarErroFunil(ctx, id, err)
			continue
		}
		mensagem, err := cliente.Enviar(ctx, corpo)
		if err != nil {
			s.marcarErroFunil(ctx, id, err)
			continue
		}
		if err := s.q.MarcarFunilEnviado(ctx, db.MarcarFunilEnviadoParams{
			ID: id, Resposta: textoOuNil(mensagem),
		}); err != nil {
			// Subiu no CRM e não deu para registrar: o id no `codigo_interno` é o que permite
			// reconhecer a duplicata do lado deles se a fila tentar de novo.
			slog.Error("funil: oportunidade criada, mas não consegui registrar", "lead_id", id, "erro", err)
			continue
		}
		slog.Info("funil: lead enviado ao Funil de Vendas", "lead_id", id, "resposta", mensagem)
		enviados++
	}
	return enviados
}

func (s *Servidor) clienteFunil() *funil.Cliente {
	if s.cfg.FunilCliente != nil {
		return s.cfg.FunilCliente
	}
	return funil.NovoCliente(s.cfg.FunilChave)
}

func (s *Servidor) marcarErroFunil(ctx context.Context, id int64, err error) {
	mensagem := strings.ToValidUTF8(err.Error(), "")
	if utf8.RuneCountInString(mensagem) > 500 {
		mensagem = string([]rune(mensagem)[:500])
	}
	slog.Warn("funil: falha ao enviar o lead", "lead_id", id, "erro", mensagem)
	if err := s.q.MarcarFunilErro(ctx, db.MarcarFunilErroParams{ID: id, Erro: &mensagem}); err != nil {
		slog.Error("funil: falha ao registrar o erro do envio", "lead_id", id, "erro", err)
	}
}

// GET /atendimento/integracao/funil — **só admin**: é conta de terceiros, e quem a configura
// decide para onde vão nome, telefone e o que a pessoa contou (a mesma régua das outras telas de
// Conexões). A chave nunca volta ao navegador: a tela só sabe se ela existe no servidor.
func (s *Servidor) integracaoFunil(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	ctx := r.Context()
	c, err := s.lerConfigFunil(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	resumo, err := s.q.ResumoDoFunil(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{
		"habilitado":        c.Habilitado,
		"chave_presente":    s.cfg.FunilChave != "",
		"codigos_completos": codigosDoFunil(c).Completos(),
		"codigo_vendedor":   c.CodigoVendedor,
		"codigo_funil":      c.CodigoFunil,
		"codigo_etapa":      c.CodigoEtapa,
		"codigo_canal":      c.CodigoCanal,
		"codigo_segmento":   c.CodigoSegmento,
		"atualizado_em":     c.AtualizadoEm,
		"fila": map[string]any{
			"pendentes": resumo.Pendentes, "enviando": resumo.Enviando,
			"com_erro": resumo.ComErro, "enviados_mes": resumo.EnviadosMes,
			"ultimo_erro": resumo.UltimoErro,
		},
	})
}

// PUT /atendimento/integracao/funil — **só admin**. Manda o formulário inteiro, como as outras
// configurações da casa: PUT parcial desligaria sem querer o que não foi mandado.
func (s *Servidor) salvarIntegracaoFunil(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p struct {
		Habilitado     bool   `json:"habilitado"`
		CodigoVendedor *int32 `json:"codigo_vendedor"`
		CodigoFunil    *int32 `json:"codigo_funil"`
		CodigoEtapa    *int32 `json:"codigo_etapa"`
		CodigoCanal    *int32 `json:"codigo_canal"`
		CodigoSegmento *int32 `json:"codigo_segmento"`
	}
	if err := lerJSON(w, r, &p, 1<<13); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	codigos := funil.Codigos{
		Vendedor: valorOuZero(p.CodigoVendedor), Funil: valorOuZero(p.CodigoFunil),
		Etapa: valorOuZero(p.CodigoEtapa), Canal: valorOuZero(p.CodigoCanal),
		Segmento: valorOuZero(p.CodigoSegmento),
	}
	// Ligar sem os códigos deixaria todo lead girando na fila levando recusa do CRM; ligar sem a
	// chave, idem. Melhor recusar aqui, com o motivo na tela.
	if p.Habilitado && !codigos.Completos() {
		responderErro(w, http.StatusBadRequest,
			"para ligar a integração, informe os códigos de vendedor, funil, canal e segmento")
		return
	}
	if p.Habilitado && s.cfg.FunilChave == "" {
		responderErro(w, http.StatusBadRequest,
			"o servidor está sem a chave de integração do Funil de Vendas (FUNIL_CHAVE no .env)")
		return
	}

	ctx := r.Context()
	if _, err := s.lerConfigFunil(ctx); err != nil {
		erroInterno(w, r, err)
		return
	}
	c, err := s.q.AtualizarConfiguracaoFunil(ctx, db.AtualizarConfiguracaoFunilParams{
		Habilitado: p.Habilitado, CodigoVendedor: p.CodigoVendedor, CodigoFunil: p.CodigoFunil,
		CodigoEtapa: p.CodigoEtapa, CodigoCanal: p.CodigoCanal, CodigoSegmento: p.CodigoSegmento,
		AtualizadoPor: &u.ID,
	})
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	s.auditar(ctx, r, &u.ID, "atendimento_funil_configurado", "integracao", "funil_de_vendas", map[string]any{
		"habilitado": c.Habilitado, "codigos_completos": codigosDoFunil(c).Completos(),
	})
	if c.Habilitado {
		s.avisarFunil()
	}
	slog.Info("funil: integração salva", "habilitado", c.Habilitado, "por", u.ID)
	responderJSON(w, http.StatusOK, map[string]any{"habilitado": c.Habilitado})
}

// POST /atendimento/leads/{id}/reenviar-funil — **admin e operador**, a mesma régua de quem mexe
// em lead. Serve para o que falhou nas cinco tentativas: o erro fica na tela, alguém corrige o
// que estava errado (um código, a chave) e manda de novo.
func (s *Servidor) reenviarLeadAoFunil(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	id, ok := idDoCaminho(r)
	if !ok {
		responderErro(w, http.StatusBadRequest, "id inválido")
		return
	}
	ctx := r.Context()
	n, err := s.q.ReenviarLeadAoFunil(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || n == 0 {
		responderErro(w, http.StatusConflict, "este lead não está esperando envio (ou já subiu)")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	s.auditar(ctx, r, &u.ID, "atendimento_lead_reenviado_funil", "lead", idTexto(id), nil)
	s.avisarFunil()
	responderJSON(w, http.StatusOK, map[string]any{"na_fila": true})
}
