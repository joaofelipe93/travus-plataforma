import pg from 'pg'
import { env } from '../config/env.js'
import { logger } from '../logger.js'

// Ids (bigint) e count(*) chegam como texto no pg; aqui viram number. Os ids desta fila
// nunca passam de Number.MAX_SAFE_INTEGER.
pg.types.setTypeParser(pg.types.builtins.INT8, (valor) => Number(valor))

/**
 * Postgres da plataforma, schema `checkin` (migração 00006 da API). O papel `checkin` só
 * enxerga esse schema; as consultas usam sempre o nome qualificado (`checkin.eventos`).
 */
export const pool = new pg.Pool({
  connectionString: env.DATABASE_URL,
  max: 5,
  application_name: 'checkin-whatsapp',
  // Sem isto, o processo dos testes não termina com conexões ociosas no pool.
  allowExitOnIdle: true,
})

// Conexão ociosa derrubada (Postgres reiniciando): sem este handler o erro derrubaria o
// processo. A próxima consulta abre outra conexão.
pool.on('error', (err) => {
  logger.warn({ err, mod: 'db' }, 'conexão com o Postgres perdida')
})

/** O pool ou o cliente de uma transação: as funções que entram em transação aceitam os dois. */
export type Executor = pg.Pool | pg.PoolClient

/** Número da trava: qualquer bigint fixo que a API não use (ela usa 7301, na importação). */
const TRAVA_RESUMO = 17_042_026

/**
 * Roda `fn` numa transação com a trava do resumo diário (`pg_advisory_xact_lock`). O webhook e o
 * agendador passam por ela: sem isso, uma reserva gravada enquanto o resumo é montado poderia
 * ficar fora do resumo (a consulta não a viu) e sem mensagem avulsa (o resumo ainda não constava).
 */
export async function comTravaDoResumo<T>(fn: (db: pg.PoolClient) => Promise<T>): Promise<T> {
  const client = await pool.connect()
  try {
    await client.query('BEGIN')
    await client.query('SELECT pg_advisory_xact_lock($1)', [TRAVA_RESUMO])
    const resultado = await fn(client)
    await client.query('COMMIT')
    return resultado
  } catch (err) {
    await client.query('ROLLBACK').catch(() => {})
    throw err
  } finally {
    client.release()
  }
}

export async function closeDb(): Promise<void> {
  await pool.end()
}
