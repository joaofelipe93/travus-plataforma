import { pool, type Executor } from './index.js'

export type EventRow = {
  id: number
  chave_dedup: string
  origem: string
  payload: unknown
  recebido_em: Date
  processado_em: Date | null
}

/**
 * Insere o evento. Se `dedupeKey` já existir, nada é inserido e
 * `isDuplicate` volta true — o webhook então não reenfileira a mensagem.
 */
export async function insertEvent(input: {
  dedupeKey: string
  source: string
  rawPayload: string
}): Promise<{ id: number; isDuplicate: boolean }> {
  const inserido = await pool.query<{ id: number }>(
    `INSERT INTO checkin.eventos (chave_dedup, origem, payload)
     VALUES ($1, $2, $3::jsonb)
     ON CONFLICT (chave_dedup) DO NOTHING
     RETURNING id`,
    [input.dedupeKey, input.source, input.rawPayload],
  )
  if (inserido.rows[0]) {
    return { id: inserido.rows[0].id, isDuplicate: false }
  }

  const existente = await pool.query<{ id: number }>(
    `SELECT id FROM checkin.eventos WHERE chave_dedup = $1`,
    [input.dedupeKey],
  )
  return { id: existente.rows[0]?.id ?? -1, isDuplicate: true }
}

/**
 * True se o evento já foi tratado: virou mensagem avulsa ou entrou na reserva do resumo (ou,
 * antes do resumo diário, virou mensagem — esses não têm `processado_em`).
 */
export async function eventoProcessado(eventId: number): Promise<boolean> {
  const { rowCount } = await pool.query(
    `SELECT 1 FROM checkin.eventos e
     WHERE e.id = $1
       AND (e.processado_em IS NOT NULL
            OR EXISTS (SELECT 1 FROM checkin.mensagens m WHERE m.evento_id = e.id))`,
    [eventId],
  )
  return (rowCount ?? 0) > 0
}

export async function marcarProcessado(eventId: number, db: Executor = pool): Promise<void> {
  await db.query(
    `UPDATE checkin.eventos SET processado_em = now() WHERE id = $1 AND processado_em IS NULL`,
    [eventId],
  )
}

/** Últimos payloads recebidos — base para mapear os campos reais depois. */
export async function listRecentEvents(limit = 20): Promise<EventRow[]> {
  const { rows } = await pool.query<EventRow>(
    `SELECT * FROM checkin.eventos ORDER BY id DESC LIMIT $1`,
    [limit],
  )
  return rows
}
