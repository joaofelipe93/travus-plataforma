package httpapi

// Configuração do serviço Canopus (tela Canopus → Configurações): dry-run automático, pasta do
// Google Drive e prazos. Fica numa linha de configuracao_canopus, criada pela migração 00012: é
// o que antes era variável de ambiente ou número fixo no código e quem opera precisa ajustar sem
// deploy. Todo perfil vê; admin e operador editam.
//
// O dry-run automático nasce desligado. Quem o liga está pedindo que o worker entre no Newcon no
// dia e na hora marcados — dry-run, que nunca confirma lance.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Fuso do agendamento e dos horários mostrados na tela (o mesmo TZ do compose).
var fusoCanopus = func() *time.Location {
	if l, err := time.LoadLocation("America/Sao_Paulo"); err == nil {
		return l
	}
	return time.Local
}()

// Limites da configuração: os mesmos CHECKs da migração 00012.
const (
	validadeDryRunMinima   = 30 * time.Minute
	validadeDryRunMaxima   = 24 * time.Hour
	retencaoScreenshotsMin = 1
	retencaoScreenshotsMax = 365
)

// Origem da pasta do Drive em vigor.
const (
	pastaDaConfiguracao = "configuracao"
	pastaDoAmbiente     = "ambiente"
	semPasta            = "nenhuma"
)

// ConfigCanopus são os valores em vigor: o banco manda, e o que ele não tem cai no ambiente
// (pasta do Drive) ou no padrão do código.
type ConfigCanopus struct {
	DryRunAutomatico bool
	DryRunDia        int
	DryRunHora       int
	DryRunMinuto     int
	AvisoEmail       bool
	// Pasta do Drive em vigor e de onde ela veio (configuracao, ambiente ou nenhuma).
	DrivePasta          string
	DrivePastaOrigem    string
	ValidadeDryRun      time.Duration
	RetencaoScreenshots time.Duration
	// Resultado do último disparo do agendamento (agendamento.go); vazio: nunca disparou.
	UltimoDisparoResultado string
}

// padroesCanopus: o que vale enquanto ninguém mexeu na configuração (ou quando o banco não
// responde). Os mesmos padrões da migração.
func (s *Servidor) padroesCanopus() ConfigCanopus {
	c := ConfigCanopus{
		DryRunDia: 10, DryRunHora: 8, AvisoEmail: true,
		ValidadeDryRun: s.cfg.ValidadeDryRun, RetencaoScreenshots: s.cfg.RetencaoScreenshots,
		DrivePastaOrigem: semPasta,
	}
	if p := s.cfg.Google.PastaDrive; p != "" {
		c.DrivePasta, c.DrivePastaOrigem = p, pastaDoAmbiente
	}
	return c
}

// lerConfiguracao lê a linha da configuração e a recria com os padrões se ela não existir (um
// DELETE à mão, ou o TRUNCATE em cascata dos testes, levam a isso).
func (s *Servidor) lerConfiguracao(ctx context.Context) (db.BuscarConfiguracaoCanopusRow, error) {
	linha, err := s.q.BuscarConfiguracaoCanopus(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := s.q.GarantirConfiguracaoCanopus(ctx); err != nil {
			return linha, err
		}
		return s.q.BuscarConfiguracaoCanopus(ctx)
	}
	return linha, err
}

// configCanopus lê a configuração em vigor. Banco fora do ar não derruba quem depende dela:
// valem os padrões, com aviso no log.
func (s *Servidor) configCanopus(ctx context.Context) ConfigCanopus {
	linha, err := s.lerConfiguracao(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("configuração do Canopus: usando os padrões", "erro", err)
		}
		return s.padroesCanopus()
	}
	return s.configDaLinha(linha)
}

func (s *Servidor) configDaLinha(l db.BuscarConfiguracaoCanopusRow) ConfigCanopus {
	c := s.padroesCanopus()
	c.DryRunAutomatico = l.DryRunAutomatico
	c.DryRunDia, c.DryRunHora, c.DryRunMinuto = int(l.DryRunDia), int(l.DryRunHora), int(l.DryRunMinuto)
	c.AvisoEmail = l.AvisoEmail
	c.ValidadeDryRun = time.Duration(l.ValidadeDryRunMinutos) * time.Minute
	c.RetencaoScreenshots = time.Duration(l.RetencaoScreenshotsDias) * 24 * time.Hour
	c.UltimoDisparoResultado = valorOuVazio(l.UltimoDisparoResultado)
	if l.DrivePastaID != nil && *l.DrivePastaID != "" {
		c.DrivePasta, c.DrivePastaOrigem = *l.DrivePastaID, pastaDaConfiguracao
	}
	return c
}

