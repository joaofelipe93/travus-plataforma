package httpapi

// Testes de integração: precisam de Postgres (TEST_DATABASE_URL). Rode com `make test`.
// O notificador é um servidor falso (httptest): nada fala com o WhatsApp.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/checkin"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const tokenCheckinTeste = "token-de-administracao-do-checkin-de-teste"

type notificadorFalso struct {
	mu          sync.Mutex
	conectado   bool
	grupo       string
	desconexoes int
	testes      int
	tokens      []string
}

func (f *notificadorFalso) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens = append(f.tokens, r.Header.Get("Authorization"))
	if r.Header.Get("Authorization") != "Bearer "+tokenCheckinTeste {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	responder := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	grupoAtual := func() any {
		if f.grupo == "" {
			return nil
		}
		return map[string]any{"jid": f.grupo, "nome": "Reservas Chalés", "origem": "tela"}
	}
	desconectado := func() bool {
		if !f.conectado {
			responder(http.StatusServiceUnavailable, map[string]string{"error": "not_connected", "status": "qr"})
		}
		return !f.conectado
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /whatsapp/status":
		if f.conectado {
			responder(200, map[string]any{"status": "open", "connected": true, "account": "5511900000000:3@s.whatsapp.net", "qr": nil, "group": grupoAtual(), "outbox": map[string]int{"enviada": 7}})
		} else {
			responder(200, map[string]any{"status": "qr", "connected": false, "account": nil, "qr": "2@qr-de-teste", "group": grupoAtual(), "outbox": map[string]int{}})
		}
	case "GET /whatsapp/groups":
		if desconectado() {
			return
		}
		responder(200, map[string]any{"groups": []map[string]any{{"jid": "1234-5678@g.us", "subject": "Reservas Chalés", "participants": 4}}})
	case "PUT /whatsapp/grupo":
		if desconectado() {
			return
		}
		var p struct{ JID string }
		_ = json.NewDecoder(r.Body).Decode(&p)
		if p.JID != "1234-5678@g.us" {
			responder(http.StatusUnprocessableEntity, map[string]string{"error": "group_not_found"})
			return
		}
		f.grupo = p.JID
		responder(200, map[string]any{"group": grupoAtual()})
	case "POST /whatsapp/test":
		if f.grupo == "" {
			responder(http.StatusBadRequest, map[string]string{"error": "group_not_configured"})
			return
		}
		if desconectado() {
			return
		}
		f.testes++
		responder(200, map[string]any{"status": "sent", "group": grupoAtual()})
	case "POST /whatsapp/desconectar":
		f.desconexoes++
		f.conectado = false
		responder(http.StatusAccepted, map[string]string{"status": "disconnected"})
	default:
		http.NotFound(w, r)
	}
}

func servidorComNotificador(t *testing.T, cliente *checkin.Cliente) (*Servidor, http.Handler) {
	t.Helper()
	s := novoServidorTeste(t)
	s.cfg.Checkin = cliente
	criarUsuario(t, s, "admin@teste.com", db.PerfilUsuarioAdmin)
	criarUsuario(t, s, "operador@teste.com", db.PerfilUsuarioOperador)
	return s, s.Rotas()
}

