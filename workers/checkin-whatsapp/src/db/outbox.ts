import { pool } from './index.js'

export const MAX_ATTEMPTS = 6

export type OutboxStatus = 'pendente' | 'enviada' | 'falhou'

export type OutboxRow = {
  id: number
  evento_id: number
  destino_jid: string
  texto: string
  status: OutboxStatus
  tentativas: number
  proxima_tentativa_em: Date
  ultimo_erro: string | null
  enviada_em: Date | null
  criada_em: Date
}

export async function enqueue(input: {
  eventId: number
  targetJid: string
  body: string
}): Promise<number> {
  // proxima_tentativa_em = now(): elegível imediatamente.
  const { rows } = await pool.query<{ id: number }>(
    `INSERT INTO checkin.mensagens (evento_id, destino_jid, texto)
     VALUES ($1, $2, $3)
     RETURNING id`,
    [input.eventId, input.targetJid, input.body],
  )
  return rows[0]!.id
}

/** True se o evento já gerou alguma mensagem (enviada, pendente ou falha). */
export async function hasMessageForEvent(eventId: number): Promise<boolean> {
  const { rowCount } = await pool.query(
    `SELECT 1 FROM checkin.mensagens WHERE evento_id = $1 LIMIT 1`,
    [eventId],
  )
  return (rowCount ?? 0) > 0
}

/**
 * Pendentes na hora de envio, em ordem. Sem trava: existe um processo só (uma sessão do
 * Baileys), e o laço não sobrepõe ciclos.
 */
export async function claimPending(limit = 10): Promise<OutboxRow[]> {
  const { rows } = await pool.query<OutboxRow>(
    `SELECT * FROM checkin.mensagens
     WHERE status = 'pendente' AND proxima_tentativa_em <= now()
     ORDER BY id ASC
     LIMIT $1`,
    [limit],
  )
  return rows
}

export async function markSent(id: number): Promise<void> {
  await pool.query(
    `UPDATE checkin.mensagens
     SET status = 'enviada', tentativas = tentativas + 1, enviada_em = now(), ultimo_erro = NULL
     WHERE id = $1`,
    [id],
  )
}

/**
 * Registra a falha. Enquanto houver tentativas sobrando, reagenda com backoff
 * exponencial (5s, 15s, 45s, 135s...); esgotadas, marca como `falhou`.
 */
export async function markFailure(row: OutboxRow, error: string) {
  const attempts = row.tentativas + 1
  if (attempts >= MAX_ATTEMPTS) {
    await pool.query(
      `UPDATE checkin.mensagens
       SET status = 'falhou', tentativas = tentativas + 1, ultimo_erro = $2
       WHERE id = $1`,
      [row.id, error],
    )
    return { retrying: false as const, attempts }
  }

  const delayMs = 5_000 * 3 ** row.tentativas
  const { rows } = await pool.query<{ proxima_tentativa_em: Date }>(
    `UPDATE checkin.mensagens
     SET tentativas = tentativas + 1,
         proxima_tentativa_em = now() + make_interval(secs => $2::double precision / 1000),
         ultimo_erro = $3
     WHERE id = $1
     RETURNING proxima_tentativa_em`,
    [row.id, delayMs, error],
  )
  return { retrying: true as const, attempts, nextAttemptAt: rows[0]?.proxima_tentativa_em }
}

/** Quantas mensagens ainda esperam envio — usado só para log de diagnóstico. */
export async function countPending(): Promise<number> {
  const { rows } = await pool.query<{ n: number }>(
    `SELECT count(*) AS n FROM checkin.mensagens WHERE status = 'pendente'`,
  )
  return rows[0]?.n ?? 0
}

export async function stats(): Promise<{ status: OutboxStatus; count: number }[]> {
  const { rows } = await pool.query<{ status: OutboxStatus; count: number }>(
    `SELECT status, count(*) AS count FROM checkin.mensagens GROUP BY status ORDER BY status`,
  )
  return rows
}
