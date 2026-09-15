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
