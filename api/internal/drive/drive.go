// Package drive envia os comprovantes (PDF) de lance para uma pasta do Google Drive.
// Escopo drive.file: o app só enxerga os arquivos que ele mesmo criou.
package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const Escopo = "https://www.googleapis.com/auth/drive.file"

const urlUpload = "https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart&fields=id,name,webViewLink"

// Endpoint OAuth do Google (sem importar golang.org/x/oauth2/google e suas dependências).
var endpointGoogle = oauth2.Endpoint{
	AuthURL:   "https://accounts.google.com/o/oauth2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

// TokenNode é o formato do token.json gravado pelo googleapis no script legado.
type TokenNode struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	ExpiryDate   int64  `json:"expiry_date"` // milissegundos
}

func (t TokenNode) OAuth2() (*oauth2.Token, error) {
	if t.RefreshToken == "" {
		return nil, errors.New("token sem refresh_token: autorize de novo o Google Drive")
	}
	if t.Scope != "" && !strings.Contains(t.Scope, Escopo) {
		return nil, fmt.Errorf("token sem o escopo %s (tem: %s)", Escopo, t.Scope)
	}
	tok := &oauth2.Token{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, TokenType: t.TokenType}
	if t.ExpiryDate > 0 {
		tok.Expiry = time.UnixMilli(t.ExpiryDate)
	}
	return tok, nil
}

type Arquivo struct {
	ID   string `json:"id"`
	Nome string `json:"name"`
	Link string `json:"webViewLink"`
}

// Enviador é o que a fila de envio precisa (o Cliente real ou um falso nos testes).
type Enviador interface {
	Enviar(ctx context.Context, nome string, pdf []byte) (Arquivo, error)
}

type Cliente struct {
	http      *http.Client
	pasta     string
	urlUpload string
}

// NovoCliente renova o access token sozinho; aoRenovar recebe o token novo para ser salvo.
func NovoCliente(clientID, clientSecret string, token *oauth2.Token, pasta string, aoRenovar func(*oauth2.Token)) *Cliente {
	cfg := &oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, Endpoint: endpointGoogle, Scopes: []string{Escopo}}
	fonte := &fonteQueAvisa{base: cfg.TokenSource(context.Background(), token), ultimo: token.AccessToken, aoRenovar: aoRenovar}
	return &Cliente{http: oauth2.NewClient(context.Background(), fonte), pasta: pasta, urlUpload: urlUpload}
}

type fonteQueAvisa struct {
	base      oauth2.TokenSource
	mu        sync.Mutex
	ultimo    string
	aoRenovar func(*oauth2.Token)
}

func (f *fonteQueAvisa) Token() (*oauth2.Token, error) {
	tok, err := f.base.Token()
	if err != nil {
		return nil, fmt.Errorf("renovando o token do Google (autorização revogada?): %w", err)
	}
	f.mu.Lock()
	mudou := tok.AccessToken != f.ultimo
	f.ultimo = tok.AccessToken
	f.mu.Unlock()
	if mudou && f.aoRenovar != nil {
		f.aoRenovar(tok)
	}
	return tok, nil
}

func (c *Cliente) Enviar(ctx context.Context, nome string, pdf []byte) (Arquivo, error) {
	if c.pasta == "" {
		return Arquivo{}, errors.New("pasta do Drive não configurada (GOOGLE_DRIVE_PASTA_ID)")
	}
	var corpo bytes.Buffer
	mw := multipart.NewWriter(&corpo)
	meta, _ := json.Marshal(map[string]any{"name": nome, "parents": []string{c.pasta}, "mimeType": "application/pdf"})
	parte, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	_, _ = parte.Write(meta)
	parte, _ = mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/pdf"}})
	_, _ = parte.Write(pdf)
	_ = mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.urlUpload, &corpo)
	if err != nil {
		return Arquivo{}, err
	}
	req.Header.Set("Content-Type", "multipart/related; boundary="+mw.Boundary())
	resp, err := c.http.Do(req)
	if err != nil {
		return Arquivo{}, fmt.Errorf("enviando ao Drive: %w", err)
	}
	defer resp.Body.Close()
	bruto, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Arquivo{}, fmt.Errorf("o Drive respondeu %d: %s", resp.StatusCode, strings.TrimSpace(string(bruto[:min(len(bruto), 300)])))
	}
	var a Arquivo
	if err := json.Unmarshal(bruto, &a); err != nil || a.ID == "" {
		return Arquivo{}, fmt.Errorf("resposta inesperada do Drive: %s", string(bruto[:min(len(bruto), 300)]))
	}
	return a, nil
}

// Mesma regra de reportFileName() em workers/canopus/src/newcon.js:
// "NOME DO CLIENTE - 006650-0924-00 - AAAA-MM-DD.pdf" (data no fuso de "data").
var caracteresProibidos = regexp.MustCompile("[\\\\/:*?\"<>|\\x00-\\x1f]")

func NomeArquivo(nomeCliente, grupo, cota, versao string, data time.Time) string {
	dia := data.Format("2006-01-02")
	tag := grupo + "-" + cota + "-" + versao
	cliente := strings.Join(strings.Fields(caracteresProibidos.ReplaceAllString(nomeCliente, " ")), " ")
	if cliente == "" {
		return "credenciamento_" + tag + "_" + dia + ".pdf"
	}
	return cliente + " - " + tag + " - " + dia + ".pdf"
}
