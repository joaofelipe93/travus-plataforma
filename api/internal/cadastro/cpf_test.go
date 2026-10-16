package cadastro

import (
	"errors"
	"testing"
)

func TestCpfCnpj(t *testing.T) {
	// Documentos fictícios, com dígito verificador certo (o repositório é público).
	validos := map[string]string{
		"111.222.333-96":     "11122233396",
		" 529 982 247 25 ":   "52998224725",
		"11.222.333/0001-81": "11222333000181",
		"12345678000195":     "12345678000195",
	}
	for bruto, esperado := range validos {
		v, err := CpfCnpj(bruto)
		if err != nil {
			t.Errorf("CpfCnpj(%q): %v", bruto, err)
			continue
		}
		if v == nil || *v != esperado {
			t.Errorf("CpfCnpj(%q) = %v, queria %q", bruto, v, esperado)
		}
	}

	if v, err := CpfCnpj("   "); err != nil || v != nil {
		t.Errorf("documento vazio devia virar nulo: %v, %v", v, err)
	}

	invalidos := []string{
		"111.222.333-00", // dígito verificador errado
		"111.111.111-11", // dígitos todos iguais passam na conta e não valem
		"11.222.333/0001-00",
		"00.000.000/0000-00",
		"1234567890",   // 10 dígitos
		"123456789012", // 12 dígitos
		"abc",
	}
	for _, bruto := range invalidos {
		v, err := CpfCnpj(bruto)
		if err == nil {
			t.Errorf("CpfCnpj(%q) = %v, devia falhar", bruto, v)
			continue
		}
		var ev Erro
		if !errors.As(err, &ev) {
			t.Errorf("CpfCnpj(%q): erro devia ser de validação, veio %T", bruto, err)
		}
	}
}
