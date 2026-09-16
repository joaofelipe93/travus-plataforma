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

export async function closeDb(): Promise<void> {
  await pool.end()
}
