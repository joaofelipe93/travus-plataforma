// Package cripto cifra segredos guardados no banco (ex.: token do Google Drive).
package cripto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

var ErrDadosInvalidos = errors.New("dados cifrados inválidos, adulterados ou de outra chave")

// Cofre cifra com AES-256-GCM. Formato gravado: nonce (12 bytes) || texto cifrado com tag.
// O "contexto" (ex.: "integracao:google_drive") entra como dado autenticado: um valor
// cifrado para um contexto não decifra em outro.
type Cofre struct {
	aead cipher.AEAD
}

// NovoCofre recebe a chave em hexadecimal (64 caracteres = 32 bytes).
func NovoCofre(chaveHex string) (*Cofre, error) {
	chave, err := hex.DecodeString(chaveHex)
	if err != nil || len(chave) != 32 {
		return nil, errors.New("CHAVE_CRIPTOGRAFIA precisa ter 64 caracteres hexadecimais (32 bytes; o make gera)")
	}
	bloco, err := aes.NewCipher(chave)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(bloco)
	if err != nil {
		return nil, err
	}
	return &Cofre{aead: aead}, nil
}

func (c *Cofre) Cifrar(texto []byte, contexto string) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("gerando nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, texto, []byte(contexto)), nil
}

func (c *Cofre) Decifrar(dados []byte, contexto string) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(dados) < n+c.aead.Overhead() {
		return nil, ErrDadosInvalidos
	}
	texto, err := c.aead.Open(nil, dados[:n], dados[n:], []byte(contexto))
	if err != nil {
		return nil, ErrDadosInvalidos
	}
	return texto, nil
}
