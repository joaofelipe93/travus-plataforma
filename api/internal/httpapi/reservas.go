package httpapi

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Tela Reservas (admin e operador): as reservas que chegaram pelo webhook do PMS, lidas de
// checkin.eventos. O payload é guardado cru pelo notificador; aqui ele é interpretado com a
// mesma busca tolerante de workers/checkin-whatsapp/src/domain/checkin.ts (vários nomes para
// o mesmo campo) e a reserva e o cancelamento dela, que chegam como dois eventos com o mesmo
// id, viram um cartão só. A tela consulta a cada 15 s.

// Eventos lidos por consulta: sobra para anos de reservas de uma pousada. Passando disso, a
// resposta avisa (limite_atingido) e a tela mostra só as mais recentes.
const limiteEventosReserva = 3000

type reservaTela struct {
	// Id da reserva no PMS (booking_uuid…) ou "evento:<id>" quando o payload não traz id.
	Chave              string     `json:"chave"`
	Situacao           string     `json:"situacao"` // confirmada | cancelada
	Status             *string    `json:"status"`   // como veio do PMS
	Hospede            *string    `json:"hospede"`
	Telefone           *string    `json:"telefone"`
	Email              *string    `json:"email"`
	Imovel             *string    `json:"imovel"`
	Canal              *string    `json:"canal"`
	CheckIn            *string    `json:"check_in"`  // AAAA-MM-DD
	CheckOut           *string    `json:"check_out"` // AAAA-MM-DD
	Noites             *int       `json:"noites"`
	Hospedes           *int       `json:"hospedes"`
	ValorCentavos      *int64     `json:"valor_centavos"`
	MotivoCancelamento *string    `json:"motivo_cancelamento"`
	LinkPrecheckin     *string    `json:"link_precheckin"`
	RecebidaEm         time.Time  `json:"recebida_em"`
	AtualizadaEm       time.Time  `json:"atualizada_em"`
	CanceladaEm        *time.Time `json:"cancelada_em"`
	// Aviso no grupo: enviada | pendente | falhou | sem_mensagem.
	Mensagem             string  `json:"mensagem"`
	MensagemCancelamento *string `json:"mensagem_cancelamento"`
	// false quando nenhum campo conhecido veio no payload (formato novo do PMS).
	Reconhecida bool `json:"reconhecida"`
}

type respostaReservas struct {
	Reservas       []reservaTela `json:"reservas"`
	LimiteAtingido bool          `json:"limite_atingido"`
}

func (s *Servidor) listarReservas(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	eventos, err := s.q.ListarEventosReserva(r.Context(), limiteEventosReserva)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	// A consulta vem do mais novo para o mais antigo; a montagem precisa da ordem de chegada.
	lidos := make([]eventoReserva, 0, len(eventos))
	for _, e := range slices.Backward(eventos) {
		var payload any
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			payload = nil
		}
		lidos = append(lidos, eventoReserva{ID: e.ID, Payload: payload, RecebidoEm: e.RecebidoEm, Mensagem: e.MensagemStatus})
	}
	responderJSON(w, http.StatusOK, respostaReservas{
		Reservas:       montarReservas(lidos),
		LimiteAtingido: len(eventos) >= limiteEventosReserva,
	})
}

type eventoReserva struct {
	ID         int64
	Payload    any
	RecebidoEm time.Time
	Mensagem   string
}

// Mesma ordem de ID_FIELDS em workers/checkin-whatsapp/src/routes/webhook.ts: é o id que o
// notificador usa para deduplicar, e o cancelamento chega com o mesmo valor da reserva.
var camposIDReserva = []string{"id", "event_id", "reservation_id", "booking_id", "booking_uuid", "confirmation_code", "reservation_code", "uuid"}

// Os nomes abaixo espelham normalizeCheckin (domain/checkin.ts), mais os campos que só a tela
// usa (e-mail, noites, valor, link do pré-check-in).
var (
	camposHospede   = []string{"guest.name", "guest.full_name", "guest_name", "hospede.nome", "hospede", "cliente.nome", "customer.name", "name", "nome"}
	camposImovel    = []string{"listing.name", "listing.title", "listing_name", "property_name", "property.name", "property", "imovel.nome", "imovel", "acomodacao", "unit.name"}
	camposCheckIn   = []string{"check_in", "checkIn", "checkin", "check_in_date", "checkin_date", "data_checkin", "arrival_date", "start_date", "dates.start"}
	camposCheckOut  = []string{"check_out", "checkOut", "checkout", "check_out_date", "checkout_date", "data_checkout", "departure_date", "end_date", "dates.end"}
	camposHospedes  = []string{"guests", "guests_count", "number_of_guests", "num_hospedes", "hospedes", "adults", "pax"}
	camposTelefone  = []string{"guest_phone", "guest.phone", "phone", "telefone", "celular", "hospede.telefone"}
	camposEmail     = []string{"guest_email", "guest.email", "email", "hospede.email"}
	camposCanal     = []string{"channel", "source", "platform", "canal", "origem", "listing.channel"}
	camposStatus    = []string{"status", "booking_status", "reservation_status", "situacao"}
	camposMotivo    = []string{"cancellation_reason", "cancelation_reason", "motivo_cancelamento"}
	camposNoites    = []string{"nights", "noites", "number_of_nights"}
	camposValor     = []string{"total_price", "price", "total", "valor_total", "valor"}
	camposPrecheck  = []string{"fnrh_precheckin_link", "precheckin_link", "link_precheckin"}
	reDataBR        = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{4})$`)
	reDataISO       = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})`)
	diaSemHorario   = "2006-01-02"
	situacaoCancela = "cancelada"
)

