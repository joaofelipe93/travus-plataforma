package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/assistente"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Ferramentas do assistente. Todas só LEEM. Leitura: estado e números do Canopus. Operador e
// admin: também reservas (dados de hóspedes, como na tela Reservas) e a consulta livre.
func (s *Servidor) ferramentasAssistente(u *UsuarioSessao) []assistente.Ferramenta {
	fs := []assistente.Ferramenta{s.ferramentaEstado(), s.ferramentaResumoCanopus()}
	if podeVerReservas(u.Perfil) {
		fs = append(fs, s.ferramentaReservas())
		if s.cfg.Assistente.Consultor != nil {
			fs = append(fs, ferramentaConsultarBanco(s.cfg.Assistente.Consultor))
		}
	}
	return fs
}

var propriedadeData = func(descricao string) map[string]any {
	return map[string]any{"type": "string", "description": descricao + " Formato AAAA-MM-DD, fuso de Brasília."}
}

// dataDoAssistente: "AAAA-MM-DD" no fuso de Brasília; fim=true devolve o início do dia seguinte
// (o "até" é inclusivo para quem pergunta).
func dataDoAssistente(valor string, fim bool) (*time.Time, error) {
	if strings.TrimSpace(valor) == "" {
		return nil, nil
	}
	d, err := time.ParseInLocation(diaSemHorario, strings.TrimSpace(valor), assistente.Fuso)
	if err != nil {
		return nil, fmt.Errorf("data inválida %q: use AAAA-MM-DD", valor)
	}
	if fim {
		d = d.AddDate(0, 0, 1)
	}
	return &d, nil
}

func lerEntrada(entrada json.RawMessage, destino any) error {
	if len(entrada) == 0 || string(entrada) == "null" {
		return nil
	}
	if err := json.Unmarshal(entrada, destino); err != nil {
		return fmt.Errorf("entrada inválida: %v", err)
	}
	return nil
}

func emJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// --- estado_plataforma

func (s *Servidor) ferramentaEstado() assistente.Ferramenta {
	return assistente.Ferramenta{
		Nome: "estado_plataforma",
		Descricao: "Situação atual da plataforma: versão publicada, alertas ativos do vigia (o que está com problema e desde quando), " +
			"último contato do robô do Canopus (worker), se o lance real está habilitado, configuração do Google Drive e " +
			"estado do WhatsApp do notificador de reservas (conectado, grupo, fila de mensagens). Sem parâmetros.",
		Rotulo: "Conferindo o estado da plataforma",
		Executar: func(ctx context.Context, _ json.RawMessage) (string, error) {
			agora := s.agora()
			estado := map[string]any{
				"versao":                valorOuPadrao(s.cfg.Versao, "dev"),
				"commit":                valorOuPadrao(s.cfg.Commit, "desconhecido"),
				"api_no_ar_desde":       s.inicio.In(assistente.Fuso).Format(time.RFC3339),
				"lance_real_habilitado": s.cfg.LanceRealHabilitado,
			}

			worker := map[string]any{"ultimo_contato": nil}
			if n := s.ultimoContatoWorker.Load(); n != 0 {
				visto := time.Unix(0, n)
				worker["ultimo_contato"] = visto.In(assistente.Fuso).Format(time.RFC3339)
				worker["ha_minutos"] = int(agora.Sub(visto).Minutes())
				worker["em_contato"] = agora.Sub(visto) < semContatoWorker
			} else {
				worker["observacao"] = "o worker não falou com a API desde que ela subiu"
			}
			estado["worker_canopus"] = worker

			g := s.cfg.Google
			drive := map[string]any{
				"client_configurado": g.ClientID != "" && g.ClientSecret != "",
				"pasta_configurada":  g.PastaDrive != "",
				"token_importado":    false,
			}
			if integ, err := s.q.BuscarIntegracao(ctx, IntegracaoGoogleDrive); err == nil {
				drive["token_importado"] = true
				drive["token_atualizado_em"] = integ.AtualizadoEm.In(assistente.Fuso).Format(time.RFC3339)
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return "", errors.New("falha ao ler a integração do Google Drive")
			}
			estado["google_drive"] = drive

			alertas := []map[string]any{}
			s.vigiaMu.Lock()
			for _, a := range s.alertas {
				alertas = append(alertas, map[string]any{
					"titulo": a.Titulo, "detalhe": a.Detalhe, "desde": a.desde.In(assistente.Fuso).Format(time.RFC3339),
				})
			}
			s.vigiaMu.Unlock()
			sort.Slice(alertas, func(i, j int) bool { return alertas[i]["desde"].(string) < alertas[j]["desde"].(string) })
			estado["alertas_ativos"] = alertas
			estado["vigia_observacao"] = "o vigia confere a cada 5 minutos; lista vazia = nada com problema na última verificação"

			estado["whatsapp_notificador"] = s.estadoWhatsappParaAssistente(ctx)
			return emJSON(estado)
		},
	}
}