// proximoDryRun é a próxima ocorrência do dia/hora escolhidos, no fuso de Brasília. Mês sem o
// dia (31 em fevereiro) usa o último dia do mês; não há calendário de feriados: dia 10 é dia 10,
// mesmo em fim de semana (um dry-run não registra nada no Newcon).
func proximoDryRun(agora time.Time, dia, hora, minuto int) time.Time {
	local := agora.In(fusoCanopus)
	primeiroDoMes := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, fusoCanopus)
	// O mês atual e o seguinte bastam; o terceiro é só cinto de segurança.
	for n := 0; n < 3; n++ {
		quando := ocorrenciaNoMes(primeiroDoMes.AddDate(0, n, 0), dia, hora, minuto)
		if quando.After(local) {
			return quando
		}
	}
	return ocorrenciaNoMes(primeiroDoMes.AddDate(0, 1, 0), dia, hora, minuto)
}

// ocorrenciaNoMes põe o dia/hora escolhidos no mês de "mes", encurtando o dia quando o mês é
// mais curto (31 em fevereiro → 28 ou 29).
func ocorrenciaNoMes(mes time.Time, dia, hora, minuto int) time.Time {
	if ultimo := diasNoMes(mes); dia > ultimo {
		dia = ultimo
	}
	return time.Date(mes.Year(), mes.Month(), dia, hora, minuto, 0, 0, fusoCanopus)
}

func diasNoMes(mes time.Time) int {
	return time.Date(mes.Year(), mes.Month(), 1, 0, 0, 0, 0, mes.Location()).AddDate(0, 1, -1).Day()
}

type configuracaoCanopusJSON struct {
	DryRunAutomatico        bool      `json:"dry_run_automatico"`
	DryRunDia               int       `json:"dry_run_dia"`
	DryRunHora              int       `json:"dry_run_hora"`
	DryRunMinuto            int       `json:"dry_run_minuto"`
	AvisoEmail              bool      `json:"aviso_email"`
	DrivePastaID            *string   `json:"drive_pasta_id"`
	DrivePastaEmVigor       string    `json:"drive_pasta_em_vigor"`
	DrivePastaOrigem        string    `json:"drive_pasta_origem"`
	ValidadeDryRunMinutos   int       `json:"validade_dry_run_minutos"`
	RetencaoScreenshotsDias int       `json:"retencao_screenshots_dias"`
	AtualizadoPorNome       *string   `json:"atualizado_por_nome"`
	AtualizadoEm            time.Time `json:"atualizado_em"`
	// Próxima ocorrência do dia/hora escolhidos, ligada ou não (a tela mostra como prévia).
	ProximoDryRun time.Time `json:"proximo_dry_run"`
	// Último disparo do agendamento e o que aconteceu (criada, sem_cotas, sem_responsavel,
	// atrasada). Ver agendamento.go.
	UltimoDisparoEm        *time.Time `json:"ultimo_disparo_em"`
	UltimoDisparoResultado *string    `json:"ultimo_disparo_resultado"`
}

func (s *Servidor) respostaConfiguracao(l db.BuscarConfiguracaoCanopusRow, u *UsuarioSessao) map[string]any {
	emVigor := s.configDaLinha(l)
	return map[string]any{
		"configuracao": configuracaoCanopusJSON{
			DryRunAutomatico: l.DryRunAutomatico, DryRunDia: int(l.DryRunDia),
			DryRunHora: int(l.DryRunHora), DryRunMinuto: int(l.DryRunMinuto),
			AvisoEmail: l.AvisoEmail, DrivePastaID: l.DrivePastaID,
			DrivePastaEmVigor: emVigor.DrivePasta, DrivePastaOrigem: emVigor.DrivePastaOrigem,
			ValidadeDryRunMinutos: int(l.ValidadeDryRunMinutos), RetencaoScreenshotsDias: int(l.RetencaoScreenshotsDias),
			AtualizadoPorNome: l.AtualizadoPorNome, AtualizadoEm: l.AtualizadoEm,
			ProximoDryRun:          proximoDryRun(s.agora(), int(l.DryRunDia), int(l.DryRunHora), int(l.DryRunMinuto)),
			UltimoDisparoEm:        l.UltimoDisparoEm,
			UltimoDisparoResultado: l.UltimoDisparoResultado,
		},
		"pode_editar": podeEditarConfiguracao(u),
		// O resumo por e-mail depende do SMTP do servidor: a tela avisa quando falta.
		"email_configurado": s.cfg.Vigia.Email != nil,
	}
}

