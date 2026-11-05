package httpapi

// Configuração do serviço de boletos (tela Canopus → Boletos): o agendamento das rodadas e o
// interruptor do envio no WhatsApp. Mesmo desenho da configuração do Canopus: um PUT com o
// formulário inteiro (PUT parcial é recusado, para ninguém desligar o agendamento sem querer).
//
// **Ligar o envio é só do admin**, como aprovar lance real: é a opção que faz a plataforma
// escrever no WhatsApp de um cliente. Operador muda o resto.

import (
	"net/http"
	"strings"
	"time"

	"github.com/joaofelipe93/travus-plataforma/api/internal/calendario"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
	"github.com/joaofelipe93/travus-plataforma/api/internal/mensagem"
)

type pedidoConfigBoletos struct {
	AgendamentoAtivo   *bool `json:"agendamento_ativo"`
	DiaEnvio           *int  `json:"dia_envio"`
	HoraEnvio          *int  `json:"hora_envio"`
	DiaCobranca        *int  `json:"dia_cobranca"`
	HoraCobranca       *int  `json:"hora_cobranca"`
	AnteciparDiaUtil   *bool `json:"antecipar_dia_util"`
	EnvioHabilitado    *bool `json:"envio_habilitado"`
	PausaEnvioSegundos *int  `json:"pausa_envio_segundos"`
	MaxMensagensRodada *int  `json:"max_mensagens_rodada"`
	// Modelos das mensagens: a mesma para todos, com os campos entre chaves.
	TextoEnvio    *string `json:"texto_envio"`
	TextoCobranca *string `json:"texto_cobranca"`
}

type configBoletosJSON struct {
	AgendamentoAtivo   bool   `json:"agendamento_ativo"`
	DiaEnvio           int    `json:"dia_envio"`
	HoraEnvio          int    `json:"hora_envio"`
	DiaCobranca        int    `json:"dia_cobranca"`
	HoraCobranca       int    `json:"hora_cobranca"`
	AnteciparDiaUtil   bool   `json:"antecipar_dia_util"`
	EnvioHabilitado    bool   `json:"envio_habilitado"`
	PausaEnvioSegundos int    `json:"pausa_envio_segundos"`
	MaxMensagensRodada int    `json:"max_mensagens_rodada"`
	TextoEnvio         string `json:"texto_envio"`
	TextoCobranca      string `json:"texto_cobranca"`
	// Nomes de quem mexeu, para a tela não precisar buscar usuário.
	EnvioHabilitadoPorNome string    `json:"envio_habilitado_por_nome"`
	AtualizadoPorNome      string    `json:"atualizado_por_nome"`
	AtualizadoEm           time.Time `json:"atualizado_em"`
	// Próximas ocorrências já com a antecipação aplicada (é o que a tela mostra).
	ProximoEnvio    string `json:"proximo_envio"`
	ProximaCobranca string `json:"proxima_cobranca"`
	// Motivo da antecipação, quando houver ("sábado", "Natal"...).
	MotivoEnvio    string `json:"motivo_envio"`
	MotivoCobranca string `json:"motivo_cobranca"`
}

func (s *Servidor) configuracaoBoletos(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	ctx := r.Context()
	c := s.configBoletos(ctx)
	fila, err := s.q.EstadoFilaBoletos(ctx)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]any{
		"configuracao": s.configBoletosParaJSON(c),
		"pode_editar":  podeEditarBoletos(u),
		// Só admin liga o envio no WhatsApp dos clientes.
		"pode_ligar_envio": u.Perfil == db.PerfilUsuarioAdmin,
		// Chave do servidor: desligada, nada sai nem com a opção ligada.
		"envio_habilitado_servidor": s.cfg.BoletosEnvioHabilitado,
		// O que a tela precisa para montar a ajuda dos campos, a prévia e o "voltar ao padrão".
		"mensagens": map[string]any{
			"campos_envio":    mensagem.CamposEnvio,
			"campos_cobranca": mensagem.CamposCobranca,
			"padrao_envio":    mensagem.PadraoEnvio,
			"padrao_cobranca": mensagem.PadraoCobranca,
			"max_caracteres":  mensagem.MaxTexto,
		},
		"fila": map[string]any{
			"pendentes": fila.Pendentes, "falharam": fila.Falharam, "proxima": fila.Proxima,
		},
	})
}

func podeEditarBoletos(u *UsuarioSessao) bool {
	return u.Perfil == db.PerfilUsuarioAdmin || u.Perfil == db.PerfilUsuarioOperador
}

