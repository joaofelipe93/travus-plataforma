// Package calendario sabe quando é dia útil no Brasil. Existe por uma instrução da issue #48:
// a cobrança do dia 14 "em caso de cair em um sábado, domingo ou feriado deve antecipar para
// 1 dia útil anterior".
//
// Que feriados entram: os **nacionais** (Lei 10.607 e Lei 14.759, que tornou o 20 de novembro
// feriado nacional em 2024) mais os dias em que **os bancos não abrem** — Carnaval (segunda e
// terça), Sexta-feira Santa e Corpus Christi. O critério é esse porque o que importa aqui é o
// cliente conseguir pagar o boleto, e isso depende do banco estar aberto.
//
// Que feriados NÃO entram: estaduais e municipais. Os clientes estão em cidades diferentes e
// não há como saber o calendário de cada um; um feriado local a mais apenas adianta o aviso em
// um dia, o que é inofensivo. Quarta-feira de Cinzas também não entra: os bancos abrem (à
// tarde) e o dia é útil.
//
// Se o usuário quiser outra regra, é aqui que se muda — e os testes falham de propósito.
package calendario

import (
	"fmt"
	"time"
)

// Fuso de Brasília: um feriado é um dia no calendário daqui, não em UTC.
var Fuso = func() *time.Location {
	if l, err := time.LoadLocation("America/Sao_Paulo"); err == nil {
		return l
	}
	return time.Local
}()

// Pascoa devolve o domingo de Páscoa do ano (algoritmo gregoriano anônimo).
func Pascoa(ano int) time.Time {
	a := ano % 19
	b := ano / 100
	c := ano % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	mes := (h + l - 7*m + 114) / 31
	dia := ((h + l - 7*m + 114) % 31) + 1
	return data(ano, time.Month(mes), dia)
}

func data(ano int, mes time.Month, dia int) time.Time {
	return time.Date(ano, mes, dia, 0, 0, 0, 0, Fuso)
}

// Feriados do ano, por data (AAAA-MM-DD), com o nome — o nome entra em mensagem de tela.
func Feriados(ano int) map[string]string {
	pascoa := Pascoa(ano)
	f := map[string]string{
		chave(data(ano, time.January, 1)):   "Confraternização Universal",
		chave(data(ano, time.April, 21)):    "Tiradentes",
		chave(data(ano, time.May, 1)):       "Dia do Trabalho",
		chave(data(ano, time.September, 7)): "Independência",
		chave(data(ano, time.October, 12)):  "Nossa Senhora Aparecida",
		chave(data(ano, time.November, 2)):  "Finados",
		chave(data(ano, time.November, 15)): "Proclamação da República",
		chave(data(ano, time.November, 20)): "Consciência Negra",
		chave(data(ano, time.December, 25)): "Natal",
		chave(pascoa.AddDate(0, 0, -48)):    "Carnaval (segunda)",
		chave(pascoa.AddDate(0, 0, -47)):    "Carnaval (terça)",
		chave(pascoa.AddDate(0, 0, -2)):     "Sexta-feira Santa",
		chave(pascoa.AddDate(0, 0, 60)):     "Corpus Christi",
	}
	return f
}

func chave(d time.Time) string { return d.Format(time.DateOnly) }

// Feriado devolve o nome do feriado naquele dia, ou "" se não for feriado.
func Feriado(d time.Time) string {
	d = d.In(Fuso)
	return Feriados(d.Year())[chave(d)]
}

// EhDiaUtil: não é sábado, não é domingo e não é feriado.
func EhDiaUtil(d time.Time) bool {
	d = d.In(Fuso)
	switch d.Weekday() {
	case time.Saturday, time.Sunday:
		return false
	}
	return Feriado(d) == ""
}

// DiaUtilAteOuAntes devolve o próprio dia, se for útil, ou o dia útil anterior mais próximo.
// É a regra da issue #48 para a cobrança: sábado, domingo ou feriado antecipa.
func DiaUtilAteOuAntes(d time.Time) time.Time {
	d = time.Date(d.In(Fuso).Year(), d.In(Fuso).Month(), d.In(Fuso).Day(), 0, 0, 0, 0, Fuso)
	for range 15 { // nenhum feriado brasileiro empilha 15 dias
		if EhDiaUtil(d) {
			return d
		}
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// DiaDoMes resolve "dia X do mês", com duas regras já em uso na plataforma e uma da issue #48:
//   - mês que não tem o dia escolhido (31 em fevereiro) usa o último dia do mês;
//   - com `antecipar`, sábado, domingo e feriado voltam para o dia útil anterior.
//
// Devolve a data no fuso de Brasília, à meia-noite.
func DiaDoMes(ano int, mes time.Month, dia int, antecipar bool) (time.Time, error) {
	if dia < 1 || dia > 31 {
		return time.Time{}, fmt.Errorf("dia do mês inválido: %d", dia)
	}
	ultimo := data(ano, mes, 1).AddDate(0, 1, -1).Day()
	if dia > ultimo {
		dia = ultimo
	}
	d := data(ano, mes, dia)
	if antecipar {
		d = DiaUtilAteOuAntes(d)
	}
	return d, nil
}

// Ocorrencia é o momento (dia e hora) de uma rodada automática no mês de referência.
func Ocorrencia(ano int, mes time.Month, dia, hora int, antecipar bool) (time.Time, error) {
	d, err := DiaDoMes(ano, mes, dia, antecipar)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(d.Year(), d.Month(), d.Day(), hora, 0, 0, 0, Fuso), nil
}
