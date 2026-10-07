package httpapi

// Configuração do Canopus: o cálculo do próximo dry-run e a pasta do Drive são testados sem
// banco; o resto precisa de Postgres (TEST_DATABASE_URL, via make test).

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

func emBrasilia(t *testing.T, texto string) time.Time {
	t.Helper()
	q, err := time.ParseInLocation("02/01/2006 15:04", texto, fusoCanopus)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestProximoDryRun(t *testing.T) {
	casos := []struct {
		nome              string
		agora             string
		dia, hora, minuto int
		quer              string
	}{
		{"ainda neste mês", "02/10/2026 09:00", 10, 8, 0, "10/10/2026 08:00"},
		{"hoje, faltando um minuto", "10/10/2026 07:59", 10, 8, 0, "10/10/2026 08:00"},
		{"na hora exata já conta como feito", "10/10/2026 08:00", 10, 8, 0, "10/11/2026 08:00"},
		{"depois da hora vai para o mês seguinte", "10/10/2026 08:01", 10, 8, 0, "10/11/2026 08:00"},
		{"virada de ano", "15/12/2026 10:00", 10, 8, 30, "10/01/2027 08:30"},
		{"mês curto usa o último dia", "01/02/2027 10:00", 31, 8, 0, "28/02/2027 08:00"},
		{"fevereiro bissexto", "01/02/2028 10:00", 31, 8, 0, "29/02/2028 08:00"},
		{"dia 10 em domingo roda no domingo", "01/05/2026 10:00", 10, 8, 0, "10/05/2026 08:00"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := proximoDryRun(emBrasilia(t, c.agora), c.dia, c.hora, c.minuto)
			if quer := emBrasilia(t, c.quer); !got.Equal(quer) {
				t.Errorf("proximoDryRun = %s, quer %s", got.Format(time.RFC3339), quer.Format(time.RFC3339))
			}
		})
	}
}

// O caso "dia 10 em domingo" só prova algo se 10/05/2026 for mesmo domingo: decisão do usuário
// é rodar no dia marcado, sem calendário de dia útil.
func TestDiaDoCasoDeFimDeSemana(t *testing.T) {
	if d := emBrasilia(t, "10/05/2026 08:00").Weekday(); d != time.Sunday {
		t.Fatalf("10/05/2026 é %s: o caso de teste perdeu o sentido", d)
	}
}

func TestPastaDoDrive(t *testing.T) {
	const id = "1AbCdEfGhIjKlMnO_pQ-r"
	texto := func(s string) *string { return &s }
	casos := []struct {
		nome  string
		valor *string
		quer  string
		erro  bool
	}{
		{"ausente", nil, "", false},
		{"em branco apaga", texto("   "), "", false},
		{"só o id", texto(id), id, false},
		{"endereço da pasta", texto("https://drive.google.com/drive/folders/" + id + "?usp=sharing"), id, false},
		{"endereço com conta", texto("https://drive.google.com/drive/u/0/folders/" + id), id, false},
		{"endereço antigo com id", texto("https://drive.google.com/open?id=" + id), id, false},
		{"curto demais", texto("abc"), "", true},
		{"com caractere estranho", texto("pasta com espaço e acento ç"), "", true},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, err := pastaDoDrive(c.valor)
			if c.erro {
				if err == nil {
					t.Fatalf("queria erro, veio %v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if valorOuVazio(got) != c.quer {
				t.Errorf("pastaDoDrive = %q, quer %q", valorOuVazio(got), c.quer)
			}
		})
	}
}

