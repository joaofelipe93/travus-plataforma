package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
)

const mensagemErroInterno = "erro interno: tente de novo; se continuar, avise o suporte"

type respostaErro struct {
	Erro string `json:"erro"`
}

func responderJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("falha ao escrever resposta", "erro", err)
	}
}

func responderErro(w http.ResponseWriter, code int, mensagem string) {
	responderJSON(w, code, respostaErro{Erro: mensagem})
}

func erroInterno(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("erro interno", "metodo", r.Method, "caminho", r.URL.Path, "erro", err)
	responderErro(w, http.StatusInternalServerError, mensagemErroInterno)
}

// lerJSON decodifica um corpo JSON pequeno, recusando campos desconhecidos.
func lerJSON(w http.ResponseWriter, r *http.Request, destino any, limite int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limite)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(destino); err != nil {
		return fmt.Errorf("JSON inválido: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON inválido: conteúdo depois do objeto")
	}
	return nil
}

func idDoCaminho(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func metodoAltera(metodo string) bool {
	switch metodo {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// ipDaRequisicao usa o X-Real-Ip que o Traefik preenche (a API não é exposta direto).
func ipDaRequisicao(r *http.Request) string {
	if ip := r.Header.Get("X-Real-Ip"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func textoOuNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func valorOuVazio(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