func podeEditarConfiguracao(u *UsuarioSessao) bool {
	return u.Perfil == db.PerfilUsuarioAdmin || u.Perfil == db.PerfilUsuarioOperador
}

func (s *Servidor) configuracaoCanopus(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	l, err := s.lerConfiguracao(r.Context())
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, s.respostaConfiguracao(l, u))
}

// Todos os campos vão juntos (a tela manda o formulário inteiro): um PUT sem um deles seria um
// desligamento silencioso do agendamento.
type pedidoConfiguracaoCanopus struct {
	DryRunAutomatico        *bool   `json:"dry_run_automatico"`
	DryRunDia               *int    `json:"dry_run_dia"`
	DryRunHora              *int    `json:"dry_run_hora"`
	DryRunMinuto            *int    `json:"dry_run_minuto"`
	AvisoEmail              *bool   `json:"aviso_email"`
	DrivePastaID            *string `json:"drive_pasta_id"`
	ValidadeDryRunMinutos   *int    `json:"validade_dry_run_minutos"`
	RetencaoScreenshotsDias *int    `json:"retencao_screenshots_dias"`
}

func (p pedidoConfiguracaoCanopus) validar() (db.AtualizarConfiguracaoCanopusParams, error) {
	var out db.AtualizarConfiguracaoCanopusParams
	presentes := map[string]bool{
		"dry_run_automatico": p.DryRunAutomatico != nil, "dry_run_dia": p.DryRunDia != nil,
		"dry_run_hora": p.DryRunHora != nil, "dry_run_minuto": p.DryRunMinuto != nil,
		"aviso_email": p.AvisoEmail != nil, "validade_dry_run_minutos": p.ValidadeDryRunMinutos != nil,
		"retencao_screenshots_dias": p.RetencaoScreenshotsDias != nil,
	}
	var faltando []string
	for campo, presente := range presentes {
		if !presente {
			faltando = append(faltando, campo)
		}
	}
	if len(faltando) > 0 {
		sort.Strings(faltando)
		return out, erroValidacao("mande a configuração inteira: falta " + strings.Join(faltando, ", "))
	}
	if *p.DryRunDia < 1 || *p.DryRunDia > 31 {
		return out, erroValidacao("o dia do dry-run automático vai de 1 a 31 (mês sem o dia escolhido usa o último dia)")
	}
	if *p.DryRunHora < 0 || *p.DryRunHora > 23 {
		return out, erroValidacao("a hora do dry-run automático vai de 0 a 23")
	}
	if *p.DryRunMinuto < 0 || *p.DryRunMinuto > 59 {
		return out, erroValidacao("o minuto do dry-run automático vai de 0 a 59")
	}
	validade := time.Duration(*p.ValidadeDryRunMinutos) * time.Minute
	if validade < validadeDryRunMinima || validade > validadeDryRunMaxima {
		return out, erroValidacao(fmt.Sprintf("o prazo para aprovar o lance real vai de %d a %d minutos",
			int(validadeDryRunMinima.Minutes()), int(validadeDryRunMaxima.Minutes())))
	}
	if *p.RetencaoScreenshotsDias < retencaoScreenshotsMin || *p.RetencaoScreenshotsDias > retencaoScreenshotsMax {
		return out, erroValidacao(fmt.Sprintf("a retenção dos screenshots vai de %d a %d dias",
			retencaoScreenshotsMin, retencaoScreenshotsMax))
	}
	pasta, err := pastaDoDrive(p.DrivePastaID)
	if err != nil {
		return out, err
	}
	return db.AtualizarConfiguracaoCanopusParams{
		DryRunAutomatico: *p.DryRunAutomatico,
		DryRunDia:        int16(*p.DryRunDia), DryRunHora: int16(*p.DryRunHora), DryRunMinuto: int16(*p.DryRunMinuto),
		AvisoEmail:              *p.AvisoEmail,
		DrivePastaID:            pasta,
		ValidadeDryRunMinutos:   int32(*p.ValidadeDryRunMinutos),
		RetencaoScreenshotsDias: int32(*p.RetencaoScreenshotsDias),
	}, nil
}

var formatoPastaDrive = regexp.MustCompile(`^[A-Za-z0-9_-]{10,200}$`)

