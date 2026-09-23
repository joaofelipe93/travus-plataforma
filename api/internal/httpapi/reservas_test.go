package httpapi

// Tela Reservas. Os testes de montagem não usam banco; o da rota precisa de Postgres
// (TEST_DATABASE_URL, rode com `make test`). Só dados fictícios: o repositório é público.

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Formato real do PMS (o mesmo congelado nos testes do notificador), com dados inventados.
const payloadReservaFicticia = `{
  "guests": "2", "nights": "2", "status": "confirmed", "channel": "airbnb",
  "check_in": "11/09/2026", "check_out": "13/09/2026",
  "guest_name": "Hóspede Fictício", "guest_email": null, "guest_phone": "5511900000000",
  "total_price": "1.360,50", "booking_uuid": "00000000-0000-4000-8000-000000000001",
  "property_name": "Chalé 03", "property_uuid": "00000000-0000-4000-8000-0000000000aa",
  "cancellation_reason": "", "fnrh_precheckin_link": ""
}`

const payloadCancelamentoFicticio = `{
  "guests": "2", "nights": "2", "status": "cancelled", "channel": "airbnb",
  "check_in": "11/09/2026", "check_out": "13/09/2026",
  "guest_name": "Hóspede Fictício", "guest_phone": "5511900000000",
  "total_price": "1.360,50", "booking_uuid": "00000000-0000-4000-8000-000000000001",
  "property_name": "Chalé 03", "cancellation_reason": "Pedido do hóspede"
}`

func eventoFicticio(t *testing.T, id int64, payload string, recebido time.Time, mensagem string) eventoReserva {
	t.Helper()
	var p any
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatal(err)
	}
	return eventoReserva{ID: id, Payload: p, RecebidoEm: recebido, Mensagem: mensagem}
}

func TestInterpretarPayloadDoPMS(t *testing.T) {
	var p any
	_ = json.Unmarshal([]byte(payloadReservaFicticia), &p)
	d := interpretarPayload(p)
	if d.cancelamento {
		t.Fatal("reserva confirmada lida como cancelamento")
	}
	quer := map[string]*string{
		"hospede": d.Hospede, "imovel": d.Imovel, "telefone": d.Telefone, "canal": d.Canal,
		"check_in": d.CheckIn, "check_out": d.CheckOut,
	}
	esperado := map[string]string{
		"hospede": "Hóspede Fictício", "imovel": "Chalé 03", "telefone": "5511900000000", "canal": "airbnb",
		"check_in": "2026-09-11", "check_out": "2026-09-13",
	}
	for campo, v := range quer {
		if v == nil || *v != esperado[campo] {
			t.Errorf("%s = %v, quer %q", campo, v, esperado[campo])
		}
	}
	if d.Email != nil || d.MotivoCancelamento != nil || d.LinkPrecheckin != nil {
		t.Errorf("null e texto vazio devem virar ausente: e-mail %v, motivo %v, link %v", d.Email, d.MotivoCancelamento, d.LinkPrecheckin)
	}
	if d.Hospedes == nil || *d.Hospedes != 2 || d.Noites == nil || *d.Noites != 2 {
		t.Errorf("hóspedes %v, noites %v", d.Hospedes, d.Noites)
	}
	if d.ValorCentavos == nil || *d.ValorCentavos != 136050 {
		t.Errorf("valor = %v, quer 136050 centavos", d.ValorCentavos)
	}
	if !d.Reconhecida {
		t.Error("payload do PMS deveria ser reconhecido")
	}
}

func TestInterpretarPayloadVariacoes(t *testing.T) {
	var p any
	_ = json.Unmarshal([]byte(`{"guest":{"name":"Outro Hóspede"},"arrival_date":"2026-10-01T14:00:00Z",
		"departure_date":"2026-10-04","total_price":450,"fnrh_precheckin_link":"javascript:alert(1)","status":"Cancelado"}`), &p)
	d := interpretarPayload(p)
	if d.Hospede == nil || *d.Hospede != "Outro Hóspede" {
		t.Errorf("hóspede aninhado: %v", d.Hospede)
	}
	if d.Noites == nil || *d.Noites != 3 {
		t.Errorf("noites calculadas pelas datas: %v, quer 3", d.Noites)
	}
	if d.ValorCentavos == nil || *d.ValorCentavos != 45000 {
		t.Errorf("valor numérico: %v", d.ValorCentavos)
	}
	if d.LinkPrecheckin != nil {
		t.Errorf("link que não é http(s) não pode chegar à tela: %v", *d.LinkPrecheckin)
	}
	if !d.cancelamento {
		t.Error(`status "Cancelado" é cancelamento`)
	}

	_ = json.Unmarshal([]byte(`{"foo":"bar"}`), &p)
	if interpretarPayload(p).Reconhecida {
		t.Error("payload sem campos conhecidos marcado como reconhecido")
	}
}

