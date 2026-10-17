package calendario

import (
	"testing"
	"time"
)

func dia(texto string) time.Time {
	d, err := time.ParseInLocation(time.DateOnly, texto, Fuso)
	if err != nil {
		panic(err)
	}
	return d
}

func TestPascoa(t *testing.T) {
	// Datas conferidas no calendário: é o que ancora Carnaval, Sexta-feira Santa e Corpus Christi.
	casos := map[int]string{
		2024: "2024-03-31",
		2025: "2025-04-20",
		2026: "2026-04-05",
		2027: "2027-03-28",
		2030: "2030-04-21",
	}
	for ano, esperado := range casos {
		if got := Pascoa(ano).Format(time.DateOnly); got != esperado {
			t.Errorf("Pascoa(%d) = %s, quer %s", ano, got, esperado)
		}
	}
}

func TestFeriadosMoveis(t *testing.T) {
	// 2026: Páscoa em 05/04 → Carnaval 16 e 17/02, Sexta-feira Santa 03/04, Corpus Christi 04/06.
	casos := map[string]string{
		"2026-02-16": "Carnaval (segunda)",
		"2026-02-17": "Carnaval (terça)",
		"2026-04-03": "Sexta-feira Santa",
		"2026-06-04": "Corpus Christi",
		"2026-11-20": "Consciência Negra",
		"2026-12-25": "Natal",
	}
	for data, nome := range casos {
		if got := Feriado(dia(data)); got != nome {
			t.Errorf("Feriado(%s) = %q, quer %q", data, got, nome)
		}
	}
	// Quarta-feira de Cinzas é dia útil (os bancos abrem à tarde).
	if got := Feriado(dia("2026-02-18")); got != "" {
		t.Errorf("quarta-feira de cinzas não é feriado aqui, veio %q", got)
	}
}

func TestEhDiaUtil(t *testing.T) {
	casos := map[string]bool{
		"2026-10-16": true,  // sexta comum
		"2026-10-17": false, // sábado
		"2026-10-18": false, // domingo
		"2026-10-12": false, // Aparecida, numa segunda
		"2026-11-20": false, // Consciência Negra, numa sexta
	}
	for data, esperado := range casos {
		if got := EhDiaUtil(dia(data)); got != esperado {
			t.Errorf("EhDiaUtil(%s) = %v, quer %v", data, got, esperado)
		}
	}
}

func TestDiaUtilAteOuAntes(t *testing.T) {
	casos := map[string]string{
		"2026-10-16": "2026-10-16", // dia útil fica onde está
		"2026-10-17": "2026-10-16", // sábado → sexta
		"2026-10-18": "2026-10-16", // domingo → sexta
		"2026-10-12": "2026-10-09", // feriado na segunda → sexta anterior
		"2026-11-20": "2026-11-19", // feriado na sexta → quinta
		// 02/11/2026 (Finados) é segunda: volta para a sexta 30/10.
		"2026-11-02": "2026-10-30",
		// 01/01/2027 é sexta-feira e feriado: volta para 31/12/2026 (quinta).
		"2027-01-01": "2026-12-31",
	}
	for data, esperado := range casos {
		if got := DiaUtilAteOuAntes(dia(data)).Format(time.DateOnly); got != esperado {
			t.Errorf("DiaUtilAteOuAntes(%s) = %s, quer %s", data, got, esperado)
		}
	}
}

func TestDiaDoMes(t *testing.T) {
	// A regra da issue #48 no dia 14, mês a mês de 2026.
	// 14/02/2026 é sábado → 13 (sexta). 14/03/2026 é sábado → 13. 14/06/2026 é domingo → 12.
	casos := []struct {
		ano      int
		mes      time.Month
		dia      int
		esperado string
	}{
		{2026, time.January, 14, "2026-01-14"},  // quarta
		{2026, time.February, 14, "2026-02-13"}, // sábado → sexta
		{2026, time.March, 14, "2026-03-13"},    // sábado → sexta
		{2026, time.June, 14, "2026-06-12"},     // domingo → sexta
		{2026, time.November, 14, "2026-11-13"}, // sábado → sexta
		{2026, time.February, 31, "2026-02-27"}, // mês sem o dia 31 usa o último (28, sábado) → 27
	}
	for _, c := range casos {
		got, err := DiaDoMes(c.ano, c.mes, c.dia, true)
		if err != nil {
			t.Fatal(err)
		}
		if got.Format(time.DateOnly) != c.esperado {
			t.Errorf("DiaDoMes(%d, %s, %d) = %s, quer %s", c.ano, c.mes, c.dia, got.Format(time.DateOnly), c.esperado)
		}
	}

	// Sem antecipar, dia 14 é dia 14, mesmo no sábado.
	semAntecipar, err := DiaDoMes(2026, time.February, 14, false)
	if err != nil {
		t.Fatal(err)
	}
	if semAntecipar.Format(time.DateOnly) != "2026-02-14" {
		t.Errorf("sem antecipar = %s", semAntecipar.Format(time.DateOnly))
	}

	if _, err := DiaDoMes(2026, time.March, 0, true); err == nil {
		t.Error("dia 0 devia falhar")
	}
	if _, err := DiaDoMes(2026, time.March, 32, true); err == nil {
		t.Error("dia 32 devia falhar")
	}
}

func TestOcorrencia(t *testing.T) {
	// O dia 8 de agosto de 2026 é sábado: antecipa para 7, às 9h de Brasília.
	o, err := Ocorrencia(2026, time.August, 8, 9, true)
	if err != nil {
		t.Fatal(err)
	}
	if o.Format("2006-01-02 15:04 -0700") != "2026-08-07 09:00 -0300" {
		t.Errorf("ocorrência = %s", o.Format("2006-01-02 15:04 -0700"))
	}
}