func TestWhatsappSoAdmin(t *testing.T) {
	falso := &notificadorFalso{conectado: true}
	srv := httptest.NewServer(falso)
	defer srv.Close()
	_, h := servidorComNotificador(t, checkin.NovoCliente(srv.URL, tokenCheckinTeste))

	operador, _ := entrar(t, h, "operador@teste.com", senhaTeste)
	esperarStatus(t, operador.req(http.MethodGet, "/integracoes/whatsapp", nil, nil), http.StatusForbidden, "status (operador)")
	esperarStatus(t, operador.req(http.MethodGet, "/integracoes/whatsapp/grupos", nil, nil), http.StatusForbidden, "grupos (operador)")
	esperarStatus(t, operador.enviarJSON(http.MethodPut, "/integracoes/whatsapp/grupo", map[string]string{"jid": "1234-5678@g.us"}), http.StatusForbidden, "grupo (operador)")
	esperarStatus(t, operador.enviarJSON(http.MethodPost, "/integracoes/whatsapp/teste", map[string]any{}), http.StatusForbidden, "teste (operador)")
	esperarStatus(t, operador.enviarJSON(http.MethodPost, "/integracoes/whatsapp/desconectar", map[string]bool{"confirmar": true}), http.StatusForbidden, "desconectar (operador)")

	anonimo := &navegador{t: t, h: h}
	esperarStatus(t, anonimo.req(http.MethodGet, "/integracoes/whatsapp", nil, nil), http.StatusUnauthorized, "status sem sessão")

	admin, _ := entrar(t, h, "admin@teste.com", senhaTeste)
	semCSRF := admin.req(http.MethodPost, "/integracoes/whatsapp/desconectar", strings.NewReader(`{"confirmar":true}`),
		map[string]string{"Origin": origemTeste, "Content-Type": "application/json"})
	esperarStatus(t, semCSRF, http.StatusForbidden, "desconectar sem CSRF")

	if len(falso.tokens) != 0 || falso.desconexoes != 0 {
		t.Fatalf("pedidos recusados não podiam chegar ao notificador: %d chamadas", len(falso.tokens))
	}
}

func TestWhatsappConectarEscolherGrupoTestarEDesconectar(t *testing.T) {
	falso := &notificadorFalso{conectado: false}
	srv := httptest.NewServer(falso)
	defer srv.Close()
	s, h := servidorComNotificador(t, checkin.NovoCliente(srv.URL, tokenCheckinTeste))
	admin, _ := entrar(t, h, "admin@teste.com", senhaTeste)

	// Aguardando o QR.
	st := decodificar[respostaWhatsapp](t, admin.req(http.MethodGet, "/integracoes/whatsapp", nil, nil))
	if !st.Disponivel || st.Estado != "aguardando_qr" || st.QR == nil || *st.QR != "2@qr-de-teste" || st.Numero != nil {
		t.Fatalf("aguardando QR: %+v", st)
	}
	esperarStatus(t, admin.req(http.MethodGet, "/integracoes/whatsapp/grupos", nil, nil), http.StatusConflict, "grupos antes de conectar")
	esperarStatus(t, admin.enviarJSON(http.MethodPost, "/integracoes/whatsapp/teste", map[string]any{}), http.StatusUnprocessableEntity, "teste sem grupo")

	// "Escaneou o QR".
	falso.mu.Lock()
	falso.conectado = true
	falso.mu.Unlock()
	st = decodificar[respostaWhatsapp](t, admin.req(http.MethodGet, "/integracoes/whatsapp", nil, nil))
	if st.Estado != "conectado" || st.QR != nil || st.Numero == nil || *st.Numero != "5511900000000" || st.Fila["enviada"] != 7 {
		t.Fatalf("conectado: %+v", st)
	}

	rec := admin.req(http.MethodGet, "/integracoes/whatsapp/grupos", nil, nil)
	esperarStatus(t, rec, http.StatusOK, "grupos")
	grupos := decodificar[struct{ Grupos []grupoWhatsapp }](t, rec).Grupos
	if len(grupos) != 1 || grupos[0].Nome != "Reservas Chalés" || grupos[0].Participantes != 4 {
		t.Fatalf("grupos: %+v", grupos)
	}

	esperarStatus(t, admin.enviarJSON(http.MethodPut, "/integracoes/whatsapp/grupo", map[string]string{"jid": "999-999@g.us"}), http.StatusUnprocessableEntity, "grupo de que o número não participa")
	esperarStatus(t, admin.enviarJSON(http.MethodPut, "/integracoes/whatsapp/grupo", map[string]string{}), http.StatusBadRequest, "grupo sem jid")
	esperarStatus(t, admin.enviarJSON(http.MethodPut, "/integracoes/whatsapp/grupo", map[string]string{"jid": "1234-5678@g.us"}), http.StatusOK, "escolher grupo")
	if n := contarAuditoria(t, s, "whatsapp_grupo_definido"); n != 1 {
		t.Fatalf("auditoria do grupo: %d", n)
	}

	esperarStatus(t, admin.enviarJSON(http.MethodPost, "/integracoes/whatsapp/teste", map[string]any{}), http.StatusOK, "mensagem de teste")
	if falso.testes != 1 || contarAuditoria(t, s, "whatsapp_teste_enviado") != 1 {
		t.Fatalf("teste: %d envios", falso.testes)
	}

	// Desconectar exige confirmação explícita.
	esperarStatus(t, admin.enviarJSON(http.MethodPost, "/integracoes/whatsapp/desconectar", map[string]any{}), http.StatusUnprocessableEntity, "desconectar sem confirmar")
	esperarStatus(t, admin.enviarJSON(http.MethodPost, "/integracoes/whatsapp/desconectar", map[string]bool{"confirmar": false}), http.StatusUnprocessableEntity, "desconectar com confirmar false")
	if falso.desconexoes != 0 {
		t.Fatal("desconectou sem confirmação")
	}
	esperarStatus(t, admin.enviarJSON(http.MethodPost, "/integracoes/whatsapp/desconectar", map[string]bool{"confirmar": true}), http.StatusNoContent, "desconectar")
	if falso.desconexoes != 1 || contarAuditoria(t, s, "whatsapp_desconectado") != 1 {
		t.Fatalf("desconexões: %d", falso.desconexoes)
	}

	for _, tk := range falso.tokens {
		if tk != "Bearer "+tokenCheckinTeste {
			t.Fatalf("pedido ao notificador sem o token de administração: %q", tk)
		}
	}
}

