import type { FastifyInstance } from 'fastify'
import type { Gerente } from '../whatsapp/sessoes.js'

export function rotasHealth(gerente: Gerente) {
  return async function (app: FastifyInstance) {
    app.get('/health', async () => ({
      status: 'ok',
      servico: 'atendimento-whatsapp',
      sessoes: gerente.status().length,
      conectadas: gerente.conectados().length,
      sem_sessao: gerente.semSessao().length,
      uptime: Math.round(process.uptime()),
      versao: process.env.VERSAO ?? 'dev',
      commit: process.env.COMMIT ?? 'desconhecido',
    }))
  }
}