func (s *Servidor) configBoletosParaJSON(c ConfigBoletos) configBoletosJSON {
	j := configBoletosJSON{
		AgendamentoAtivo: c.AgendamentoAtivo, DiaEnvio: c.DiaEnvio, HoraEnvio: c.HoraEnvio,
		DiaCobranca: c.DiaCobranca, HoraCobranca: c.HoraCobranca,
		AnteciparDiaUtil: c.AnteciparDiaUtil, EnvioHabilitado: c.EnvioHabilitado,
		PausaEnvioSegundos: int(c.PausaEnvio / time.Second), MaxMensagensRodada: c.MaxMensagensRodada,
		TextoEnvio: c.TextoEnvio, TextoCobranca: c.TextoCobranca,
		EnvioHabilitadoPorNome: c.EnvioHabilitadoPorNm, AtualizadoPorNome: c.AtualizadoPorNome,
		AtualizadoEm: c.AtualizadoEm,
	}
	agora := s.agora().In(calendario.Fuso)
	if envio, motivo, ok := proximaOcorrencia(agora, c.DiaEnvio, c.HoraEnvio, c.AnteciparDiaUtil); ok {
		j.ProximoEnvio, j.MotivoEnvio = envio.Format(time.RFC3339), motivo
	}
	if cob, motivo, ok := proximaOcorrencia(agora, c.DiaCobranca, c.HoraCobranca, c.AnteciparDiaUtil); ok {
		j.ProximaCobranca, j.MotivoCobranca = cob.Format(time.RFC3339), motivo
	}
	return j
}

// proximaOcorrencia: a do mês atual se ainda não passou, senão a do mês que vem. O motivo
// explica a antecipação ("14/11 cai no sábado") — é o que a tela mostra ao lado da data.
func proximaOcorrencia(agora time.Time, dia, hora int, antecipar bool) (time.Time, string, bool) {
	for i := range 2 {
		mes := agora.AddDate(0, i, 0)
		quando, err := calendario.Ocorrencia(mes.Year(), mes.Month(), dia, hora, antecipar)
		if err != nil {
			return time.Time{}, "", false
		}
		if quando.After(agora) {
			return quando, motivoDaAntecipacao(mes.Year(), mes.Month(), dia, quando, antecipar), true
		}
	}
	return time.Time{}, "", false
}

func motivoDaAntecipacao(ano int, mes time.Month, dia int, quando time.Time, antecipar bool) string {
	if !antecipar {
		return ""
	}
	original, err := calendario.DiaDoMes(ano, mes, dia, false)
	if err != nil || original.Format(time.DateOnly) == quando.Format(time.DateOnly) {
		return ""
	}
	if nome := calendario.Feriado(original); nome != "" {
		return "o dia " + original.Format("02/01") + " é feriado (" + nome + ")"
	}
	switch original.Weekday() {
	case time.Saturday:
		return "o dia " + original.Format("02/01") + " cai no sábado"
	case time.Sunday:
		return "o dia " + original.Format("02/01") + " cai no domingo"
	}
	return "o dia " + original.Format("02/01") + " não é dia útil"
}