func TestWhatsappNotificadorIndisponivel(t *testing.T) {
	// Servidor que não existe mais (porta fechada) e cliente sem configuração.
	fechado := httptest.NewServer(http.NotFoundHandler())
	url := fechado.URL
	fechado.Close()
	outroToken := httptest.NewServer(&notificadorFalso{conectado: true})
	defer outroToken.Close()

	for nome, cliente := range map[string]*checkin.Cliente{
		"fora do ar":       checkin.NovoCliente(url, tokenCheckinTeste),
		"sem configuração": checkin.NovoCliente("", ""),
		"token errado":     checkin.NovoCliente(outroToken.URL, "outro-token"),
	} {
		t.Run(nome, func(t *testing.T) {
			_, h := servidorComNotificador(t, cliente)
			admin, _ := entrar(t, h, "admin@teste.com", senhaTeste)

			rec := admin.req(http.MethodGet, "/integracoes/whatsapp", nil, nil)
			esperarStatus(t, rec, http.StatusOK, "status com o notificador fora")
			st := decodificar[respostaWhatsapp](t, rec)
			if st.Disponivel || st.Estado != "indisponivel" || st.Motivo == "" || st.QR != nil {
				t.Fatalf("status indisponível: %+v", st)
			}
			if strings.Contains(rec.Body.String(), tokenCheckinTeste) {
				t.Fatal("resposta vazou o token do notificador")
			}
			esperarStatus(t, admin.req(http.MethodGet, "/integracoes/whatsapp/grupos", nil, nil), http.StatusServiceUnavailable, "grupos")
			esperarStatus(t, admin.enviarJSON(http.MethodPost, "/integracoes/whatsapp/desconectar", map[string]bool{"confirmar": true}), http.StatusServiceUnavailable, "desconectar")
		})
	}
}

func TestNumeroDaConta(t *testing.T) {
	for entrada, quer := range map[string]string{
		"5511900000000:3@s.whatsapp.net": "5511900000000",
		"5511900000000@s.whatsapp.net":   "5511900000000",
	} {
		if got := numeroDaConta(&entrada); got == nil || *got != quer {
			t.Errorf("numeroDaConta(%q) = %v, quer %q", entrada, got, quer)
		}
	}
	vazio := ""
	if numeroDaConta(nil) != nil || numeroDaConta(&vazio) != nil {
		t.Error("conta vazia deveria dar nil")
	}
}
