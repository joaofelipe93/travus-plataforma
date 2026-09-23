// Package httpapi monta as rotas HTTP da API.
package httpapi

import (
	"context"
	"crypto/x509"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joaofelipe93/travus-plataforma/api/internal/checkin"
	"github.com/joaofelipe93/travus-plataforma/api/internal/cripto"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
	"github.com/joaofelipe93/travus-plataforma/api/internal/drive"
)

// Pinger é o que o /health precisa do banco (o *pgxpool.Pool satisfaz).
type Pinger interface {
	Ping(ctx context.Context) error
}

type ConfigGoogle struct {
	ClientID     string
	ClientSecret string
	PastaDrive   string
}

// Config do servidor HTTP.
type Config struct {
	// Origem pública do app (ex.: http://app.localhost): checagem de Origin e
	// redirecionamento para /login.
	AppOrigin string
	// Cookie com Secure e prefixo __Host-. Só desligue se o navegador recusar em dev.
	CookieSecure      bool
	SessaoInatividade time.Duration
	SessaoMaxima      time.Duration
	// Token de serviço exigido nas rotas internas (worker). Mínimo 32 caracteres.
	TokenWorker string
	// Screenshots têm dados pessoais: são apagados depois deste prazo.
	RetencaoScreenshots time.Duration
	// Lance real só com LANCE_REAL_HABILITADO=true. O worker tem a sua própria chave.
	LanceRealHabilitado bool
	// Prazo, contado do fim do dry-run, para aprová-lo como lance real.
	ValidadeDryRun time.Duration
	// Cifra segredos de integrações (token do Google). Sem cofre, não há envio ao Drive.
	Cofre  *cripto.Cofre
	Google ConfigGoogle
	// Alertas por e-mail e monitor externo (vigia.go).
	Vigia ConfigVigia
	// Notificador de check-in (tela WhatsApp). nil: as rotas respondem "indisponível".
	Checkin *checkin.Cliente
	// Versão publicada (version.txt) e commit, mostrados no /health.
	Versao string
	Commit string
}

type Servidor struct {
	cfg   Config
	pool  *pgxpool.Pool
	q     *db.Queries
	ping  Pinger
	hub   *Hub
	agora func() time.Time

	// Fechado no desligamento: encerra os streams SSE abertos.
	encerrando     chan struct{}
	encerrarUmaVez sync.Once

	// Fila do Drive: acordada quando um lance ganha PDF. enviadorDrive substitui o
	// cliente do Google nos testes.
	acordarDrive  chan struct{}
	enviadorDrive drive.Enviador
	driveMu       sync.Mutex
	driveCliente  drive.Enviador
	driveVersao   time.Time
	avisouDrive   string

	// Vigia: início da API, último contato do worker (UnixNano) e alertas já avisados.
	inicio              time.Time
	ultimoContatoWorker atomic.Int64
	vigiaMu             sync.Mutex
	alertas             map[string]*alertaAtivo
	vigiaRaizes         *x509.CertPool
	// Notificador de check-in: desde quando está sem resposta ou com o WhatsApp fora (só a
	// goroutine do vigia mexe).
	checkinSemRespostaDesde time.Time
	whatsappForaDesde       time.Time
}

func NovoServidor(cfg Config, pool *pgxpool.Pool) *Servidor {
	if cfg.ValidadeDryRun == 0 {
		cfg.ValidadeDryRun = 2 * time.Hour
	}
	if cfg.Vigia.Intervalo == 0 {
		cfg.Vigia.Intervalo = 5 * time.Minute
	}
	if cfg.Vigia.LimiteDisco == 0 {
		cfg.Vigia.LimiteDisco = 0.8
	}
	return &Servidor{
		cfg: cfg, pool: pool, q: db.New(pool), ping: pool, hub: NovoHub(pool), agora: time.Now,
		encerrando: make(chan struct{}), acordarDrive: make(chan struct{}, 1),
		inicio: time.Now(), alertas: map[string]*alertaAtivo{},
	}
}

// Encerrar fecha os streams SSE (use com http.Server.RegisterOnShutdown).
func (s *Servidor) Encerrar() {
	s.encerrarUmaVez.Do(func() { close(s.encerrando) })
}

