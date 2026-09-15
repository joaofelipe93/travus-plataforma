package auth

import (
	"strings"
	"testing"
)

func TestHashEVerificacaoDeSenha(t *testing.T) {
	hash, err := GerarHashSenha("correta-cavalo-bateria")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("formato inesperado: %s", hash)
	}

	ok, err := VerificarSenha("correta-cavalo-bateria", hash)
	if err != nil || !ok {
		t.Fatalf("senha correta recusada: ok=%v err=%v", ok, err)
	}
	ok, err = VerificarSenha("correta-cavalo-bateriA", hash)
	if err != nil || ok {
		t.Fatalf("senha errada aceita: ok=%v err=%v", ok, err)
	}

	outro, _ := GerarHashSenha("correta-cavalo-bateria")
	if outro == hash {
		t.Error("dois hashes da mesma senha deveriam ter sais diferentes")
	}
}

func TestVerificarSenhaComHashInvalido(t *testing.T) {
	for _, h := range []string{"", "texto", "$argon2i$v=19$m=1,t=1,p=1$c2Fs$aGFzaA", "$argon2id$v=19$m=0,t=1,p=1$c2Fs$aGFzaA"} {
		if ok, err := VerificarSenha("x", h); ok || err == nil {
			t.Errorf("hash %q: ok=%v err=%v, esperava erro", h, ok, err)
		}
	}
}

func TestValidarNovaSenha(t *testing.T) {
	if err := ValidarNovaSenha("curta"); err == nil {
		t.Error("senha curta aceita")
	}
	if err := ValidarNovaSenha("ãããããããããããã"); err != nil {
		t.Errorf("12 caracteres acentuados recusados: %v", err)
	}
	if err := ValidarNovaSenha(strings.Repeat("a", 257)); err == nil {
		t.Error("senha enorme aceita")
	}
}

func TestTokens(t *testing.T) {
	token, hash, err := NovoTokenSessao()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 || len(hash) != 32 {
		t.Fatalf("tamanhos inesperados: token=%d hash=%d", len(token), len(hash))
	}
	if string(HashToken(token)) != string(hash) {
		t.Error("HashToken não bate com o hash gerado")
	}
	outro, _, _ := NovoTokenSessao()
	if outro == token {
		t.Error("tokens repetidos")
	}

	csrf, _ := NovoTokenCSRF()
	if !CSRFConfere(csrf, csrf) || CSRFConfere("", "") || CSRFConfere(csrf, csrf+"x") {
		t.Error("CSRFConfere com resultado errado")
	}
}
