import type { FastifyInstance } from 'fastify'
import { z } from 'zod'
import { definirGrupoDestino, grupoDestino } from '../db/configuracao.js'
import { listRecentEvents } from '../db/events.js'
import { stats } from '../db/outbox.js'
import { getConta, getQr, getStatus, isConnected, reiniciarSessao } from '../whatsapp/client.js'
import { listGroups, sendText, NotConnectedError } from '../whatsapp/sender.js'
import { requireAdminToken } from './auth.js'

const corpoGrupo = z.object({
  jid: z.string().regex(/^[0-9-]+@g\.us$/, 'jid deve ser de um grupo (…@g.us)'),
})

/**
 * Administração do WhatsApp. Sem rota no gateway: quem chama é a API da plataforma (tela
 * WhatsApp, só admin), pela rede interna, com o ADMIN_TOKEN.
 */
export async function whatsappRoutes(app: FastifyInstance) {
  app.addHook('preHandler', requireAdminToken)

  app.get('/whatsapp/status', async () => {
    const [destino, fila] = await Promise.all([grupoDestino(), stats()])
    return {
      status: getStatus(),
      connected: isConnected(),
      // Número pareado, com a conexão aberta.
      account: getConta() ?? null,
      // String bruta do QR (a tela desenha a imagem). Troca a cada ~20s enquanto não pareia.
      qr: getQr() ?? null,
      group: destino,
      outbox: Object.fromEntries(fila.map((s) => [s.status, s.count])),
    }
  })

  app.get('/whatsapp/groups', async (_req, reply) => {
    try {
      return { groups: await listGroups() }
    } catch (err) {
      if (err instanceof NotConnectedError) {
        return reply.code(503).send({ error: 'not_connected', status: getStatus() })
      }
      throw err
    }
  })

  // Escolhe o grupo de destino. Só aceita um grupo de que o número pareado participa (e grava
  // o nome junto, para a tela e os logs).
  app.put('/whatsapp/grupo', async (req, reply) => {
    const corpo = corpoGrupo.safeParse(req.body)
    if (!corpo.success) {
      return reply.code(400).send({ error: 'invalid_jid', message: corpo.error.issues[0]?.message })
    }
    let grupos
    try {
      grupos = await listGroups()
    } catch (err) {
      if (err instanceof NotConnectedError) {
        return reply.code(503).send({ error: 'not_connected', status: getStatus() })
      }
      throw err
    }
    const grupo = grupos.find((g) => g.jid === corpo.data.jid)
    if (!grupo) {
      return reply.code(422).send({ error: 'group_not_found' })
    }
    await definirGrupoDestino(grupo.jid, grupo.subject)
    req.log.info({ grupo: grupo.jid, nome: grupo.subject }, 'grupo de destino definido')
    return { group: await grupoDestino() }
  })

  app.post('/whatsapp/test', async (_req, reply) => {
    const destino = await grupoDestino()
    if (!destino) {
      return reply.code(400).send({ error: 'group_not_configured' })
    }
    try {
      const messageId = await sendText(
        destino.jid,
        `🔧 Teste do notificador de check-in — ${new Date().toLocaleString('pt-BR', { timeZone: 'America/Sao_Paulo' })}`,
      )
      return { status: 'sent', messageId, group: destino }
    } catch (err) {
      if (err instanceof NotConnectedError) {
        return reply.code(503).send({ error: 'not_connected', status: getStatus() })
      }
      throw err
    }
  })

  // Desvincula o aparelho, apaga a sessão e gera um QR novo. As mensagens ficam na fila.
  app.post('/whatsapp/desconectar', async (req, reply) => {
    req.log.warn({ status: getStatus() }, 'pedido para desconectar e gerar novo QR')
    await reiniciarSessao()
    return reply.code(202).send({ status: getStatus() })
  })

  // Payloads já recebidos — a base para mapear os campos reais em domain/checkin.ts.
  app.get('/events', async (req) => {
    const limit = Number((req.query as { limit?: string })?.limit ?? 20)
    return {
      events: (await listRecentEvents(Number.isFinite(limit) ? Math.min(limit, 100) : 20)).map((e) => ({
        id: e.id,
        source: e.origem,
        dedupeKey: e.chave_dedup,
        receivedAt: e.recebido_em,
        payload: e.payload,
      })),
    }
  })
}
