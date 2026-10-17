package httpapi

// Tela do WhatsApp dos boletos. O serviço de envio é um dublê HTTP: **nenhum teste fala com o
// WhatsApp** nem pareia número.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/joaofelipe93/travus-plataforma/api/internal/boletoswa"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

const tokenBoletosWaTeste = "token-do-servico-de-boletos-com-mais-de-32-caracteres"

type servicoBoletosFalso struct {
	mu          sync.Mutex
	conectado   bool
	testes      []string
	desconexoes int
}

func (f *servicoBoletosFalso) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+tokenBoletosWaTeste {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	responder := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /whatsapp/status":
		if f.conectado {
			responder(200, map[string]any{
				"status": "open", "connected": true, "account": "5511900000000:3@s.whatsapp.net", "qr": nil,
				"envio_habilitado_servico": true, "envio_habilitado_configuracao": true,
				"pausa_envio_segundos": 20, "fila": map[string]int{"pendente": 2},
			})
			return
		}
		responder(200, map[string]any{
			"status": "qr", "connected": false, "account": nil, "qr": "2@qr-de-teste",
			"envio_habilitado_servico": true, "envio_habilitado_configuracao": false,
			"pausa_envio_segundos": 20, "fila": map[string]int{},
		})
	case "POST /whatsapp/teste":
		if !f.conectado {
			responder(http.StatusServiceUnavailable, map[string]string{"error": "not_connected"})
			return
		}
		var corpo struct{ Telefone string }
		_ = json.NewDecoder(r.Body).Decode(&corpo)
		f.testes = append(f.testes, corpo.Telefone)
		responder(202, map[string]any{"enfileirada": 42})
	case "POST /whatsapp/desconectar":
		f.desconexoes++
		responder(200, map[string]any{"status": "disconnected"})
	default:
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
	}
}

func cenarioWhatsappBoletos(t *testing.T, conectado bool) (*Servidor, *navegador, *servicoBoletosFalso, http.Handler) {
	t.Helper()
	falso := &servicoBoletosFalso{conectado: conectado}
	servidorFalso := httptest.NewServer(falso)
	t.Cleanup(servidorFalso.Close)

	s := novoServidorTeste(t)
	s.cfg.BoletosWhatsapp = boletoswa.NovoCliente(servidorFalso.URL, tokenBoletosWaTeste)
	h := s.Rotas()
	criarUsuario(t, s, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	admin, _ := entrar(t, h, "admin@exemplo.com", senhaTeste)
	return s, admin, falso, h
}

func TestWhatsappBoletosSoAdmin(t *testing.T) {
	s, _, _, h := cenarioWhatsappBoletos(t, true)
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	operador, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)

	// O QR dá acesso à conta do WhatsApp: operador não vê.
	esperarStatus(t, operador.req(http.MethodGet, "/integracoes/whatsapp-boletos", nil, nil), http.StatusForbidden, "operador vê o QR")
	esperarStatus(t, operador.enviarJSON(http.MethodPost, "/integracoes/whatsapp-boletos/teste",
		map[string]string{"telefone": "11900000000"}), http.StatusForbidden, "operador manda teste")
	esperarStatus(t, operador.enviarJSON(http.MethodPost, "/integracoes/whatsapp-boletos/desconectar",
		map[string]bool{"confirmar": true}), http.StatusForbidden, "operador desconecta")

	anonimo := &navegador{t: t, h: h}
	esperarStatus(t, anonimo.req(http.MethodGet, "/integracoes/whatsapp-boletos", nil, nil), http.StatusUnauthorized, "sem sessão")
}

