package cripto

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const chaveA = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
const chaveB = "ff0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestCifrarEDecifrar(t *testing.T) {
	cofre, err := NovoCofre(chaveA)
	if err != nil {
		t.Fatal(err)
	}
	segredo := []byte(`{"refresh_token":"1//exemplo"}`)
	cifrado, err := cofre.Cifrar(segredo, "integracao:google_drive")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cifrado, []byte("refresh_token")) {
		t.Fatal("texto aparece no valor cifrado")
	}
	outro, _ := cofre.Cifrar(segredo, "integracao:google_drive")
	if bytes.Equal(cifrado, outro) {
		t.Error("duas cifragens iguais: nonce repetido")
	}

	texto, err := cofre.Decifrar(cifrado, "integracao:google_drive")
	if err != nil || !bytes.Equal(texto, segredo) {
		t.Fatalf("decifrar: %q %v", texto, err)
	}

	if _, err := cofre.Decifrar(cifrado, "integracao:outra"); !errors.Is(err, ErrDadosInvalidos) {
		t.Errorf("contexto errado decifrou: %v", err)
	}
	adulterado := append([]byte{}, cifrado...)
	adulterado[len(adulterado)-1] ^= 1
	if _, err := cofre.Decifrar(adulterado, "integracao:google_drive"); !errors.Is(err, ErrDadosInvalidos) {
		t.Errorf("valor adulterado decifrou: %v", err)
	}
	if _, err := cofre.Decifrar([]byte("curto"), "integracao:google_drive"); !errors.Is(err, ErrDadosInvalidos) {
		t.Errorf("valor curto: %v", err)
	}

	cofreB, _ := NovoCofre(chaveB)
	if _, err := cofreB.Decifrar(cifrado, "integracao:google_drive"); !errors.Is(err, ErrDadosInvalidos) {
		t.Errorf("outra chave decifrou: %v", err)
	}
}

func TestChaveInvalida(t *testing.T) {
	for _, chave := range []string{"", "abc", strings.Repeat("0", 62), strings.Repeat("z", 64)} {
		if _, err := NovoCofre(chave); err == nil {
			t.Errorf("chave %q aceita", chave)
		}
	}
}
