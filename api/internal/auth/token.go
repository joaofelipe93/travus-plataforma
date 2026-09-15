package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// NovoTokenSessao gera o token que vai no cookie (256 bits aleatórios) e o SHA-256
// dele, que é o que fica no banco.
func NovoTokenSessao() (token string, hash []byte, err error) {
	token, err = aleatorio(32)
	if err != nil {
		return "", nil, err
	}
	return token, HashToken(token), nil
}

// HashToken é a chave da sessão no banco. Quem lê o banco não consegue montar o cookie.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// NovoTokenCSRF gera o token que o navegador precisa mandar em X-CSRF-Token.
func NovoTokenCSRF() (string, error) {
	return aleatorio(32)
}

// CSRFConfere compara em tempo constante.
func CSRFConfere(recebido, esperado string) bool {
	if recebido == "" || esperado == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(recebido), []byte(esperado)) == 1
}

func aleatorio(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
