package drive

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// Esperados gerados rodando reportFileName() do newcon.js original (data 13/09/2026 23:30 local).
func TestNomeArquivoIgualAoNewconJs(t *testing.T) {
	data := time.Date(2026, 9, 13, 23, 30, 0, 0, time.FixedZone("BRT", -3*3600))
	casos := []struct{ nome, grupo, cota, versao, esperado string }{
		{"CLIENTE EXEMPLO UM", "006650", "0924", "00", "CLIENTE EXEMPLO UM - 006650-0924-00 - 2026-09-13.pdf"},
		{"  joão da silva-souza  ", "006650", "0924", "00", "joão da silva-souza - 006650-0924-00 - 2026-09-13.pdf"},
		{`A/B\C:D*E?F"G<H>I|J`, "001234", "0056", "01", "A B C D E F G H I J - 001234-0056-01 - 2026-09-13.pdf"},
		{"EXEMPLO, CLIENTE TRÊS", "006660", "1488", "00", "EXEMPLO, CLIENTE TRÊS - 006660-1488-00 - 2026-09-13.pdf"},
		{"", "006660", "1488", "00", "credenciamento_006660-1488-00_2026-09-13.pdf"},
	}
	for _, c := range casos {
		if got := NomeArquivo(c.nome, c.grupo, c.cota, c.versao, data); got != c.esperado {
			t.Errorf("NomeArquivo(%q) = %q, quer %q", c.nome, got, c.esperado)
		}
	}
}

func TestEnviarMultipart(t *testing.T) {
	var metadados, pdf string
	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-de-acesso" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		leitor := multipart.NewReader(r.Body, params["boundary"])
		p1, _ := leitor.NextPart()
		b1, _ := io.ReadAll(p1)
		p2, _ := leitor.NextPart()
		b2, _ := io.ReadAll(p2)
		metadados, pdf = string(b1), string(b2)
		w.Write([]byte(`{"id":"arquivo-123","name":"x.pdf","webViewLink":"https://drive.google.com/file/d/arquivo-123/view"}`))
	}))
	defer servidor.Close()

	c := &Cliente{
		http:      oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token-de-acesso", TokenType: "Bearer"})),
		pasta:     "pasta-xyz",
		urlUpload: servidor.URL,
	}
	a, err := c.Enviar(context.Background(), "CLIENTE - 006650-0924-00 - 2026-09-13.pdf", []byte("%PDF-1.4 teste"))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "arquivo-123" || !strings.Contains(a.Link, "arquivo-123") {
		t.Errorf("arquivo: %+v", a)
	}
	if !strings.Contains(metadados, `"parents":["pasta-xyz"]`) || !strings.Contains(metadados, `"name":"CLIENTE - 006650-0924-00 - 2026-09-13.pdf"`) {
		t.Errorf("metadados: %s", metadados)
	}
	if pdf != "%PDF-1.4 teste" {
		t.Errorf("pdf: %q", pdf)
	}
}

func TestEnviarErros(t *testing.T) {
	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"File not found: pasta"}}`, http.StatusNotFound)
	}))
	defer servidor.Close()
	c := &Cliente{http: http.DefaultClient, pasta: "p", urlUpload: servidor.URL}
	if _, err := c.Enviar(context.Background(), "x.pdf", []byte("%PDF-")); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("erro esperado com 404: %v", err)
	}
	semPasta := &Cliente{http: http.DefaultClient, urlUpload: servidor.URL}
	if _, err := semPasta.Enviar(context.Background(), "x.pdf", []byte("%PDF-")); err == nil {
		t.Error("enviou sem pasta configurada")
	}
}

func TestTokenNode(t *testing.T) {
	tok, err := TokenNode{AccessToken: "a", RefreshToken: "r", Scope: Escopo, TokenType: "Bearer", ExpiryDate: 1789428400000}.OAuth2()
	if err != nil || tok.RefreshToken != "r" || tok.Expiry.UnixMilli() != 1789428400000 {
		t.Fatalf("token: %+v %v", tok, err)
	}
	if _, err := (TokenNode{AccessToken: "a"}).OAuth2(); err == nil {
		t.Error("aceitou token sem refresh_token")
	}
	if _, err := (TokenNode{RefreshToken: "r", Scope: "https://www.googleapis.com/auth/gmail.readonly"}).OAuth2(); err == nil {
		t.Error("aceitou token com outro escopo")
	}
}

type fonteFixa struct{ tokens []string }

func (f *fonteFixa) Token() (*oauth2.Token, error) {
	tok := &oauth2.Token{AccessToken: f.tokens[0], Expiry: time.Now().Add(time.Hour)}
	if len(f.tokens) > 1 {
		f.tokens = f.tokens[1:]
	}
	return tok, nil
}

func TestFonteAvisaQuandoRenova(t *testing.T) {
	var salvos []string
	f := &fonteQueAvisa{base: &fonteFixa{tokens: []string{"antigo", "antigo", "novo"}}, ultimo: "antigo", aoRenovar: func(t *oauth2.Token) { salvos = append(salvos, t.AccessToken) }}
	for i := 0; i < 3; i++ {
		if _, err := f.Token(); err != nil {
			t.Fatal(err)
		}
	}
	if len(salvos) != 1 || salvos[0] != "novo" {
		t.Errorf("salvos = %v", salvos)
	}
}