func (s *Servidor) atualizarConfiguracaoBoletos(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoConfigBoletos
	if err := lerJSON(w, r, &p, 8<<10); err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	// Formulário inteiro: PUT parcial desligaria o que não veio.
	faltando := p.AgendamentoAtivo == nil || p.DiaEnvio == nil || p.HoraEnvio == nil ||
		p.DiaCobranca == nil || p.HoraCobranca == nil || p.AnteciparDiaUtil == nil ||
		p.EnvioHabilitado == nil || p.PausaEnvioSegundos == nil || p.MaxMensagensRodada == nil ||
		p.TextoEnvio == nil || p.TextoCobranca == nil
	if faltando {
		responderErro(w, http.StatusBadRequest, "mande a configuração inteira: um PUT parcial desligaria o que não veio")
		return
	}
	for campo, valor := range map[string]int{"dia_envio": *p.DiaEnvio, "dia_cobranca": *p.DiaCobranca} {
		if valor < 1 || valor > 31 {
			responderErro(w, http.StatusBadRequest, campo+" precisa ser um dia entre 1 e 31")
			return
		}
	}
	for campo, valor := range map[string]int{"hora_envio": *p.HoraEnvio, "hora_cobranca": *p.HoraCobranca} {
		if valor < 0 || valor > 23 {
			responderErro(w, http.StatusBadRequest, campo+" precisa ser uma hora entre 0 e 23")
			return
		}
	}
	if *p.PausaEnvioSegundos < 5 || *p.PausaEnvioSegundos > 600 {
		responderErro(w, http.StatusBadRequest,
			"a pausa entre mensagens precisa ficar entre 5 e 600 segundos: é o que protege o número de ser bloqueado")
		return
	}
	if *p.MaxMensagensRodada < 1 || *p.MaxMensagensRodada > 5000 {
		responderErro(w, http.StatusBadRequest, "o teto de mensagens por rodada precisa ficar entre 1 e 5000")
		return
	}
	// O texto chega a um cliente: campo inventado ({nmoe}) é recusado aqui, não descoberto lá.
	if err := mensagem.Validar(*p.TextoEnvio, mensagem.CamposEnvio); err != nil {
		responderErro(w, http.StatusBadRequest, "mensagem do boleto: "+err.Error())
		return
	}
	if err := mensagem.Validar(*p.TextoCobranca, mensagem.CamposCobranca); err != nil {
		responderErro(w, http.StatusBadRequest, "mensagem da cobrança: "+err.Error())
		return
	}

	ctx := r.Context()
	atual := s.configBoletos(ctx)
	// Ligar ou desligar o envio no WhatsApp dos clientes é só do admin (a configuração não pode
	// ser a porta dos fundos da permissão da tela).
	if *p.EnvioHabilitado != atual.EnvioHabilitado && u.Perfil != db.PerfilUsuarioAdmin {
		responderErro(w, http.StatusForbidden, "só um admin liga ou desliga o envio dos boletos no WhatsApp")
		return
	}
	if *p.EnvioHabilitado && !s.cfg.BoletosEnvioHabilitado {
		responderErro(w, http.StatusConflict,
			"a chave BOLETOS_ENVIO_HABILITADO do servidor está desligada: ligue nela primeiro (deploy/.env) e publique")
		return
	}

	if err := s.q.AtualizarConfiguracaoBoletos(ctx, db.AtualizarConfiguracaoBoletosParams{
		AgendamentoAtivo: *p.AgendamentoAtivo,
		DiaEnvio:         int16(*p.DiaEnvio),
		HoraEnvio:        int16(*p.HoraEnvio),
		DiaCobranca:      int16(*p.DiaCobranca),
		HoraCobranca:     int16(*p.HoraCobranca),
		AnteciparDiaUtil: *p.AnteciparDiaUtil,
		EnvioHabilitado:  *p.EnvioHabilitado,
		// Guarda quem ligou (só muda quando passa de desligado para ligado).
		EnvioHabilitadoPor: &u.ID,
		PausaEnvioSegundos: int16(*p.PausaEnvioSegundos),
		MaxMensagensRodada: int32(*p.MaxMensagensRodada),
		TextoEnvio:         strings.TrimSpace(*p.TextoEnvio),
		TextoCobranca:      strings.TrimSpace(*p.TextoCobranca),
		AtualizadoPor:      &u.ID,
	}); err != nil {
		erroInterno(w, r, err)
		return
	}

	mudou := map[string]any{}
	if *p.AgendamentoAtivo != atual.AgendamentoAtivo {
		mudou["agendamento_ativo"] = *p.AgendamentoAtivo
	}
	if *p.EnvioHabilitado != atual.EnvioHabilitado {
		mudou["envio_habilitado"] = *p.EnvioHabilitado
	}
	if *p.DiaEnvio != atual.DiaEnvio || *p.HoraEnvio != atual.HoraEnvio {
		mudou["envio"] = map[string]int{"dia": *p.DiaEnvio, "hora": *p.HoraEnvio}
	}
	if *p.DiaCobranca != atual.DiaCobranca || *p.HoraCobranca != atual.HoraCobranca {
		mudou["cobranca"] = map[string]int{"dia": *p.DiaCobranca, "hora": *p.HoraCobranca}
	}
	// O texto inteiro não vai para a auditoria (é longo e aparece na rodada): fica o aviso de
	// que alguém mexeu na mensagem que chega ao cliente.
	if strings.TrimSpace(*p.TextoEnvio) != atual.TextoEnvio {
		mudou["texto_envio"] = "alterado"
	}
	if strings.TrimSpace(*p.TextoCobranca) != atual.TextoCobranca {
		mudou["texto_cobranca"] = "alterado"
	}
	s.auditar(ctx, r, &u.ID, "boletos_configuracao_atualizada", "boletos_configuracao", "1", mudou)

	// Ligar o agendamento (ou mudar o horário) fecha a ocorrência que já passou: nada roda
	// retroativo e o vigia não reclama de algo que ninguém tinha agendado.
	if *p.AgendamentoAtivo && !atual.AgendamentoAtivo {
		s.fecharOcorrenciasAnteriores(ctx)
	}
	s.configuracaoBoletos(w, r, u)
}
