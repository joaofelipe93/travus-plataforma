import { timingSafeEqual } from 'node:crypto'
import type { FastifyReply, FastifyRequest } from 'fastify'
import { env } from '../config/env.js'

/** Comparação em tempo constante — não vaza o segredo pelo tempo de resposta. */
function tokenMatches(received: string, expected: Buffer): boolean {
  const got = Buffer.from(received)
  if (got.length !== expected.length) return false
  return timingSafeEqual(got, expected)
}

/**
 * Exige `x-webhook-token` (ou `Authorization: Bearer <token>`, caso o provedor
 * do webhook só ofereça esse formato) igual a `segredo`.
 */
function exigirToken(segredo: string) {
  const expected = Buffer.from(segredo)
  return async function (req: FastifyRequest, reply: FastifyReply) {
    const header = req.headers['x-webhook-token']
    const auth = req.headers.authorization

    const token =
      (typeof header === 'string' ? header : undefined) ??
      (auth?.startsWith('Bearer ') ? auth.slice(7) : undefined)

    if (!token || !tokenMatches(token, expected)) {
      req.log.warn({ ip: req.ip, path: req.url }, 'token inválido ou ausente')
      return reply.code(401).send({ error: 'unauthorized' })
    }
  }
}

/** Webhook do PMS: o token que o provedor conhece. */
export const requireToken = exigirToken(env.WEBHOOK_SECRET)

/**
 * Rotas de administração (/whatsapp/*, /events): só a API da plataforma, com outro token.
 * Quem tem o token do webhook (o provedor) não lê o QR nem os payloads.
 */
export const requireAdminToken = exigirToken(env.ADMIN_TOKEN)
