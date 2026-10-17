import pg from 'pg'
import { env } from '../config/env.js'
import { logger } from '../logger.js'

// Ids (bigint) e count(*) chegam como texto no pg; aqui viram number. Os ids desta fila nunca
// passam de Number.MAX_SAFE_INTEGER.
pg.types.setTypeParser(pg.types.builtins.INT8, (valor) => Number(valor))

/**
 * Postgres da plataforma, schema `boletos` (migração 00017 da API). O papel `boletos` só
 * enxerga a fila de mensagens e a configuração: não vê clientes, cotas, parcelas nem o resto da
 * plataforma. As consultas usam sempre o nome qualificado (`boletos.mensagens`).
 *
 * Mudança de tabela é **migração nova na API**, nunca DDL aqui.
 */
export const pool = new pg.Pool({
  connectionString: env.DATABASE_URL,
  max: 5,
  application_name: 'boletos-whatsapp',
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