func (s *Servidor) estadoWhatsappParaAssistente(ctx context.Context) map[string]any {
	if s.cfg.Checkin == nil {
		return map[string]any{"estado": "nao_configurado"}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	st, err := s.cfg.Checkin.Status(ctx)
	if err != nil {
		return map[string]any{"estado": "indisponivel", "observacao": "o notificador não respondeu (fora do ar ou desligado neste ambiente)"}
	}
	// Sem o QR nem o número da conta: não servem para responder e dão acesso ao WhatsApp.
	w := map[string]any{"estado": st.Status, "conectado": st.Conectado, "fila": st.Fila}
	if st.Grupo != nil {
		nome := ""
		if st.Grupo.Nome != nil {
			nome = *st.Grupo.Nome
		}
		w["grupo"] = nome
	} else {
		w["grupo"] = nil
	}
	return w
}

// --- resumo_canopus

func (s *Servidor) ferramentaResumoCanopus() assistente.Ferramenta {
	return assistente.Ferramenta{
		Nome: "resumo_canopus",
		Descricao: "Números do Canopus num período: clientes e cotas cadastrados (sempre o total atual), execuções por tipo " +
			"(dry_run, real, reimpressao) e situação, cotas processadas por situação, lances registrados (pela plataforma ou " +
			"trazidos do Histórico), PDFs de comprovante e envio ao Google Drive (enviados, na fila, com erro), e as 10 " +
			"execuções mais recentes. Sem datas, conta o histórico inteiro.",
		Parametros: map[string]any{
			"desde": propriedadeData("Primeiro dia do período (opcional)."),
			"ate":   propriedadeData("Último dia do período, inclusive (opcional)."),
		},
		Rotulo: "Somando os números do Canopus",
		Executar: func(ctx context.Context, entrada json.RawMessage) (string, error) {
			var p struct {
				Desde string `json:"desde"`
				Ate   string `json:"ate"`
			}
			if err := lerEntrada(entrada, &p); err != nil {
				return "", err
			}
			desde, err := dataDoAssistente(p.Desde, false)
			if err != nil {
				return "", err
			}
			ate, err := dataDoAssistente(p.Ate, true)
			if err != nil {
				return "", err
			}

			cadastro, err := s.q.AssistenteCadastro(ctx)
			if err != nil {
				return "", errors.New("falha ao ler o cadastro")
			}
			execs, err := s.q.AssistenteExecucoesPorTipo(ctx, db.AssistenteExecucoesPorTipoParams{Desde: desde, Ate: ate})
			if err != nil {
				return "", errors.New("falha ao ler as execuções")
			}
			cotas, err := s.q.AssistenteCotasPorStatus(ctx, db.AssistenteCotasPorStatusParams{Desde: desde, Ate: ate})
			if err != nil {
				return "", errors.New("falha ao ler as cotas das execuções")
			}
			lances, err := s.q.AssistenteLances(ctx, db.AssistenteLancesParams{Desde: desde, Ate: ate})
			if err != nil {
				return "", errors.New("falha ao ler os lances")
			}
			ultimas, err := s.q.AssistenteUltimasExecucoes(ctx)
			if err != nil {
				return "", errors.New("falha ao ler as últimas execuções")
			}

			porTipo := map[string]map[string]int32{}
			totalPorTipo := map[string]int32{}
			for _, e := range execs {
				if porTipo[e.Tipo] == nil {
					porTipo[e.Tipo] = map[string]int32{}
				}
				porTipo[e.Tipo][e.Status] = e.Quantidade
				totalPorTipo[e.Tipo] += e.Quantidade
			}
			cotasPorTipo := map[string]map[string]int32{}
			for _, c := range cotas {
				if cotasPorTipo[c.Tipo] == nil {
					cotasPorTipo[c.Tipo] = map[string]int32{}
				}
				cotasPorTipo[c.Tipo][c.Status] = c.Quantidade
			}
			recentes := make([]map[string]any, 0, len(ultimas))
			for _, e := range ultimas {
				item := map[string]any{
					"id": e.ID, "tipo": e.Tipo, "status": e.Status, "criada_por": e.CriadaPor,
					"criada_em": e.CriadaEm.In(assistente.Fuso).Format(time.RFC3339),
					"cotas":     e.Cotas, "cotas_com_erro": e.CotasComErro,
				}
				if e.FinalizadaEm != nil {
					item["finalizada_em"] = e.FinalizadaEm.In(assistente.Fuso).Format(time.RFC3339)
				}
				if e.Erro != nil {
					item["erro"] = *e.Erro
				}
				recentes = append(recentes, item)
			}

			periodo := map[string]any{"desde": nil, "ate": nil}
			if p.Desde != "" {
				periodo["desde"] = p.Desde
			}
			if p.Ate != "" {
				periodo["ate"] = p.Ate
			}
			return emJSON(map[string]any{
				"periodo":                   periodo,
				"cadastro_atual":            cadastro,
				"execucoes_total_por_tipo":  totalPorTipo,
				"execucoes_por_tipo_status": porTipo,
				"cotas_processadas":         cotasPorTipo,
				"lances":                    lances,
				"ultimas_execucoes":         recentes,
				"observacao":                "execuções e cotas contam pela data de criação da execução; lances pela data de registro",
			})
		},
	}
}

// --- listar_reservas

const (
	reservasPadrao = 30
	reservasMaximo = 100
)

func (s *Servidor) ferramentaReservas() assistente.Ferramenta {
	return assistente.Ferramenta{
		Nome: "listar_reservas",
		Descricao: "Reservas da pousada recebidas do PMS (o mesmo da tela Reservas), com reserva e cancelamento juntos. Filtra por " +
			"data de check-in, por data em que a reserva chegou, por situação e por texto (hóspede, imóvel ou canal). Devolve " +
			"os totais do filtro (quantidade, confirmadas, canceladas, noites, soma dos valores, por imóvel e por canal) e " +
			"as reservas (as que chegaram por último primeiro), com o aviso no grupo do WhatsApp (enviada, pendente, falhou, " +
			"resumo = entrou no resumo diário, sem_mensagem).",
		Parametros: map[string]any{
			"check_in_desde": propriedadeData("Check-in a partir de (opcional)."),
			"check_in_ate":   propriedadeData("Check-in até, inclusive (opcional)."),
			"recebida_desde": propriedadeData("Reserva recebida a partir de (opcional). Use para \"reservas feitas hoje\"."),
			"recebida_ate":   propriedadeData("Reserva recebida até, inclusive (opcional)."),
			"situacao": map[string]any{"type": "string", "enum": []string{"confirmada", "cancelada", "todas"},
				"description": "Padrão: todas."},
			"busca":  map[string]any{"type": "string", "description": "Trecho do nome do hóspede, do imóvel ou do canal (opcional)."},
			"limite": map[string]any{"type": "integer", "description": fmt.Sprintf("Reservas na lista (padrão %d, máximo %d). Os totais contam todas.", reservasPadrao, reservasMaximo)},
		},
		Rotulo: "Buscando as reservas",
		Executar: func(ctx context.Context, entrada json.RawMessage) (string, error) {
			var p struct {
				CheckInDesde  string `json:"check_in_desde"`
				CheckInAte    string `json:"check_in_ate"`
				RecebidaDesde string `json:"recebida_desde"`
				RecebidaAte   string `json:"recebida_ate"`
				Situacao      string `json:"situacao"`
				Busca         string `json:"busca"`
				Limite        int    `json:"limite"`
			}
			if err := lerEntrada(entrada, &p); err != nil {
				return "", err
			}
			for _, d := range []string{p.CheckInDesde, p.CheckInAte} {
				if _, err := dataDoAssistente(d, false); err != nil {
					return "", err
				}
			}
			recDesde, err := dataDoAssistente(p.RecebidaDesde, false)
			if err != nil {
				return "", err
			}
			recAte, err := dataDoAssistente(p.RecebidaAte, true)
			if err != nil {
				return "", err
			}
			if p.Limite <= 0 {
				p.Limite = reservasPadrao
			}
			p.Limite = min(p.Limite, reservasMaximo)

			todas, limiteAtingido, err := s.carregarReservas(ctx)
			if err != nil {
				return "", errors.New("falha ao ler as reservas")
			}
			busca := strings.ToLower(strings.TrimSpace(p.Busca))
			filtradas := make([]reservaTela, 0)
			for _, r := range todas {
				if p.Situacao == "confirmada" || p.Situacao == "cancelada" {
					if r.Situacao != p.Situacao {
						continue
					}
				}
				if p.CheckInDesde != "" && (r.CheckIn == nil || *r.CheckIn < p.CheckInDesde) {
					continue
				}
				if p.CheckInAte != "" && (r.CheckIn == nil || *r.CheckIn > p.CheckInAte) {
					continue
				}
				if recDesde != nil && r.RecebidaEm.Before(*recDesde) {
					continue
				}
				if recAte != nil && !r.RecebidaEm.Before(*recAte) {
					continue
				}
				if busca != "" && !contemTexto(busca, r.Hospede, r.Imovel, r.Canal) {
					continue
				}
				filtradas = append(filtradas, r)
			}
			return emJSON(resumirReservas(filtradas, p.Limite, limiteAtingido))
		},
	}
}

func contemTexto(busca string, campos ...*string) bool {
	for _, c := range campos {
		if c != nil && strings.Contains(strings.ToLower(*c), busca) {
			return true
		}
	}
	return false
}

func resumirReservas(rs []reservaTela, limite int, limiteAtingido bool) map[string]any {
	var confirmadas, canceladas, noites int
	var somaCentavos int64
	semValor := 0
	porImovel := map[string]int{}
	porCanal := map[string]int{}
	for _, r := range rs {
		if r.Situacao == situacaoCancela {
			canceladas++
		} else {
			confirmadas++
			if r.Noites != nil {
				noites += *r.Noites
			}
			if r.ValorCentavos != nil {
				somaCentavos += *r.ValorCentavos
			} else {
				semValor++
			}
		}
		porImovel[textoOuTraco(r.Imovel)]++
		porCanal[textoOuTraco(r.Canal)]++
	}
	lista := make([]map[string]any, 0, min(limite, len(rs)))
	for _, r := range rs[:min(limite, len(rs))] {
		item := map[string]any{
			"situacao": r.Situacao, "hospede": r.Hospede, "imovel": r.Imovel, "canal": r.Canal,
			"check_in": r.CheckIn, "check_out": r.CheckOut, "noites": r.Noites, "hospedes": r.Hospedes,
			"telefone": r.Telefone, "recebida_em": r.RecebidaEm.In(assistente.Fuso).Format(time.RFC3339),
			"aviso_no_grupo": r.Mensagem,
		}
		if r.ValorCentavos != nil {
			item["valor"] = reais(*r.ValorCentavos)
		}
		if r.CanceladaEm != nil {
			item["cancelada_em"] = r.CanceladaEm.In(assistente.Fuso).Format(time.RFC3339)
			item["aviso_do_cancelamento"] = r.MensagemCancelamento
			item["motivo_cancelamento"] = r.MotivoCancelamento
		}
		lista = append(lista, item)
	}
	resp := map[string]any{
		"total":                  len(rs),
		"confirmadas":            confirmadas,
		"canceladas":             canceladas,
		"noites_das_confirmadas": noites,
		"soma_valor_confirmadas": reais(somaCentavos),
		"confirmadas_sem_valor":  semValor,
		"por_imovel":             porImovel,
		"por_canal":              porCanal,
		"reservas":               lista,
		"mostrando":              len(lista),
	}
	if limiteAtingido {
		resp["observacao"] = fmt.Sprintf("só os %d eventos mais recentes do PMS foram lidos: reservas muito antigas podem faltar", limiteEventosReserva)
	}
	return resp
}

func textoOuTraco(s *string) string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return "(sem informação)"
	}
	return *s
}