func TestValidarConfiguracaoCanopus(t *testing.T) {
	completo := func() pedidoConfiguracaoCanopus {
		b := func(v bool) *bool { return &v }
		i := func(v int) *int { return &v }
		return pedidoConfiguracaoCanopus{
			DryRunAutomatico: b(true), DryRunDia: i(10), DryRunHora: i(8), DryRunMinuto: i(0),
			AvisoEmail: b(true), ValidadeDryRunMinutos: i(120), RetencaoScreenshotsDias: i(30),
		}
	}
	if _, err := completo().validar(); err != nil {
		t.Fatalf("configuração válida recusada: %v", err)
	}

	semHora := completo()
	semHora.DryRunHora = nil
	if _, err := semHora.validar(); err == nil {
		t.Error("campo faltando devia ser recusado (um PUT parcial desligaria o agendamento sem querer)")
	}

	casos := map[string]func(p *pedidoConfiguracaoCanopus){
		"dia zero":         func(p *pedidoConfiguracaoCanopus) { *p.DryRunDia = 0 },
		"dia 32":           func(p *pedidoConfiguracaoCanopus) { *p.DryRunDia = 32 },
		"hora 24":          func(p *pedidoConfiguracaoCanopus) { *p.DryRunHora = 24 },
		"minuto 60":        func(p *pedidoConfiguracaoCanopus) { *p.DryRunMinuto = 60 },
		"validade curta":   func(p *pedidoConfiguracaoCanopus) { *p.ValidadeDryRunMinutos = 10 },
		"validade longa":   func(p *pedidoConfiguracaoCanopus) { *p.ValidadeDryRunMinutos = 2000 },
		"retenção zero":    func(p *pedidoConfiguracaoCanopus) { *p.RetencaoScreenshotsDias = 0 },
		"retenção de anos": func(p *pedidoConfiguracaoCanopus) { *p.RetencaoScreenshotsDias = 400 },
	}
	for nome, estragar := range casos {
		t.Run(nome, func(t *testing.T) {
			p := completo()
			estragar(&p)
			if _, err := p.validar(); err == nil {
				t.Errorf("%s: devia ser recusado", nome)
			}
		})
	}
}

// Padrões da migração 00012: o agendamento nasce desligado (ligar faz o worker entrar no Newcon).
func TestConfiguracaoNasceDesligada(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "leitura@exemplo.com", db.PerfilUsuarioLeitura)
	n, rec := entrar(t, h, "leitura@exemplo.com", senhaTeste)
	esperarStatus(t, rec, http.StatusOK, "login")

	rec = n.req(http.MethodGet, "/configuracao/canopus", nil, nil)
	esperarStatus(t, rec, http.StatusOK, "configuração")
	resp := decodificar[struct {
		Configuracao configuracaoCanopusJSON `json:"configuracao"`
		PodeEditar   bool                    `json:"pode_editar"`
	}](t, rec)
	c := resp.Configuracao
	if c.DryRunAutomatico {
		t.Error("o dry-run automático tem de nascer desligado")
	}
	if c.DryRunDia != 10 || c.DryRunHora != 8 || c.DryRunMinuto != 0 {
		t.Errorf("padrão do agendamento = dia %d às %d:%02d, quer dia 10 às 08:00", c.DryRunDia, c.DryRunHora, c.DryRunMinuto)
	}
	if c.ValidadeDryRunMinutos != 120 || c.RetencaoScreenshotsDias != 30 {
		t.Errorf("prazos padrão = %d min e %d dias, quer 120 e 30", c.ValidadeDryRunMinutos, c.RetencaoScreenshotsDias)
	}
	if c.DrivePastaID != nil || c.DrivePastaOrigem != semPasta {
		t.Errorf("pasta do Drive = %v (%s), quer nenhuma", c.DrivePastaID, c.DrivePastaOrigem)
	}
	if !c.ProximoDryRun.After(time.Now()) {
		t.Errorf("próximo dry-run = %s, devia estar no futuro", c.ProximoDryRun)
	}
	if resp.PodeEditar {
		t.Error("o perfil leitura não edita a configuração")
	}
}

