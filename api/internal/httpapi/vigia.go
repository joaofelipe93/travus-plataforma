package httpapi

// Vigia (Etapa 4): checagens periódicas que viram alertas por e-mail. Cobre o que a API
// enxerga de dentro da VM; a VM inteira fora do ar é papel do monitor externo, que recebe um
// ping a cada ciclo (VIGIA_PING_URL) e avisa quando os pings param.

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joaofelipe93/travus-plataforma/api/internal/alerta"
)

type ConfigVigia struct {
	// Sem e-mail, os alertas só vão para o log.
	Email alerta.Enviador
	// URL de ping do monitor externo (ex.: healthchecks.io), chamada quando o banco responde.
	PingURL string
	// Pasta dos backups montada só para leitura: último backup, falhas e uso do disco.
	BackupDir string
	// host:porta cujo certificado HTTPS é conferido (ex.: app.exemplo.com.br:443).
	CertificadoHost string
	Intervalo       time.Duration
	// Fração do disco (0 a 1) acima da qual há alerta. Padrão 0,8.
	LimiteDisco float64
}

const (
	semContatoWorker  = 10 * time.Minute
	backupAtrasado    = 26 * time.Hour
	certificadoMinimo = 14 * 24 * time.Hour
	lembreteAlerta    = 12 * time.Hour
)

type achado struct {
	Chave   string
	Titulo  string
	Detalhe string
	// Evento: não tem "resolvido" nem lembrete (ex.: cota em erro_apos_confirmar).
	Evento bool
}

type alertaAtivo struct {
	achado
	desde     time.Time
	avisadoEm time.Time
}

func (s *Servidor) rodarVigia(ctx context.Context) {
	t := time.NewTicker(s.cfg.Vigia.Intervalo)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.cicloVigia(ctx)
	}
}

func (s *Servidor) cicloVigia(ctx context.Context) {
	achados, bancoOk := s.verificarSaude(ctx)
	s.notificar(ctx, achados, bancoOk)
	if bancoOk && s.cfg.Vigia.PingURL != "" {
		s.pingMonitor(ctx)
	}
}

// verificarSaude devolve os problemas encontrados e se o banco respondeu (sem banco, as
// checagens que dependem dele não rodam).
func (s *Servidor) verificarSaude(ctx context.Context) ([]achado, bool) {
	var out []achado
	agora := s.agora()
	ctxBanco, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	bancoOk := true
	if err := s.ping.Ping(ctxBanco); err != nil {
		bancoOk = false
		out = append(out, achado{Chave: "banco", Titulo: "Banco de dados inacessível", Detalhe: err.Error()})
	}

	ultimo := s.inicio
	if n := s.ultimoContatoWorker.Load(); n != 0 {
		ultimo = time.Unix(0, n)
	}
	if d := agora.Sub(ultimo); d > semContatoWorker {
		out = append(out, achado{
			Chave:   "worker",
			Titulo:  "Worker Canopus sem contato com a API há " + textoTempo(d),
			Detalhe: "O container worker-canopus parou ou não alcança a API: nenhuma execução anda (make logs s=worker-canopus).",
		})
	}

	if bancoOk {
		out = append(out, s.verificarBanco(ctxBanco)...)
	}
	out = append(out, s.verificarBackup(agora)...)
	if a := s.verificarCertificado(ctx, agora); a != nil {
		out = append(out, *a)
	}
	return out, bancoOk
}

func (s *Servidor) verificarBanco(ctx context.Context) []achado {
	var out []achado
	if f, err := s.q.VigiaFila(ctx); err != nil {
		slog.Error("vigia: consulta da fila", "erro", err)
	} else {
		if f.NaFilaAntigas > 0 {
			out = append(out, achado{
				Chave:   "fila",
				Titulo:  fmt.Sprintf("%d execução(ões) na fila há mais de 30 min", f.NaFilaAntigas),
				Detalhe: "O worker não está pegando execuções (desligado, sem as credenciais do Newcon ou com outra execução travada).",
			})
		}
		if f.TravasVencidas > 0 {
			out = append(out, achado{
				Chave:   "trava",
				Titulo:  fmt.Sprintf("%d execução(ões) em andamento sem sinal do worker há mais de 5 min", f.TravasVencidas),
				Detalhe: "O worker parou no meio de uma execução.",
			})
		}
	}

	if cotas, err := s.q.VigiaCotasAposConfirmar(ctx); err != nil {
		slog.Error("vigia: consulta das cotas", "erro", err)
	} else {
		for _, c := range cotas {
			out = append(out, achado{
				Chave:  fmt.Sprintf("apos-confirmar:%d", c.ID),
				Titulo: fmt.Sprintf("Cota %s-%s-%s (execução nº %d): erro depois de clicar em Confirmar", c.Grupo, c.Cota, c.Versao, c.ExecucaoID),
				Detalhe: fmt.Sprintf("%s\nConfira no Histórico do Newcon se o lance foi registrado: %s/execucoes/%d",
					valorOuVazio(c.Erro), s.cfg.AppOrigin, c.ExecucaoID),
				Evento: true,
			})
		}
	}

	if d, err := s.q.VigiaDrive(ctx); err != nil {
		slog.Error("vigia: consulta do Drive", "erro", err)
	} else {
		if d.Desistidos > 0 {
			out = append(out, achado{
				Chave:   "drive-desistiu",
				Titulo:  fmt.Sprintf("%d comprovante(s) não foram para o Google Drive depois de 5 tentativas", d.Desistidos),
				Detalhe: "O erro aparece no comprovante, na tela do cliente; lá há o botão Tentar de novo.",
			})
		}
		if d.Atrasados > 0 {
			out = append(out, achado{
				Chave:   "drive-atrasado",
				Titulo:  fmt.Sprintf("%d comprovante(s) esperando o envio ao Google Drive há mais de 1 h", d.Atrasados),
				Detalhe: "Confira o token, o client OAuth e a pasta com make google-status.",
			})
		}
	}
	return out
}

