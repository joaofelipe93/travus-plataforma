import { env } from '../config/env.js'
import { pool } from './index.js'

export type GrupoDestino = {
  jid: string
  /** Nome do grupo quando escolhido pela tela; null quando vem da variável de ambiente. */
  nome: string | null
  origem: 'tela' | 'ambiente'
}

/**
 * Grupo que recebe as notificações: o escolhido pelo admin na tela (checkin.configuracao)
 * ou, enquanto ninguém escolheu, o WHATSAPP_GROUP_JID do ambiente. Lido a cada uso: trocar
 * o grupo na tela vale para o próximo webhook, sem reiniciar.
 */
export async function grupoDestino(): Promise<GrupoDestino | null> {
  const { rows } = await pool.query<{ grupo_jid: string; grupo_nome: string }>(
    `SELECT grupo_jid, grupo_nome FROM checkin.configuracao WHERE id`,
  )
  if (rows[0]) {
    return { jid: rows[0].grupo_jid, nome: rows[0].grupo_nome, origem: 'tela' }
  }
  if (env.WHATSAPP_GROUP_JID) {
    return { jid: env.WHATSAPP_GROUP_JID, nome: null, origem: 'ambiente' }
  }
  return null
}

export async function definirGrupoDestino(jid: string, nome: string): Promise<void> {
  await pool.query(
    `INSERT INTO checkin.configuracao (id, grupo_jid, grupo_nome) VALUES (true, $1, $2)
     ON CONFLICT (id) DO UPDATE
       SET grupo_jid = excluded.grupo_jid, grupo_nome = excluded.grupo_nome, atualizado_em = now()`,
    [jid, nome],
  )
}