func TestConfiguracaoLeituraNaoEdita(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "leitura@exemplo.com", db.PerfilUsuarioLeitura)
	n, rec := entrar(t, h, "leitura@exemplo.com", senhaTeste)
	esperarStatus(t, rec, http.StatusOK, "login")

	rec = n.enviarJSON(http.MethodPut, "/configuracao/canopus", map[string]any{
		"dry_run_automatico": true, "dry_run_dia": 10, "dry_run_hora": 8, "dry_run_minuto": 0,
		"aviso_email": true, "validade_dry_run_minutos": 120, "retencao_screenshots_dias": 30,
	})
	esperarStatus(t, rec, http.StatusForbidden, "PUT com perfil leitura")
	if n := contarAuditoria(t, s, "configuracao_canopus_atualizada"); n != 0 {
		t.Errorf("auditoria registrou %d alterações numa tentativa recusada", n)
	}
}

func TestConfiguracaoOperadorEdita(t *testing.T) {
	s := novoServidorTeste(t)
	// Pasta vinda do ambiente: a configuração manda, mas em branco ela volta a valer.
	s.cfg.Google.PastaDrive = "pasta-do-ambiente-0001"
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	n, rec := entrar(t, h, "operador@exemplo.com", senhaTeste)
	esperarStatus(t, rec, http.StatusOK, "login")

	const pasta = "1AbCdEfGhIjKlMnO_pQ-r"
	rec = n.enviarJSON(http.MethodPut, "/configuracao/canopus", map[string]any{
		"dry_run_automatico": true, "dry_run_dia": 15, "dry_run_hora": 7, "dry_run_minuto": 30,
		"aviso_email": false, "drive_pasta_id": "https://drive.google.com/drive/folders/" + pasta + "?usp=sharing",
		"validade_dry_run_minutos": 180, "retencao_screenshots_dias": 45,
	})
	esperarStatus(t, rec, http.StatusOK, "PUT da configuração")
	salva := decodificar[struct {
		Configuracao configuracaoCanopusJSON `json:"configuracao"`
		PodeEditar   bool                    `json:"pode_editar"`
	}](t, rec).Configuracao
	if !salva.DryRunAutomatico || salva.DryRunDia != 15 || salva.DryRunHora != 7 || salva.DryRunMinuto != 30 {
		t.Errorf("agendamento salvo = %+v", salva)
	}
	if valorOuVazio(salva.DrivePastaID) != pasta || salva.DrivePastaOrigem != pastaDaConfiguracao {
		t.Errorf("pasta do Drive = %v (%s), quer o id %q vindo da configuração", salva.DrivePastaID, salva.DrivePastaOrigem, pasta)
	}
	if salva.AvisoEmail {
		t.Error("aviso por e-mail devia ter sido desligado")
	}
	if salva.AtualizadoPorNome == nil {
		t.Error("a configuração devia guardar quem alterou")
	}
	if n := contarAuditoria(t, s, "configuracao_canopus_atualizada"); n != 1 {
		t.Errorf("auditoria = %d, quer 1", n)
	}

	// Os valores em vigor são os que o resto da API usa.
	emVigor := s.configCanopus(context.Background())
	if emVigor.ValidadeDryRun != 3*time.Hour || emVigor.RetencaoScreenshots != 45*24*time.Hour {
		t.Errorf("prazos em vigor = %s e %s", emVigor.ValidadeDryRun, emVigor.RetencaoScreenshots)
	}
	if emVigor.DrivePasta != pasta {
		t.Errorf("pasta em vigor = %q, quer %q", emVigor.DrivePasta, pasta)
	}

	// Pasta em branco: apaga e volta a valer a do ambiente.
	rec = n.enviarJSON(http.MethodPut, "/configuracao/canopus", map[string]any{
		"dry_run_automatico": true, "dry_run_dia": 15, "dry_run_hora": 7, "dry_run_minuto": 30,
		"aviso_email": false, "drive_pasta_id": "",
		"validade_dry_run_minutos": 180, "retencao_screenshots_dias": 45,
	})
	esperarStatus(t, rec, http.StatusOK, "apagar a pasta")
	semPastaNaConfig := decodificar[struct {
		Configuracao configuracaoCanopusJSON `json:"configuracao"`
	}](t, rec).Configuracao
	if semPastaNaConfig.DrivePastaID != nil {
		t.Errorf("pasta na configuração = %v, quer nula", semPastaNaConfig.DrivePastaID)
	}
	if semPastaNaConfig.DrivePastaEmVigor != "pasta-do-ambiente-0001" || semPastaNaConfig.DrivePastaOrigem != pastaDoAmbiente {
		t.Errorf("pasta em vigor = %q (%s), quer a do ambiente", semPastaNaConfig.DrivePastaEmVigor, semPastaNaConfig.DrivePastaOrigem)
	}
	if n := contarAuditoria(t, s, "configuracao_canopus_atualizada"); n != 2 {
		t.Errorf("auditoria = %d, quer 2 (a pasta mudou de novo)", n)
	}
}

