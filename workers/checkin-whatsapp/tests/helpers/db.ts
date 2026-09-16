import './setup.js'
import { pool } from '../../src/db/index.js'
import type { OutboxRow } from '../../src/db/outbox.js'
import type { EventRow } from '../../src/db/events.js'

export { pool }

const url = process.env.TEST_DATABASE_URL
if (!url) {
  throw new Error('TEST_DATABASE_URL não definida: os testes do banco usam o Postgres de teste (rode make test na raiz)')
}
if (!new URL(url).pathname.includes('teste')) {
  // Os testes apagam as tabelas: nunca contra o banco de verdade.
  throw new Error('TEST_DATABASE_URL precisa apontar para um banco de teste (nome com "teste")')
}

/** Zera as tabelas entre testes. */
export async function resetDb(): Promise<void> {
  await pool.query('TRUNCATE checkin.mensagens, checkin.eventos RESTART IDENTITY')
}

/** Cria um evento direto no banco, para testes que só precisam do id. */
export async function seedEvent(dedupeKey = `evt-${Math.random()}`): Promise<number> {
  const { rows } = await pool.query<{ id: number }>(
    `INSERT INTO checkin.eventos (chave_dedup, origem, payload) VALUES ($1, 'teste', '{}') RETURNING id`,
    [dedupeKey],
  )
  return rows[0]!.id
}

export async function getOutboxRow(id: number): Promise<OutboxRow> {
  const { rows } = await pool.query<OutboxRow>('SELECT * FROM checkin.mensagens WHERE id = $1', [id])
  if (!rows[0]) throw new Error(`mensagem ${id} não encontrada`)
  return rows[0]
}

export async function getEventRow(id: number): Promise<EventRow> {
  const { rows } = await pool.query<EventRow>('SELECT * FROM checkin.eventos WHERE id = $1', [id])
  if (!rows[0]) throw new Error(`evento ${id} não encontrado`)
  return rows[0]
}

/** Força `proxima_tentativa_em` — evita esperar o backoff real no teste. */
export async function setNextAttempt(id: number, at: Date): Promise<void> {
  await pool.query('UPDATE checkin.mensagens SET proxima_tentativa_em = $2 WHERE id = $1', [id, at])
}

export async function contaMensagens(): Promise<number> {
  const { rows } = await pool.query<{ n: number }>('SELECT count(*) AS n FROM checkin.mensagens')
  return rows[0]!.n
}