func (s *Servidor) verificarBackup(agora time.Time) []achado {
	dir := s.cfg.Vigia.BackupDir
	if dir == "" {
		return nil
	}
	var out []achado
	ok, errOk := os.Stat(filepath.Join(dir, "ultimo-ok"))
	switch {
	case errOk == nil && agora.Sub(ok.ModTime()) > backupAtrasado:
		out = append(out, achado{
			Chave:   "backup",
			Titulo:  "Backup do Postgres atrasado",
			Detalhe: "O último backup completo foi em " + ok.ModTime().In(time.Local).Format("02/01/2006 15:04") + " (make logs s=backup).",
		})
	case errOk != nil && agora.Sub(s.inicio) > backupAtrasado:
		out = append(out, achado{
			Chave:   "backup",
			Titulo:  "Nenhum backup do Postgres encontrado",
			Detalhe: "Não há " + filepath.Join(dir, "ultimo-ok") + ": o serviço backup está rodando?",
		})
	}
	if e, err := os.Stat(filepath.Join(dir, "ultimo-erro")); err == nil && (errOk != nil || e.ModTime().After(ok.ModTime())) {
		texto, _ := os.ReadFile(filepath.Join(dir, "ultimo-erro"))
		if len(texto) > 500 {
			texto = texto[:500]
		}
		out = append(out, achado{Chave: "backup-erro", Titulo: "O último backup do Postgres falhou", Detalhe: strings.TrimSpace(strings.ToValidUTF8(string(texto), ""))})
	}

	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err == nil && st.Blocks > 0 {
		usado := 1 - float64(st.Bavail)/float64(st.Blocks)
		if usado > s.cfg.Vigia.LimiteDisco {
			out = append(out, achado{
				Chave:   "disco",
				Titulo:  fmt.Sprintf("Disco da VM com %.0f%% de uso", usado*100),
				Detalhe: "Apague backups antigos que já foram copiados (make backup-baixar) ou aumente o disco.",
			})
		}
	}
	return out
}

func (s *Servidor) verificarCertificado(ctx context.Context, agora time.Time) *achado {
	alvo := s.cfg.Vigia.CertificadoHost
	if alvo == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(alvo)
	if err != nil {
		host, alvo = alvo, net.JoinHostPort(alvo, "443")
	}
	ctxTLS, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	discador := tls.Dialer{Config: &tls.Config{ServerName: host, RootCAs: s.vigiaRaizes, MinVersion: tls.VersionTLS12}}
	conn, err := discador.DialContext(ctxTLS, "tcp", alvo)
	if err != nil {
		return &achado{Chave: "certificado", Titulo: "Não consegui conferir o certificado HTTPS de " + host, Detalhe: err.Error()}
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return &achado{Chave: "certificado", Titulo: "Não consegui conferir o certificado HTTPS de " + host, Detalhe: "o servidor não mandou certificado"}
	}
	if resta := certs[0].NotAfter.Sub(agora); resta < certificadoMinimo {
		return &achado{
			Chave:   "certificado",
			Titulo:  fmt.Sprintf("Certificado HTTPS de %s vence em %d dia(s)", host, int(resta.Hours()/24)),
			Detalhe: "O Traefik renova sozinho 30 dias antes do vencimento: se chegou aqui, a renovação está falhando (make logs s=traefik).",
		}
	}
	return nil
}