func TestConfiguracaoSemMudancaNaoAudita(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	n, rec := entrar(t, h, "admin@exemplo.com", senhaTeste)
	esperarStatus(t, rec, http.StatusOK, "login")

	iguaisAosPadroes := map[string]any{
		"dry_run_automatico": false, "dry_run_dia": 10, "dry_run_hora": 8, "dry_run_minuto": 0,
		"aviso_email": true, "drive_pasta_id": nil,
		"validade_dry_run_minutos": 120, "retencao_screenshots_dias": 30,
	}
	esperarStatus(t, n.enviarJSON(http.MethodPut, "/configuracao/canopus", iguaisAosPadroes), http.StatusOK, "PUT sem mudança")
	if n := contarAuditoria(t, s, "configuracao_canopus_atualizada"); n != 0 {
		t.Errorf("auditoria = %d: um PUT que não muda nada não é alteração", n)
	}
}

func TestConfiguracaoInvalidaRecusada(t *testing.T) {
	s := novoServidorTeste(t)
	h := s.Rotas()
	criarUsuario(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	n, rec := entrar(t, h, "admin@exemplo.com", senhaTeste)
	esperarStatus(t, rec, http.StatusOK, "login")

	rec = n.enviarJSON(http.MethodPut, "/configuracao/canopus", map[string]any{
		"dry_run_automatico": true, "dry_run_dia": 40, "dry_run_hora": 8, "dry_run_minuto": 0,
		"aviso_email": true, "validade_dry_run_minutos": 120, "retencao_screenshots_dias": 30,
	})
	esperarStatus(t, rec, http.StatusUnprocessableEntity, "dia inválido")

	// Campo desconhecido: a API não aceita (o formulário manda o que ela conhece).
	rec = n.enviarJSON(http.MethodPut, "/configuracao/canopus", map[string]any{
		"dry_run_automatico": true, "dry_run_dia": 10, "dry_run_hora": 8, "dry_run_minuto": 0,
		"aviso_email": true, "validade_dry_run_minutos": 120, "retencao_screenshots_dias": 30,
		"lance_real_habilitado": true,
	})
	esperarStatus(t, rec, http.StatusBadRequest, "campo desconhecido")

	// Nada mudou no banco.
	emVigor := s.configCanopus(context.Background())
	if emVigor.DryRunAutomatico || emVigor.DryRunDia != 10 {
		t.Errorf("configuração mudou depois de pedidos recusados: %+v", emVigor)
	}
}

// A linha da configuração se recria com os padrões se desaparecer.
func TestConfiguracaoRecriadaSeFaltar(t *testing.T) {
	s := novoServidorTeste(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, "DELETE FROM configuracao_canopus"); err != nil {
		t.Fatal(err)
	}
	c := s.configCanopus(ctx)
	if c.DryRunDia != 10 || c.DryRunAutomatico {
		t.Errorf("configuração recriada = %+v, quer os padrões", c)
	}
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM configuracao_canopus").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("linhas na configuração = %d, quer 1", n)
	}
}
