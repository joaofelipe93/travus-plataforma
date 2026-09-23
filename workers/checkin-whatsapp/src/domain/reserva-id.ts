/** Campos de id mais comuns; sem nenhum deles, cai no hash do payload. */
export const ID_FIELDS = [
  'id',
  'event_id',
  'reservation_id',
  'booking_id',
  // O provedor real (workflow do PMS) manda o id da reserva neste nome. Sem
  // ele a chave caía no SHA-256 do corpo, que inclui `_workflow_execution_id`
  // — valor que muda a cada execução, fazendo um reprocessamento da mesma
  // reserva escapar da deduplicação e duplicar a mensagem no grupo.
  'booking_uuid',
  'confirmation_code',
  'reservation_code',
  'uuid',
]

/**
 * Id da reserva no payload (o primeiro dos `ID_FIELDS`), sem o status: a reserva e o
 * cancelamento dela têm o mesmo. Base da chave de deduplicação e da linha em `checkin.reservas`.
 */
export function idReserva(payload: unknown): string | undefined {
  if (payload === null || typeof payload !== 'object') return undefined
  const record = payload as Record<string, unknown>
  for (const field of ID_FIELDS) {
    const value = record[field]
    if (typeof value === 'string' && value.trim() !== '') return value.trim()
    if (typeof value === 'number') return String(value)
  }
  return undefined
}