func TestWhatsappBoletosStatusETeste(t *testing.T) {
	s, admin, falso, _ := cenarioWhatsappBoletos(t, true)
	s.cfg.BoletosEnvioHabilitado = true

	rec := admin.req(http.MethodGet, "/integracoes/whatsapp-boletos", nil, nil)
	esperarStatus(t, rec, http.StatusOK, "status")
	estado := decodificar[struct {
		Disponivel              bool           `json:"disponivel"`
		Estado                  string         `json:"estado"`
		Numero                  *string        `json:"numero"`
		QR                      *string        `json:"qr"`
		EnvioHabilitadoServidor bool           `json:"envio_habilitado_servidor"`
		Fila                    map[string]int `json:"fila"`
	}](t, rec)
	if !estado.Disponivel || estado.Estado != "conectado" || estado.Numero == nil || !estado.EnvioHabilitadoServidor {
		t.Fatalf("estado = %+v", estado)
	}
	if estado.Fila["pendente"] != 2 {
		t.Errorf("fila = %+v", estado.Fila)
	}

	// O telefone é normalizado antes de ir para o serviço.
	rec = admin.enviarJSON(http.MethodPost, "/integracoes/whatsapp-boletos/teste", map[string]string{"telefone": "(11) 90000-0000"})
	esperarStatus(t, rec, http.StatusAccepted, "mandar teste")
	if len(falso.testes) != 1 || falso.testes[0] != "5511900000000" {
		t.Errorf("telefone repassado = %+v", falso.testes)
	}
	// Telefone impossível não chega ao serviço.
	esperarStatus(t, admin.enviarJSON(http.MethodPost, "/integracoes/whatsapp-boletos/teste",
		map[string]string{"telefone": "123"}), http.StatusBadRequest, "telefone curto")
	if len(falso.testes) != 1 {
		t.Error("telefone inválido não devia ser repassado")
	}
	if contarAuditoria(t, s, "boletos_whatsapp_teste") != 1 {
		t.Error("teste não auditado")
	}

	esperarStatus(t, admin.enviarJSON(http.MethodPost, "/integracoes/whatsapp-boletos/desconectar", map[string]bool{}),
		http.StatusBadRequest, "desconectar sem confirmar")
	esperarStatus(t, admin.enviarJSON(http.MethodPost, "/integracoes/whatsapp-boletos/desconectar", map[string]bool{"confirmar": true}),
		http.StatusNoContent, "desconectar")
	if falso.desconexoes != 1 {
		t.Errorf("desconexões = %d", falso.desconexoes)
	}
}

func TestWhatsappBoletosDesconectadoENaoConfigurado(t *testing.T) {
	// Serviço no ar, WhatsApp sem parear: o QR aparece e o teste é recusado com explicação.
	_, admin, _, _ := cenarioWhatsappBoletos(t, false)
	rec := admin.req(http.MethodGet, "/integracoes/whatsapp-boletos", nil, nil)
	esperarStatus(t, rec, http.StatusOK, "status sem pareamento")
	estado := decodificar[struct {
		Estado string  `json:"estado"`
		QR     *string `json:"qr"`
	}](t, rec)
	if estado.Estado != "aguardando_qr" || estado.QR == nil {
		t.Errorf("estado = %+v", estado)
	}
	esperarStatus(t, admin.enviarJSON(http.MethodPost, "/integracoes/whatsapp-boletos/teste",
		map[string]string{"telefone": "11900000000"}), http.StatusConflict, "teste sem pareamento")

	// Serviço não configurado (sem URL/token): a tela mostra "indisponível", não erro.
	s2 := novoServidorTeste(t)
	h2 := s2.Rotas()
	criarUsuario(t, s2, "admin@exemplo.com", db.PerfilUsuarioAdmin)
	admin2, _ := entrar(t, h2, "admin@exemplo.com", senhaTeste)
	rec = admin2.req(http.MethodGet, "/integracoes/whatsapp-boletos", nil, nil)
	esperarStatus(t, rec, http.StatusOK, "serviço não configurado")
	semServico := decodificar[struct {
		Disponivel bool   `json:"disponivel"`
		Estado     string `json:"estado"`
	}](t, rec)
	if semServico.Disponivel || semServico.Estado != "indisponivel" {
		t.Errorf("sem serviço = %+v", semServico)
	}
}
