import { timingSafeEqual } from 'node:crypto'
import type { FastifyReply, FastifyRequest } from 'fastify'
import { env } from '../config/env.js'

/** Comparação em tempo constante — não vaza o segredo pelo tempo de resposta. */
function tokenIgual(recebido: string, esperado: Buffer): boolean {
  const got = Buffer.from(recebido)
  if (got.length !== esperado.length) return false
  return timingSafeEqual(got, esperado)
}

/**
 * Único token deste serviço: ele não tem webhook. Quem chama é a API da plataforma (tela de
 * boletos, só admin), pela rede interna. **Quem tem este token lê o QR** — e com o QR pareia o
 * próprio aparelho na conta do WhatsApp. Por isso nenhuma destas rotas vai para o gateway.
 */
export const exigirTokenAdmin = (() => {
  const esperado = Buffer.from(env.ADMIN_TOKEN)
  return async function (req: FastifyRequest, reply: FastifyReply) {
    const header = req.headers['x-admin-token']
    const auth = req.headers.authorization
    const token =
      (typeof header === 'string' ? header : undefined) ??
      (auth?.startsWith('Bearer ') ? auth.slice(7) : undefined)
    if (!token || !tokenIgual(token, esperado)) {
      req.log.warn({ ip: req.ip, path: req.url }, 'token inválido ou ausente')
      return reply.code(401).send({ error: 'unauthorized' })
    }
  }
})()
