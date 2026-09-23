package httpapi

// Testes do vigia (alertas). Precisam de Postgres (make test).

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joaofelipe93/travus-plataforma/api/internal/alerta"
	"github.com/joaofelipe93/travus-plataforma/api/internal/checkin"
)

type emailFalso struct {
	mensagens []alerta.Mensagem
	falhar    bool
}

func (f *emailFalso) Enviar(_ context.Context, m alerta.Mensagem) error {
	if f.falhar {
		return errors.New("SMTP fora do ar")
	}
	f.mensagens = append(f.mensagens, m)
	return nil
}

func TestVigia(t *testing.T) {
	s, n, cotas, _ := cenarioExecucoes(t)
	ctx := context.Background()
	email := &emailFalso{}
	var pings atomic.Int32
	monitor := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { pings.Add(1) }))
	defer monitor.Close()
	https := httptest.NewTLSServer(http.NotFoundHandler())
	defer https.Close()

	backups := t.TempDir()
	marcador := filepath.Join(backups, "ultimo-ok")
	if err := os.WriteFile(marcador, []byte("2026-09-14T03:00:00-03:00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	velho := time.Now().Add(-30 * time.Hour)
	if err := os.Chtimes(marcador, velho, velho); err != nil {
		t.Fatal(err)
	}
	s.cfg.Vigia = ConfigVigia{
		Email: email, PingURL: monitor.URL, BackupDir: backups,
		CertificadoHost: strings.TrimPrefix(https.URL, "https://"), Intervalo: time.Minute, LimiteDisco: 2,
	}
	s.inicio = time.Now().Add(-time.Hour)

	// Execução parada na fila há 2 h, com uma cota que clicou em Confirmar sem resultado.
	parada := n.criarDryRun(t, idsDe(cotas[:1]))
	s.exec(t, "UPDATE execucoes SET criada_em = now() - interval '2 hours' WHERE id = $1", parada)
	ec := n.detalhe(t, parada).Cotas[0]
	s.exec(t, "UPDATE execucao_cotas SET status = 'em_andamento', iniciada_em = now() WHERE id = $1", ec.ID)
	s.exec(t, "UPDATE execucao_cotas SET status = 'confirmacao_iniciada' WHERE id = $1", ec.ID)
	s.exec(t, "UPDATE execucao_cotas SET status = 'erro_apos_confirmar', erro = 'o lance pode ter sido registrado' WHERE id = $1", ec.ID)

	s.cicloVigia(ctx)
	if len(email.mensagens) != 1 {
		t.Fatalf("e-mails no primeiro ciclo = %d", len(email.mensagens))
	}
	primeiro := email.mensagens[0]
	for _, esperado := range []string{
		"Worker Canopus sem contato", "na fila há mais de 30 min", "erro depois de clicar em Confirmar",
		"Backup do Postgres atrasado", "Não consegui conferir o certificado HTTPS",
	} {
		if !strings.Contains(primeiro.Corpo, esperado) {
			t.Errorf("e-mail sem %q:\n%s", esperado, primeiro.Corpo)
		}
	}
	if !strings.HasPrefix(primeiro.Assunto, "[Travus app.teste] ALERTA: ") || !strings.Contains(primeiro.Assunto, "(+4)") {
		t.Errorf("assunto: %q", primeiro.Assunto)
	}
	if strings.Contains(primeiro.Corpo, cotas[0].ClienteNome) {
		t.Error("nome do cliente no e-mail de alerta")
	}
	if pings.Load() != 1 {
		t.Errorf("pings no monitor externo = %d", pings.Load())
	}

	// Nada mudou: nenhum e-mail novo.
	s.cicloVigia(ctx)
	if len(email.mensagens) != 1 {
		t.Fatalf("alerta repetido: %d e-mails", len(email.mensagens))
	}

	// Worker volta, a fila anda e o certificado passa a ser confiável: resolvidos. A cota em
	// erro_apos_confirmar é evento: não aparece como resolvida.
	s.ultimoContatoWorker.Store(time.Now().UnixNano())
	s.exec(t, "UPDATE execucoes SET status = 'cancelada', finalizada_em = now() WHERE id = $1", parada)
	s.vigiaRaizes = https.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	s.cicloVigia(ctx)
	if len(email.mensagens) != 2 {
		t.Fatalf("e-mails depois de resolver = %d", len(email.mensagens))
	}
	resolvido := email.mensagens[1]
	if !strings.Contains(resolvido.Assunto, "Resolvido: ") || !strings.Contains(resolvido.Corpo, "RESOLVIDOS") ||
		!strings.Contains(resolvido.Corpo, "Worker Canopus") || !strings.Contains(resolvido.Corpo, "certificado") {
		t.Errorf("e-mail de resolvido: %q\n%s", resolvido.Assunto, resolvido.Corpo)
	}
	if strings.Contains(resolvido.Corpo, "Confirmar") || strings.Contains(resolvido.Corpo, "Backup") {
		t.Errorf("resolveu o que continua ativo:\n%s", resolvido.Corpo)
	}

	// SMTP fora do ar: o alerta novo tenta de novo no ciclo seguinte.
	if err := os.WriteFile(filepath.Join(backups, "ultimo-erro"), []byte("2026-09-15T03:00:00-03:00 pg_dump falhou\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	email.falhar = true
	s.cicloVigia(ctx)
	email.falhar = false
	s.cicloVigia(ctx)
	if len(email.mensagens) != 3 || !strings.Contains(email.mensagens[2].Corpo, "O último backup do Postgres falhou") {
		t.Fatalf("alerta depois da falha no SMTP: %d e-mails", len(email.mensagens))
	}

	// Disco acima do limite.
	s.cfg.Vigia.LimiteDisco = 0
	s.cicloVigia(ctx)
	if len(email.mensagens) != 4 || !strings.Contains(email.mensagens[3].Corpo, "Disco da VM") {
		t.Fatalf("alerta de disco: %d e-mails", len(email.mensagens))
	}
}

func TestVigiaContatoDoWorker(t *testing.T) {
	s := novoServidorTeste(t)
	s.inicio = time.Now().Add(-time.Hour)
	interno := s.RotasInternas()
	w := &workerTeste{t: t, h: interno, nome: "worker-vigia"}
	w.proxima()
	achados, ok := s.verificarSaude(context.Background())
	if !ok {
		t.Fatal("banco indisponível")
	}
	for _, a := range achados {
		if a.Chave == "worker" {
			t.Fatalf("worker acabou de falar com a API e o vigia reclamou: %+v", a)
		}
	}

	// Token errado não conta como contato.
	s.ultimoContatoWorker.Store(0)
	r := httptest.NewRequest(http.MethodPost, "/internal/tarefas/proxima", strings.NewReader(`{"tipos":["dry_run"]}`))
	r.Header.Set("Authorization", "Bearer token-errado-token-errado-token-errado")
	interno.ServeHTTP(httptest.NewRecorder(), r)
	if s.ultimoContatoWorker.Load() != 0 {
		t.Error("requisição sem token válido contou como contato do worker")
	}
}

func TestVigiaNotificadorCheckin(t *testing.T) {
	s := novoServidorTeste(t)
	ctx := context.Background()
	s.exec(t, "TRUNCATE checkin.mensagens, checkin.resumos, checkin.reservas, checkin.eventos, checkin.configuracao RESTART IDENTITY")
	falso := &notificadorFalso{conectado: false}
	srv := httptest.NewServer(falso)
	defer srv.Close()
	s.cfg.Checkin = checkin.NovoCliente(srv.URL, tokenCheckinTeste)
	s.cfg.Vigia.Checkin = true
	s.inicio = time.Now().Add(-time.Hour)
	s.ultimoContatoWorker.Store(time.Now().UnixNano())

	relogio := time.Now()
	s.agora = func() time.Time { return relogio }
	checkinAchados := func() map[string]achado {
		t.Helper()
		achados, ok := s.verificarSaude(ctx)
		if !ok {
			t.Fatal("banco indisponível")
		}
		m := map[string]achado{}
		for _, a := range achados {
			if strings.HasPrefix(a.Chave, "checkin-") {
				m[a.Chave] = a
			}
		}
		return m
	}
	avancar := func(d time.Duration) { relogio = relogio.Add(d) }

	// WhatsApp esperando o QR: tolera 10 min (reinício, pareamento em andamento).
	if a := checkinAchados(); len(a) != 0 {
		t.Fatalf("alerta antes da tolerância: %+v", a)
	}
	avancar(11 * time.Minute)
	a := checkinAchados()
	if w, ok := a["checkin-whatsapp"]; !ok || !strings.Contains(w.Titulo, "QR") || !strings.Contains(w.Detalhe, "app.teste/reservas/whatsapp") {
		t.Fatalf("esperado alerta do QR: %+v", a)
	}

	// Conectou, sem grupo escolhido.
	falso.mu.Lock()
	falso.conectado = true
	falso.mu.Unlock()
	a = checkinAchados()
	if _, ok := a["checkin-whatsapp"]; ok {
		t.Fatal("alerta de desconexão continuou depois de conectar")
	}
	if _, ok := a["checkin-sem-grupo"]; !ok {
		t.Fatalf("esperado alerta de grupo não escolhido: %+v", a)
	}

	// Grupo escolhido; mensagens que desistiram e paradas na fila com o WhatsApp conectado.
	falso.mu.Lock()
	falso.grupo = "1234-5678@g.us"
	falso.mu.Unlock()
	s.exec(t, `INSERT INTO checkin.eventos (chave_dedup, origem, payload) VALUES ('teste:1', 'teste', '{"guest_name":"Hóspede Fictício"}')`)
	s.exec(t, `INSERT INTO checkin.mensagens (evento_id, destino_jid, texto, status, tentativas) VALUES (1, '1234-5678@g.us', 'Hóspede Fictício', 'falhou', 6)`)
	s.exec(t, `INSERT INTO checkin.mensagens (evento_id, destino_jid, texto, criada_em) VALUES (1, '1234-5678@g.us', 'Hóspede Fictício', now() - interval '1 hour')`)
	a = checkinAchados()
	if _, ok := a["checkin-sem-grupo"]; ok {
		t.Fatal("alerta de grupo continuou depois de escolher")
	}
	if f, ok := a["checkin-falhas"]; !ok || !strings.HasPrefix(f.Titulo, "1 mensagem") {
		t.Fatalf("esperado alerta de mensagens que falharam: %+v", a)
	}
	if _, ok := a["checkin-fila"]; !ok {
		t.Fatalf("esperado alerta de fila parada: %+v", a)
	}
	for _, x := range a {
		if strings.Contains(x.Titulo+x.Detalhe, "Fictício") || strings.Contains(x.Titulo+x.Detalhe, "Reservas Chalés") {
			t.Fatalf("alerta com nome de hóspede ou de grupo: %+v", x)
		}
	}

	// Desconectado com fila parada: só o alerta de desconexão (depois da tolerância), não o da fila.
	falso.mu.Lock()
	falso.conectado = false
	falso.mu.Unlock()
	avancar(time.Minute)
	if _, ok := checkinAchados()["checkin-fila"]; ok {
		t.Fatal("fila parada com o WhatsApp desconectado não é alerta próprio")
	}

	// Notificador fora do ar: tolera 10 min.
	srv.Close()
	avancar(time.Minute)
	if _, ok := checkinAchados()["checkin-servico"]; ok {
		t.Fatal("alerta de serviço antes da tolerância")
	}
	avancar(11 * time.Minute)
	a = checkinAchados()
	if _, ok := a["checkin-servico"]; !ok {
		t.Fatalf("esperado alerta do notificador sem resposta: %+v", a)
	}
	if _, ok := a["checkin-whatsapp"]; ok {
		t.Fatal("sem resposta do serviço, o estado do WhatsApp é desconhecido: não alerta desconexão")
	}
	if strings.Contains(a["checkin-servico"].Detalhe, tokenCheckinTeste) {
		t.Fatal("token no alerta")
	}

	// Desligado (ambiente local): nenhuma checagem.
	s.cfg.Vigia.Checkin = false
	if a := checkinAchados(); len(a) != 0 {
		t.Fatalf("checagem do notificador com VIGIA_CHECKIN desligado: %+v", a)
	}
}