// reais formata centavos como "R$ 1.234,56".
func reais(centavos int64) string {
	sinal := ""
	if centavos < 0 {
		sinal, centavos = "-", -centavos
	}
	inteiro := fmt.Sprint(centavos / 100)
	var b strings.Builder
	for i, c := range inteiro {
		if i > 0 && (len(inteiro)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return fmt.Sprintf("%sR$ %s,%02d", sinal, b.String(), centavos%100)
}

// --- consultar_banco

func ferramentaConsultarBanco(c *assistente.Consultor) assistente.Ferramenta {
	return assistente.Ferramenta{
		Nome: "consultar_banco",
		Descricao: "Roda UM comando SELECT (ou WITH … SELECT) no PostgreSQL da plataforma com um papel só de leitura e devolve " +
			"colunas e linhas em JSON (no máximo 200 linhas; \"cortado\": true quando havia mais). Enxerga só as tabelas e " +
			"colunas do esquema descrito nas instruções. Tempo limite de 5 s. Prefira agregar (count, sum, group by).",
		Parametros: map[string]any{
			"sql": map[string]any{"type": "string", "description": "A consulta SQL (um comando só, sem ponto e vírgula no meio)."},
		},
		Obrigatorio: []string{"sql"},
		Rotulo:      "Consultando o banco de dados",
		Executar: func(ctx context.Context, entrada json.RawMessage) (string, error) {
			var p struct {
				SQL string `json:"sql"`
			}
			if err := lerEntrada(entrada, &p); err != nil {
				return "", err
			}
			res, err := c.Consultar(ctx, p.SQL)
			if err != nil {
				return "", err
			}
			return emJSON(res)
		},
	}
}
