/**
 * ⭐ PONTO ÚNICO DE REFATORAÇÃO.
 *
 * Enquanto o payload real do provedor não é conhecido, este módulo faz uma
 * extração TOLERANTE: procura cada campo em vários nomes/aninhamentos comuns e
 * aceita não encontrar nada. O payload cru fica sempre preservado em
 * `CheckinEvent.raw` e na tabela `events`.
 *
 * Quando o formato real estiver definido, troque `normalizeCheckin` por um
 * parse estrito (zod) usando os payloads já gravados em `events` como fixtures.
 * Nada fora deste arquivo precisa mudar.
 */

export type CheckinEvent = {
  hospede?: string
  imovel?: string
  checkIn?: string
  checkOut?: string
  hospedes?: number
  telefone?: string
  codigo?: string
  canal?: string
  /** `confirmed`, `cancelled`… Define se a mensagem é de reserva ou cancelamento. */
  status?: string
  /** Preenchido pelo provedor quando a reserva é cancelada. */
  motivoCancelamento?: string
  /** Nome do evento/gatilho, quando o provedor manda um (`event`, `event_type`…). */
  evento?: string
  /** Quando o check-in foi efetuado, se o provedor informar. */
  checkinEm?: string
  /** Payload original, sempre preservado. */
  raw: unknown
}

/** Lê um caminho aninhado ('guest.name') sem estourar em nós ausentes. */
function get(obj: unknown, path: string): unknown {
  let current = obj
  for (const key of path.split('.')) {
    if (current === null || typeof current !== 'object') return undefined
    current = (current as Record<string, unknown>)[key]
  }
  return current
}

/** Primeiro caminho que resolve para uma string não vazia. */
function pickString(obj: unknown, paths: string[]): string | undefined {
  for (const path of paths) {
    const value = get(obj, path)
    if (typeof value === 'string' && value.trim() !== '') return value.trim()
    if (typeof value === 'number') return String(value)
  }
  return undefined
}

/** Primeiro caminho que resolve para um número finito. */
function pickNumber(obj: unknown, paths: string[]): number | undefined {
  for (const path of paths) {
    const value = get(obj, path)
    if (typeof value === 'number' && Number.isFinite(value)) return value
    if (typeof value === 'string' && value.trim() !== '') {
      const parsed = Number(value)
      if (Number.isFinite(parsed)) return parsed
    }
  }
  return undefined
}

export function normalizeCheckin(raw: unknown): CheckinEvent {
  return {
    hospede: pickString(raw, [
      'guest.name',
      'guest.full_name',
      'guest_name',
      'hospede.nome',
      'hospede',
      'cliente.nome',
      'customer.name',
      'name',
      'nome',
    ]),
    imovel: pickString(raw, [
      'listing.name',
      'listing.title',
      'listing_name',
      'property_name',
      'property.name',
      'property',
      'imovel.nome',
      'imovel',
      'acomodacao',
      'unit.name',
    ]),
    checkIn: pickString(raw, [
      'check_in',
      'checkIn',
      'checkin',
      'check_in_date',
      'checkin_date',
      'data_checkin',
      'arrival_date',
      'start_date',
      'dates.start',
    ]),
    checkOut: pickString(raw, [
      'check_out',
      'checkOut',
      'checkout',
      'check_out_date',
      'checkout_date',
      'data_checkout',
      'departure_date',
      'end_date',
      'dates.end',
    ]),
    hospedes: pickNumber(raw, [
      'guests',
      'guests_count',
      'number_of_guests',
      'num_hospedes',
      'hospedes',
      'adults',
      'pax',
    ]),
    telefone: pickString(raw, [
      'guest_phone',
      'guest.phone',
      'phone',
      'telefone',
      'celular',
      'hospede.telefone',
    ]),
    codigo: pickString(raw, [
      'confirmation_code',
      'reservation_code',
      'codigo',
      'code',
      'reservation_id',
      'booking_id',
      'booking_uuid',
      'id',
    ]),
    canal: pickString(raw, [
      'channel',
      'source',
      'platform',
      'canal',
      'origem',
      'listing.channel',
    ]),
    status: pickString(raw, ['status', 'booking_status', 'reservation_status', 'situacao']),
    motivoCancelamento: pickString(raw, [
      'cancellation_reason',
      'cancelation_reason',
      'motivo_cancelamento',
    ]),
    evento: pickString(raw, ['event', 'event_type', 'evento', 'trigger', 'action', 'tipo']),
    checkinEm: pickString(raw, [
      'checked_in_at',
      'checkin_at',
      'check_in_at',
      'actual_check_in',
      'checkin_realizado_em',
    ]),
    raw,
  }
}

/**
 * True quando o evento é um cancelamento.
 *
 * O provedor manda o mesmo webhook para reserva e cancelamento, mudando o
 * `status`. Casa por prefixo para cobrir `cancelled`, `canceled`, `cancelado` e
 * `cancelada` — a grafia varia entre canais. Um `cancellation_reason`
 * preenchido também conta: se o motivo veio, houve cancelamento, mesmo que o
 * status chegue num valor que não conhecemos.
 */
export function isCancelamento(evt: CheckinEvent): boolean {
  if (evt.status?.trim().toLowerCase().startsWith('cancel')) return true
  return (evt.motivoCancelamento ?? '').trim() !== ''
}

/**
 * True quando o evento é um **check-in efetuado** (o hóspede chegou), não uma reserva nova.
 *
 * O PMS manda isso para a mesma rota (automação criada em 30/09/2026), então o reconhecimento é
 * tolerante como o resto do módulo: o nome do evento, um status de "hospedado" ou a hora do
 * check-in. Cancelamento tem precedência: um payload que cancela nunca é check-in.
 *
 * Se o payload real usar outro nome, é aqui que se acrescenta — e o vigia da API avisa quando
 * chega evento que não viramos nada.
 */
const STATUS_CHECKIN = ['checked_in', 'checkedin', 'checkin', 'check_in', 'in_house', 'inhouse', 'hospedado']

export function isCheckinRealizado(evt: CheckinEvent): boolean {
  if (isCancelamento(evt)) return false
  const evento = evt.evento?.trim().toLowerCase().replace(/[\s-]/g, '_')
  if (evento !== undefined && /check_?in/.test(evento) && !/out/.test(evento)) return true
  const status = evt.status?.trim().toLowerCase().replace(/[\s-]/g, '_')
  if (status !== undefined && STATUS_CHECKIN.includes(status)) return true
  return (evt.checkinEm ?? '').trim() !== ''
}

/** True quando nenhum campo conhecido foi reconhecido no payload. */
export function isUnmapped(evt: CheckinEvent): boolean {
  return (
    evt.hospede === undefined &&
    evt.imovel === undefined &&
    evt.checkIn === undefined &&
    evt.checkOut === undefined
  )
}