// montarReservas junta os eventos (em ordem de chegada) por reserva: o evento mais recente
// que não é cancelamento dá os dados (uma alteração de reserva sobrescreve), e o
// cancelamento marca a reserva. Volta da mais recente para a mais antiga.
func montarReservas(eventos []eventoReserva) []reservaTela {
	porChave := map[string]*reservaTela{}
	var ordem []string
	for _, e := range eventos {
		chave := chaveReserva(e)
		atual, existe := porChave[chave]
		if !existe {
			atual = &reservaTela{Chave: chave, Situacao: "confirmada", RecebidaEm: e.RecebidoEm, Mensagem: "sem_mensagem"}
			porChave[chave] = atual
			ordem = append(ordem, chave)
		}
		atual.AtualizadaEm = e.RecebidoEm
		mensagem := e.Mensagem
		if mensagem == "" {
			mensagem = "sem_mensagem"
		}

		dados := interpretarPayload(e.Payload)
		if dados.cancelamento {
			quando := e.RecebidoEm
			atual.Situacao = situacaoCancela
			atual.CanceladaEm = &quando
			atual.MensagemCancelamento = &mensagem
			atual.MotivoCancelamento = dados.MotivoCancelamento
			atual.Status = dados.Status
			// O cancelamento repete os dados da reserva: preenche só o que faltava (reserva
			// que chegou antes de o PMS mandar o evento de criação, por exemplo).
			completar(atual, dados.reservaTela)
			continue
		}
		atual.Situacao = "confirmada"
		atual.CanceladaEm, atual.MensagemCancelamento, atual.MotivoCancelamento = nil, nil, nil
		atual.Mensagem = mensagem
		sobrescrever(atual, dados.reservaTela)
	}

	reservas := make([]reservaTela, 0, len(ordem))
	for _, chave := range ordem {
		reservas = append(reservas, *porChave[chave])
	}
	sort.SliceStable(reservas, func(i, j int) bool { return reservas[i].AtualizadaEm.After(reservas[j].AtualizadaEm) })
	return reservas
}

type payloadInterpretado struct {
	reservaTela
	cancelamento bool
}

func interpretarPayload(p any) payloadInterpretado {
	var d payloadInterpretado
	d.Hospede = pegarTexto(p, camposHospede)
	d.Imovel = pegarTexto(p, camposImovel)
	d.CheckIn = pegarData(p, camposCheckIn)
	d.CheckOut = pegarData(p, camposCheckOut)
	d.Hospedes = pegarInteiro(p, camposHospedes)
	d.Telefone = pegarTexto(p, camposTelefone)
	d.Email = pegarTexto(p, camposEmail)
	if d.Canal = pegarTexto(p, camposCanal); d.Canal != nil {
		canal := strings.ToLower(*d.Canal)
		d.Canal = &canal
	}
	d.Status = pegarTexto(p, camposStatus)
	d.MotivoCancelamento = pegarTexto(p, camposMotivo)
	d.LinkPrecheckin = pegarLink(p, camposPrecheck)
	d.ValorCentavos = pegarValor(p, camposValor)
	d.Noites = pegarInteiro(p, camposNoites)
	if d.Noites == nil && d.CheckIn != nil && d.CheckOut != nil {
		entrada, err1 := time.Parse(diaSemHorario, *d.CheckIn)
		saida, err2 := time.Parse(diaSemHorario, *d.CheckOut)
		if err1 == nil && err2 == nil && saida.After(entrada) {
			n := int(saida.Sub(entrada).Hours() / 24)
			d.Noites = &n
		}
	}
	d.Reconhecida = d.Hospede != nil || d.Imovel != nil || d.CheckIn != nil || d.CheckOut != nil
	// Igual a isCancelamento do notificador: status começando por "cancel" ou motivo preenchido.
	d.cancelamento = (d.Status != nil && strings.HasPrefix(strings.ToLower(*d.Status), "cancel")) || d.MotivoCancelamento != nil
	return d
}

func chaveReserva(e eventoReserva) string {
	if obj, ok := e.Payload.(map[string]any); ok {
		for _, campo := range camposIDReserva {
			switch v := obj[campo].(type) {
			case string:
				if t := strings.TrimSpace(v); t != "" {
					return t
				}
			case float64:
				return strconv.FormatFloat(v, 'f', -1, 64)
			}
		}
	}
	return "evento:" + strconv.FormatInt(e.ID, 10)
}