// RodarTarefasDeFundo: escuta de eventos para o SSE, limpeza de screenshots vencidos,
// envio de comprovantes ao Google Drive e vigia (alertas).
func (s *Servidor) RodarTarefasDeFundo(ctx context.Context) {
	go s.hub.Rodar(ctx)
	go s.rodarFilaDrive(ctx)
	go s.rodarVigia(ctx)
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			if n, err := s.q.ApagarArquivosExpirados(ctx); err != nil {
				if ctx.Err() == nil {
					slog.Warn("falha ao apagar screenshots vencidos", "erro", err)
				}
			} else if n > 0 {
				slog.Info("screenshots vencidos apagados", "quantidade", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// Rotas da API. No gateway, o navegador chega por app.<domínio>/api/... (o Traefik
// remove o /api) e ferramentas por api.<domínio>/... Tudo menos /health, /auth/login e
// /auth/verificar passa antes pelo ForwardAuth; mesmo assim a API valida a sessão de novo.
// As rotas do worker ficam em RotasInternas, em outra porta.
func (s *Servidor) Rotas() http.Handler {
	editores := []db.PerfilUsuario{db.PerfilUsuarioAdmin, db.PerfilUsuarioOperador}
	admins := []db.PerfilUsuario{db.PerfilUsuarioAdmin}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.health)

	mux.HandleFunc("POST /auth/login", s.login)
	mux.HandleFunc("GET /auth/verificar", s.verificar)
	mux.Handle("GET /auth/sessao", s.autenticado(s.sessao))
	mux.Handle("POST /auth/logout", s.autenticado(s.logout))

	// Meu perfil: dados de contato e credenciais pessoais (perfil.go). Todo perfil mexe nos
	// próprios dados; o panorama de quem já cadastrou o quê é só do admin.
	mux.Handle("GET /perfil", s.autenticado(s.meuPerfil))
	mux.Handle("PATCH /perfil", s.autenticado(s.atualizarMeuPerfil))
	mux.Handle("PUT /perfil/credenciais/{credencial}", s.autenticado(s.gravarMinhaCredencial))
	mux.Handle("DELETE /perfil/credenciais/{credencial}", s.autenticado(s.apagarMinhaCredencial))
	mux.Handle("GET /credenciais", s.autenticado(exigirPerfil(s.panoramaCredenciais, admins...)))

	// CRM do Canopus (crm.go): o cadastro é digitado, não mais importado de planilha.
	mux.Handle("GET /clientes", s.autenticado(s.listarClientes))
	mux.Handle("POST /clientes", s.autenticado(exigirPerfil(s.criarCliente, editores...)))
	mux.Handle("GET /clientes/{id}", s.autenticado(s.buscarCliente))
	mux.Handle("PATCH /clientes/{id}", s.autenticado(exigirPerfil(s.atualizarCliente, editores...)))
	mux.Handle("DELETE /clientes/{id}", s.autenticado(exigirPerfil(s.excluirCliente, editores...)))
	mux.Handle("GET /cotas", s.autenticado(s.listarCotas))
	mux.Handle("POST /cotas", s.autenticado(exigirPerfil(s.criarCota, editores...)))
	mux.Handle("GET /cotas/grupos", s.autenticado(s.listarGrupos))
	mux.Handle("PUT /cotas/{id}", s.autenticado(exigirPerfil(s.atualizarCota, editores...)))
	mux.Handle("PATCH /cotas/{id}", s.autenticado(exigirPerfil(s.definirCotaAtiva, editores...)))
	mux.Handle("DELETE /cotas/{id}", s.autenticado(exigirPerfil(s.excluirCota, editores...)))

	// Importações antigas: só consulta. Importar planilha saiu da plataforma com o CRM;
	// o histórico fica porque as cotas importadas apontam para ele.
	mux.Handle("GET /importacoes", s.autenticado(s.listarImportacoes))
	mux.Handle("GET /importacoes/{id}", s.autenticado(s.buscarImportacao))

	mux.Handle("GET /execucoes", s.autenticado(s.listarExecucoes))
	mux.Handle("POST /execucoes", s.autenticado(exigirPerfil(s.criarExecucao, editores...)))
	mux.Handle("GET /execucoes/{id}", s.autenticado(s.buscarExecucao))
	mux.Handle("POST /execucoes/{id}/cancelar", s.autenticado(exigirPerfil(s.cancelarExecucao, editores...)))
	mux.Handle("GET /execucoes/{id}/eventos", s.autenticado(s.eventosExecucao))
	mux.Handle("GET /arquivos/{id}", s.autenticado(s.baixarArquivo))

	// Etapa 3: lance real (só admin aprova) e reimpressão de comprovante.
	mux.Handle("GET /execucoes/{id}/revisao", s.autenticado(s.revisaoDryRun))
	mux.Handle("POST /execucoes/reais", s.autenticado(exigirPerfil(s.aprovarExecucaoReal, admins...)))
	mux.Handle("POST /execucoes/reimpressoes", s.autenticado(exigirPerfil(s.criarReimpressao, editores...)))
	mux.Handle("POST /lances/{id}/reenviar-drive", s.autenticado(exigirPerfil(s.reenviarAoDrive, editores...)))
	mux.Handle("GET /integracoes/google-drive", s.autenticado(exigirPerfil(s.situacaoGoogleDrive, admins...)))

	// Reservas recebidas pelo notificador (reservas.go): dados de hóspedes, sem o perfil leitura.
	mux.Handle("GET /reservas", s.autenticado(exigirPerfil(s.listarReservas, editores...)))

	// WhatsApp do notificador de check-in (whatsapp.go): o QR dá acesso à conta, só admin.
	mux.Handle("GET /integracoes/whatsapp", s.autenticado(exigirPerfil(s.situacaoWhatsapp, admins...)))
	mux.Handle("GET /integracoes/whatsapp/grupos", s.autenticado(exigirPerfil(s.gruposWhatsapp, admins...)))
	mux.Handle("PUT /integracoes/whatsapp/grupo", s.autenticado(exigirPerfil(s.definirGrupoWhatsapp, admins...)))
	mux.Handle("POST /integracoes/whatsapp/teste", s.autenticado(exigirPerfil(s.testeWhatsapp, admins...)))
	mux.Handle("POST /integracoes/whatsapp/desconectar", s.autenticado(exigirPerfil(s.desconectarWhatsapp, admins...)))

	return recuperar(mux)
}

type healthResponse struct {
	Status     string `json:"status"`
	Servico    string `json:"servico"`
	Banco      string `json:"banco"`
	ViaGateway bool   `json:"via_gateway"`
	Host       string `json:"host,omitempty"`
	Horario    string `json:"horario"`
	Versao     string `json:"versao"`
	Commit     string `json:"commit"`
}

// health responde se a API está de pé e se alcança o Postgres. "via_gateway" indica se a
// requisição passou pelo Traefik, que sempre preenche X-Forwarded-Host.
func (s *Servidor) health(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:     "ok",
		Servico:    "api",
		Banco:      "ok",
		ViaGateway: r.Header.Get("X-Forwarded-Host") != "",
		Host:       r.Header.Get("X-Forwarded-Host"),
		Horario:    s.agora().UTC().Format(time.RFC3339),
		Versao:     valorOuPadrao(s.cfg.Versao, "dev"),
		Commit:     valorOuPadrao(s.cfg.Commit, "desconhecido"),
	}
	code := http.StatusOK

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.ping.Ping(ctx); err != nil {
		// O detalhe do erro fica só no log: a rota é pública.
		slog.Warn("health: banco indisponível", "erro", err)
		resp.Status, resp.Banco = "degradado", "indisponivel"
		code = http.StatusServiceUnavailable
	}

	responderJSON(w, code, resp)
}

func recuperar(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.Error("pânico no handler", "metodo", r.Method, "caminho", r.URL.Path, "valor", v, "pilha", string(debug.Stack()))
				responderErro(w, http.StatusInternalServerError, mensagemErroInterno)
			}
		}()
		h.ServeHTTP(w, r)
	})
}
