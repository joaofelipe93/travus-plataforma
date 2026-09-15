// Package auth cuida de senhas, tokens de sessão e CSRF.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Parâmetros do argon2id (mínimo recomendado pela OWASP). Cada cálculo usa ~19 MiB,
// então limitamos quantos rodam ao mesmo tempo para a API caber em 256 MB.
const (
	argonMemoriaKiB  = 19 * 1024
	argonIteracoes   = 2
	argonParalelismo = 1
	argonTamanhoSal  = 16
	argonTamanhoHash = 32

	SenhaTamanhoMinimo = 12
	senhaTamanhoMaximo = 256
)

var vagasHash = make(chan struct{}, 2)

var ErrHashInvalido = errors.New("hash de senha em formato desconhecido")

// ValidarNovaSenha aplica as regras para senhas novas.
func ValidarNovaSenha(senha string) error {
	n := utf8.RuneCountInString(senha)
	if n < SenhaTamanhoMinimo {
		return fmt.Errorf("a senha precisa ter pelo menos %d caracteres", SenhaTamanhoMinimo)
	}
	if n > senhaTamanhoMaximo {
		return fmt.Errorf("a senha pode ter no máximo %d caracteres", senhaTamanhoMaximo)
	}
	return nil
}

// GerarHashSenha devolve o hash no formato PHC: $argon2id$v=19$m=...,t=...,p=...$sal$hash.
func GerarHashSenha(senha string) (string, error) {
	sal := make([]byte, argonTamanhoSal)
	if _, err := rand.Read(sal); err != nil {
		return "", err
	}
	hash := calcular(senha, sal, argonMemoriaKiB, argonIteracoes, argonParalelismo, argonTamanhoHash)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoriaKiB, argonIteracoes, argonParalelismo,
		base64.RawStdEncoding.EncodeToString(sal), base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerificarSenha compara em tempo constante a senha com um hash gerado por GerarHashSenha.
func VerificarSenha(senha, codificado string) (bool, error) {
	partes := strings.Split(codificado, "$")
	if len(partes) != 6 || partes[1] != "argon2id" {
		return false, ErrHashInvalido
	}
	var versao int
	if _, err := fmt.Sscanf(partes[2], "v=%d", &versao); err != nil || versao != argon2.Version {
		return false, ErrHashInvalido
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(partes[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || t == 0 || p == 0 {
		return false, ErrHashInvalido
	}
	sal, err := base64.RawStdEncoding.DecodeString(partes[4])
	if err != nil {
		return false, ErrHashInvalido
	}
	esperado, err := base64.RawStdEncoding.DecodeString(partes[5])
	if err != nil || len(esperado) == 0 {
		return false, ErrHashInvalido
	}
	obtido := calcular(senha, sal, m, t, p, uint32(len(esperado)))
	return subtle.ConstantTimeCompare(obtido, esperado) == 1, nil
}

var (
	hashFalsoOnce sync.Once
	hashFalso     string
)

// SimularVerificacao gasta o mesmo tempo de uma verificação real. Usada quando o e-mail
// não existe, para o tempo de resposta não revelar quais e-mails estão cadastrados.
func SimularVerificacao(senha string) {
	hashFalsoOnce.Do(func() {
		hashFalso, _ = GerarHashSenha("senha-inexistente-para-tempo-constante")
	})
	_, _ = VerificarSenha(senha, hashFalso)
}

func calcular(senha string, sal []byte, m, t uint32, p uint8, tamanho uint32) []byte {
	vagasHash <- struct{}{}
	defer func() { <-vagasHash }()
	return argon2.IDKey([]byte(senha), sal, t, m, p, tamanho)
}