func sobrescrever(dst *reservaTela, src reservaTela) {
	dst.Status = src.Status
	dst.Reconhecida = src.Reconhecida
	dst.Hospede, dst.Telefone, dst.Email = src.Hospede, src.Telefone, src.Email
	dst.Imovel, dst.Canal = src.Imovel, src.Canal
	dst.CheckIn, dst.CheckOut, dst.Noites, dst.Hospedes = src.CheckIn, src.CheckOut, src.Noites, src.Hospedes
	dst.ValorCentavos, dst.LinkPrecheckin = src.ValorCentavos, src.LinkPrecheckin
}

func completar(dst *reservaTela, src reservaTela) {
	dst.Reconhecida = dst.Reconhecida || src.Reconhecida
	primeiro := func(a, b *string) *string {
		if a != nil {
			return a
		}
		return b
	}
	dst.Hospede, dst.Telefone, dst.Email = primeiro(dst.Hospede, src.Hospede), primeiro(dst.Telefone, src.Telefone), primeiro(dst.Email, src.Email)
	dst.Imovel, dst.Canal = primeiro(dst.Imovel, src.Imovel), primeiro(dst.Canal, src.Canal)
	dst.CheckIn, dst.CheckOut = primeiro(dst.CheckIn, src.CheckIn), primeiro(dst.CheckOut, src.CheckOut)
	if dst.Noites == nil {
		dst.Noites = src.Noites
	}
	if dst.Hospedes == nil {
		dst.Hospedes = src.Hospedes
	}
	if dst.ValorCentavos == nil {
		dst.ValorCentavos = src.ValorCentavos
	}
}

// valorNoCaminho lê "guest.name" sem estourar em nós ausentes.
func valorNoCaminho(obj any, caminho string) any {
	atual := obj
	for chave := range strings.SplitSeq(caminho, ".") {
		m, ok := atual.(map[string]any)
		if !ok {
			return nil
		}
		atual = m[chave]
	}
	return atual
}

// pegarTexto: primeiro caminho com texto não vazio (ou número).
func pegarTexto(obj any, caminhos []string) *string {
	for _, c := range caminhos {
		switch v := valorNoCaminho(obj, c).(type) {
		case string:
			if t := strings.TrimSpace(v); t != "" {
				return &t
			}
		case float64:
			t := strconv.FormatFloat(v, 'f', -1, 64)
			return &t
		}
	}
	return nil
}

// pegarInteiro: o PMS manda números como texto ("guests": "2").
func pegarInteiro(obj any, caminhos []string) *int {
	for _, c := range caminhos {
		var f float64
		switch v := valorNoCaminho(obj, c).(type) {
		case float64:
			f = v
		case string:
			n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				continue
			}
			f = n
		default:
			continue
		}
		if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 100000 {
			continue
		}
		n := int(math.Round(f))
		return &n
	}
	return nil
}

// pegarData aceita dd/mm/aaaa (o formato do PMS) e AAAA-MM-DD (com ou sem horário).
func pegarData(obj any, caminhos []string) *string {
	t := pegarTexto(obj, caminhos)
	if t == nil {
		return nil
	}
	var texto string
	if m := reDataBR.FindStringSubmatch(*t); m != nil {
		dia, _ := strconv.Atoi(m[1])
		mes, _ := strconv.Atoi(m[2])
		texto = m[3] + "-" + dois(mes) + "-" + dois(dia)
	} else if m := reDataISO.FindStringSubmatch(*t); m != nil {
		texto = m[1] + "-" + m[2] + "-" + m[3]
	} else {
		return nil
	}
	if _, err := time.Parse(diaSemHorario, texto); err != nil {
		return nil
	}
	return &texto
}

func dois(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// pegarValor: "360,00", "1.234,56", "R$ 360" ou número → centavos.
func pegarValor(obj any, caminhos []string) *int64 {
	for _, c := range caminhos {
		var f float64
		switch v := valorNoCaminho(obj, c).(type) {
		case float64:
			f = v
		case string:
			t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "R$"))
			if strings.Contains(t, ",") {
				t = strings.ReplaceAll(t, ".", "")
				t = strings.Replace(t, ",", ".", 1)
			}
			n, err := strconv.ParseFloat(t, 64)
			if err != nil {
				continue
			}
			f = n
		default:
			continue
		}
		if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
			continue
		}
		centavos := int64(math.Round(f * 100))
		return &centavos
	}
	return nil
}

// pegarLink: só http(s), para a tela poder abrir sem virar javascript: vindo do payload.
func pegarLink(obj any, caminhos []string) *string {
	t := pegarTexto(obj, caminhos)
	if t == nil {
		return nil
	}
	if l := strings.ToLower(*t); !strings.HasPrefix(l, "https://") && !strings.HasPrefix(l, "http://") {
		return nil
	}
	return t
}