func TestMontarReservasJuntaCancelamento(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	reservas := montarReservas([]eventoReserva{
		eventoFicticio(t, 1, payloadReservaFicticia, t0, "enviada"),
		eventoFicticio(t, 2, `{"guest_name":"Sem Id","property_name":"Chalé 01"}`, t0.Add(time.Hour), ""),
		eventoFicticio(t, 3, payloadCancelamentoFicticio, t0.Add(2*time.Hour), "pendente"),
	})
	if len(reservas) != 2 {
		t.Fatalf("quer 2 reservas (reserva + cancelamento viram uma), veio %d: %+v", len(reservas), reservas)
	}
	c := reservas[0]
	if c.Chave != "00000000-0000-4000-8000-000000000001" || c.Situacao != "cancelada" {
		t.Fatalf("mais recente deveria ser a cancelada: %+v", c)
	}
	if c.Mensagem != "enviada" || c.MensagemCancelamento == nil || *c.MensagemCancelamento != "pendente" {
		t.Errorf("mensagens: reserva %q, cancelamento %v", c.Mensagem, c.MensagemCancelamento)
	}
	if c.MotivoCancelamento == nil || *c.MotivoCancelamento != "Pedido do hóspede" || c.CanceladaEm == nil {
		t.Errorf("motivo %v, cancelada em %v", c.MotivoCancelamento, c.CanceladaEm)
	}
	if !c.RecebidaEm.Equal(t0) || !c.AtualizadaEm.Equal(t0.Add(2*time.Hour)) {
		t.Errorf("recebida %v, atualizada %v", c.RecebidaEm, c.AtualizadaEm)
	}
	if reservas[1].Chave != "evento:2" || reservas[1].Mensagem != "sem_mensagem" {
		t.Errorf("reserva sem id: %+v", reservas[1])
	}
}

func TestRotaReservas(t *testing.T) {
	s := novoServidorTeste(t)
	s.exec(t, "TRUNCATE checkin.mensagens, checkin.eventos RESTART IDENTITY")
	h := s.Rotas()
	criarUsuario(t, s, "operador@exemplo.com", db.PerfilUsuarioOperador)
	criarUsuario(t, s, "leitura@exemplo.com", db.PerfilUsuarioLeitura)

	s.exec(t, `INSERT INTO checkin.eventos (chave_dedup, origem, payload) VALUES ('nova-reserva:1', 'nova-reserva', $1::jsonb)`, payloadReservaFicticia)
	s.exec(t, `INSERT INTO checkin.mensagens (evento_id, destino_jid, texto, status) VALUES (1, '1234-5678@g.us', 'x', 'falhou')`)
	s.exec(t, `INSERT INTO checkin.mensagens (evento_id, destino_jid, texto, status) VALUES (1, '1234-5678@g.us', 'x', 'enviada')`)
	s.exec(t, `INSERT INTO checkin.eventos (chave_dedup, origem, payload) VALUES ('nova-reserva:1:cancelled', 'nova-reserva', $1::jsonb)`, payloadCancelamentoFicticio)

	leitura, _ := entrar(t, h, "leitura@exemplo.com", senhaTeste)
	esperarStatus(t, leitura.req(http.MethodGet, "/reservas", nil, nil), http.StatusForbidden, "perfil leitura")

	op, _ := entrar(t, h, "operador@exemplo.com", senhaTeste)
	rec := op.req(http.MethodGet, "/reservas", nil, nil)
	esperarStatus(t, rec, http.StatusOK, "operador")
	resp := decodificar[respostaReservas](t, rec)
	if len(resp.Reservas) != 1 || resp.LimiteAtingido {
		t.Fatalf("quer 1 reserva: %+v", resp)
	}
	r := resp.Reservas[0]
	if r.Situacao != "cancelada" || r.Mensagem != "enviada" || r.MensagemCancelamento == nil || *r.MensagemCancelamento != "sem_mensagem" {
		t.Fatalf("reserva montada errada (a mensagem vale a mais recente do evento): %+v", r)
	}
}