// pastaDoDrive aceita o id da pasta ou o endereço copiado do navegador
// ("https://drive.google.com/drive/folders/<id>?usp=sharing"). Em branco (ou ausente): apaga, e
// volta a valer o GOOGLE_DRIVE_PASTA_ID.
func pastaDoDrive(valor *string) (*string, error) {
	if valor == nil {
		return nil, nil
	}
	bruto := strings.TrimSpace(*valor)
	if bruto == "" {
		return nil, nil
	}
	id := bruto
	if strings.ContainsAny(bruto, "/?") {
		u, err := url.Parse(bruto)
		if err != nil {
			return nil, erroValidacao("não reconheci a pasta do Drive: cole o endereço da pasta ou só o id")
		}
		if v := u.Query().Get("id"); v != "" {
			id = v
		} else {
			partes := strings.Split(strings.Trim(u.Path, "/"), "/")
			id = partes[len(partes)-1]
		}
	}
	if !formatoPastaDrive.MatchString(id) {
		return nil, erroValidacao(`pasta do Drive inválida: cole o endereço da pasta ou só o id (letras, números, "-" e "_")`)
	}
	return &id, nil
}

func (s *Servidor) atualizarConfiguracaoCanopus(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoConfiguracaoCanopus
	if err := lerJSON(w, r, &p, 8<<10); err != nil {
		responderErro(w, http.StatusBadRequest, "JSON inválido: mande a configuração inteira")
		return
	}
	params, err := p.validar()
	var invalido erroValidacao
	if errors.As(err, &invalido) {
		responderErro(w, http.StatusUnprocessableEntity, invalido.Error())
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	params.AtualizadoPor = &u.ID

	ctx := r.Context()
	antes, err := s.lerConfiguracao(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if err := s.q.AtualizarConfiguracaoCanopus(ctx, params); err != nil {
		erroInterno(w, r, err)
		return
	}
	depois, err := s.lerConfiguracao(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if alteracoes := mudancasConfiguracao(antes, depois); len(alteracoes) > 0 {
		s.auditar(ctx, r, &u.ID, "configuracao_canopus_atualizada", "configuracao", "canopus",
			map[string]any{"alteracoes": alteracoes})
		// A pasta mudou: o cliente do Drive em memória aponta para a antiga.
		if _, mudou := alteracoes["drive_pasta_id"]; mudou {
			s.esquecerClienteDrive()
			s.avisarDrive()
		}
		// Agendamento ligado agora, ou com outro horário: a ocorrência que já passou não roda
		// retroativo nem vira alerta do vigia (agendamento.go).
		if depois.DryRunAutomatico && horarioMudou(alteracoes) {
			if err := s.marcarOcorrenciaAnterior(ctx, depois); err != nil {
				erroInterno(w, r, err)
				return
			}
			if atualizada, err := s.lerConfiguracao(ctx); err == nil {
				depois = atualizada
			}
		}
	}
	responderJSON(w, http.StatusOK, s.respostaConfiguracao(depois, u))
}

// horarioMudou: o agendamento acabou de ser ligado ou o dia/hora/minuto é outro.
func horarioMudou(alteracoes map[string]any) bool {
	for _, campo := range []string{"dry_run_automatico", "dry_run_dia", "dry_run_hora", "dry_run_minuto"} {
		if _, mudou := alteracoes[campo]; mudou {
			return true
		}
	}
	return false
}

// mudancasConfiguracao: o que mudou, para a auditoria. Nada aqui é segredo — a pasta do Drive é
// um identificador, não uma credencial.
func mudancasConfiguracao(antes, depois db.BuscarConfiguracaoCanopusRow) map[string]any {
	alteracoes := map[string]any{}
	comparar := func(campo string, de, para any) {
		if fmt.Sprint(de) != fmt.Sprint(para) {
			alteracoes[campo] = map[string]any{"de": de, "para": para}
		}
	}
	comparar("dry_run_automatico", antes.DryRunAutomatico, depois.DryRunAutomatico)
	comparar("dry_run_dia", antes.DryRunDia, depois.DryRunDia)
	comparar("dry_run_hora", antes.DryRunHora, depois.DryRunHora)
	comparar("dry_run_minuto", antes.DryRunMinuto, depois.DryRunMinuto)
	comparar("aviso_email", antes.AvisoEmail, depois.AvisoEmail)
	comparar("drive_pasta_id", valorOuVazio(antes.DrivePastaID), valorOuVazio(depois.DrivePastaID))
	comparar("validade_dry_run_minutos", antes.ValidadeDryRunMinutos, depois.ValidadeDryRunMinutos)
	comparar("retencao_screenshots_dias", antes.RetencaoScreenshotsDias, depois.RetencaoScreenshotsDias)
	return alteracoes
}
