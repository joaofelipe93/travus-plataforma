import type { FastifyInstance } from 'fastify'
import { getStatus } from '../whatsapp/client.js'
import { situacaoDaFila } from '../db/fila.js'

export async function rotasHealth(app: FastifyInstance) {
  app.get('/health', async () => ({
    status: 'ok',
    servico: 'boletos-whatsapp',
    whatsapp: getStatus(),
    fila: Object.fromEntries((await situacaoDaFila()).map((s) => [s.status, s.count])),
    uptime: Math.round(process.uptime()),
    versao: process.env.VERSAO ?? 'dev',
    commit: process.env.COMMIT ?? 'desconhecido',
  }))
}