// notificar compara com os alertas já avisados e manda um e-mail só com o que mudou: novos,
// lembretes (a cada 12 h) e resolvidos. Sem o banco (completo=false), nada é dado como
// resolvido, para não repetir o aviso quando ele voltar.
func (s *Servidor) notificar(ctx context.Context, achados []achado, completo bool) {
	s.vigiaMu.Lock()
	defer s.vigiaMu.Unlock()
	agora := s.agora()

	vistos := map[string]bool{}
	var novos, lembretes []achado
	for _, a := range achados {
		if vistos[a.Chave] {
			continue
		}
		vistos[a.Chave] = true
		at, existe := s.alertas[a.Chave]
		if !existe {
			at = &alertaAtivo{desde: agora}
			s.alertas[a.Chave] = at
			slog.Warn("vigia: alerta", "chave", a.Chave, "titulo", a.Titulo)
		}
		at.achado = a
		switch {
		case at.avisadoEm.IsZero():
			novos = append(novos, a)
		case !a.Evento && agora.Sub(at.avisadoEm) >= lembreteAlerta:
			lembretes = append(lembretes, a)
		}
	}
	var resolvidos []*alertaAtivo
	if completo {
		for chave, at := range s.alertas {
			if vistos[chave] {
				continue
			}
			delete(s.alertas, chave)
			if !at.Evento && !at.avisadoEm.IsZero() {
				resolvidos = append(resolvidos, at)
			}
			slog.Info("vigia: resolvido", "chave", chave, "titulo", at.Titulo)
		}
	}
	if len(novos)+len(lembretes)+len(resolvidos) == 0 || s.cfg.Vigia.Email == nil {
		return
	}

	ctxEnvio, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := s.cfg.Vigia.Email.Enviar(ctxEnvio, s.montarEmail(novos, lembretes, resolvidos)); err != nil {
		// Novos e lembretes tentam de novo no próximo ciclo; o aviso de resolvido se perde.
		slog.Error("vigia: e-mail de alerta não foi enviado", "erro", err)
		return
	}
	for _, a := range append(novos, lembretes...) {
		s.alertas[a.Chave].avisadoEm = agora
	}
}

func (s *Servidor) montarEmail(novos, lembretes []achado, resolvidos []*alertaAtivo) alerta.Mensagem {
	var b strings.Builder
	lista := func(titulo string, itens []achado) {
		if len(itens) == 0 {
			return
		}
		b.WriteString(titulo + "\n")
		for _, a := range itens {
			fmt.Fprintf(&b, "- %s\n", a.Titulo)
			if a.Detalhe != "" {
				fmt.Fprintf(&b, "  %s\n", strings.ReplaceAll(a.Detalhe, "\n", "\n  "))
			}
		}
		b.WriteString("\n")
	}
	lista("NOVOS", novos)
	lista("AINDA ATIVOS", lembretes)
	if len(resolvidos) > 0 {
		b.WriteString("RESOLVIDOS\n")
		for _, at := range resolvidos {
			fmt.Fprintf(&b, "- %s (desde %s)\n", at.Titulo, at.desde.In(time.Local).Format("02/01 15:04"))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Plataforma: %s\n", s.cfg.AppOrigin)

	ambiente := strings.TrimPrefix(strings.TrimPrefix(s.cfg.AppOrigin, "https://"), "http://")
	var assunto string
	switch {
	case len(novos) > 0:
		assunto = "ALERTA: " + novos[0].Titulo
		if len(novos) > 1 {
			assunto += fmt.Sprintf(" (+%d)", len(novos)-1)
		}
	case len(lembretes) > 0:
		assunto = "Ainda ativo: " + lembretes[0].Titulo
		if len(lembretes) > 1 {
			assunto += fmt.Sprintf(" (+%d)", len(lembretes)-1)
		}
	default:
		assunto = "Resolvido: " + resolvidos[0].Titulo
		if len(resolvidos) > 1 {
			assunto += fmt.Sprintf(" (+%d)", len(resolvidos)-1)
		}
	}
	return alerta.Mensagem{Assunto: fmt.Sprintf("[Travus %s] %s", ambiente, assunto), Corpo: b.String()}
}

func (s *Servidor) pingMonitor(ctx context.Context) {
	ctxPing, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctxPing, http.MethodGet, s.cfg.Vigia.PingURL, nil)
	if err != nil {
		slog.Warn("vigia: VIGIA_PING_URL inválida")
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Sem a URL no log: ela identifica o monitor.
		slog.Warn("vigia: ping do monitor externo falhou", "erro", strings.ReplaceAll(err.Error(), s.cfg.Vigia.PingURL, "VIGIA_PING_URL"))
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
}

func textoTempo(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%d min", int(d.Minutes()))
	}
	return fmt.Sprintf("%d h", int(d.Hours()))
}
