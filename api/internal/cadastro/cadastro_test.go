package cadastro

import (
	"errors"
	"testing"
	"time"
)

func TestNome(t *testing.T) {
	nome, normalizado, err := Nome("  joão   machado  de siqueira ")
	if err != nil {
		t.Fatal(err)
	}
	if nome != "joão machado de siqueira" {
		t.Errorf("nome = %q", nome)
	}
	if normalizado != "JOÃO MACHADO DE SIQUEIRA" {
		t.Errorf("normalizado = %q", normalizado)
	}
	if _, _, err := Nome("   "); err == nil {
		t.Error("nome vazio devia falhar")
	}
}

func TestGrupoCotaVersao(t *testing.T) {
	casos := []struct {
		fn    func(string) (string, error)
		campo string
		in    string
		want  string
		erro  bool
	}{
		{Grupo, "grupo", "6650", "006650", false},
		{Grupo, "grupo", "6.650", "006650", false}, // a planilha às vezes traz ponto de milhar
		{Grupo, "grupo", " 006650 ", "006650", false},
		{Grupo, "grupo", "0", "", true},
		{Grupo, "grupo", "1234567", "", true},
		{Grupo, "grupo", "", "", true},
		{Cota, "cota", "924", "0924", false},
		{Cota, "cota", "12", "0012", false},
		{Cota, "cota", "abc", "", true},
		{Versao, "versão", "", "00", false},
		{Versao, "versão", "0", "00", false},
		{Versao, "versão", "3", "03", false},
		{Versao, "versão", "123", "", true},
	}
	for _, c := range casos {
		got, err := c.fn(c.in)
		if c.erro {
			if err == nil {
				t.Errorf("%s(%q) = %q, queria erro", c.campo, c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s(%q) = %q, %v; quer %q", c.campo, c.in, got, err, c.want)
		}
	}
}

func TestEmail(t *testing.T) {
	v, err := Email(" pessoa@exemplo.com ")
	if err != nil || v == nil || *v != "pessoa@exemplo.com" {
		t.Errorf("e-mail válido: %v, %v", v, err)
	}
	if v, err := Email("  "); err != nil || v != nil {
		t.Errorf("e-mail vazio devia virar nulo: %v, %v", v, err)
	}
	for _, ruim := range []string{"sem-arroba", "@exemplo.com", "pessoa@", "a@b@c.com", "pessoa@exemplo", "com espaco@exemplo.com"} {
		if _, err := Email(ruim); err == nil {
			t.Errorf("e-mail %q devia falhar", ruim)
		}
	}
}

func TestModalidadeEAdministradora(t *testing.T) {
	if m, err := Modalidade(""); err != nil || m != ModalidadePadrao {
		t.Errorf("modalidade vazia = %q, %v", m, err)
	}
	if m, err := Modalidade("Segundo_Fixo"); err != nil || m != "segundo_fixo" {
		t.Errorf("modalidade com maiúsculas = %q, %v", m, err)
	}
	if _, err := Modalidade("turbo"); err == nil {
		t.Error("modalidade inexistente devia falhar")
	}
	if a, err := Administradora(" "); err != nil || a != AdministradoraPad {
		t.Errorf("administradora vazia = %q, %v", a, err)
	}
	if a, err := Administradora("canopus"); err != nil || a != "CANOPUS" {
		t.Errorf("administradora em minúsculas = %q, %v", a, err)
	}
}

func TestDiaDoMesEData(t *testing.T) {
	if d, err := DiaDoMes("vencimento", nil); err != nil || d != nil {
		t.Errorf("dia vazio = %v, %v", d, err)
	}
	quinze := 15
	if d, err := DiaDoMes("vencimento", &quinze); err != nil || d == nil || *d != 15 {
		t.Errorf("dia 15 = %v, %v", d, err)
	}
	for _, fora := range []int{0, 32, -1} {
		if _, err := DiaDoMes("vencimento", &fora); err == nil {
			t.Errorf("dia %d devia falhar", fora)
		}
	}
	d, err := Data("contratação", "2025-10-01")
	if err != nil || d == nil || !d.Equal(time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("data = %v, %v", d, err)
	}
	if _, err := Data("contratação", "10/01/2025"); err == nil {
		t.Error("data no formato da planilha devia falhar (a web manda AAAA-MM-DD)")
	}
}

func TestErroDeValidacaoEIdentificavel(t *testing.T) {
	_, err := Grupo("abc")
	var ev Erro
	if !errors.As(err, &ev) || ev.Mensagem == "" {
		t.Errorf("erro de validação não identificável: %v", err)
	}
}
