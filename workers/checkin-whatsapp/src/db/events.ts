import { pool } from './index.js'

export type EventRow = {
  id: number
  chave_dedup: string
  origem: string
  payload: unknown
  recebido_em: Date
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

/** Últimos payloads recebidos — base para mapear os campos reais depois. */
export async function listRecentEvents(limit = 20): Promise<EventRow[]> {
  const { rows } = await pool.query<EventRow>(
    `SELECT * FROM checkin.eventos ORDER BY id DESC LIMIT $1`,
    [limit],
  )
  return rows
}
