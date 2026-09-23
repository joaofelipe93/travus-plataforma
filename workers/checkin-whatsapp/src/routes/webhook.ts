import { createHash } from 'node:crypto'
import type { FastifyInstance } from 'fastify'
import { grupoDestino } from '../db/configuracao.js'
import { eventoProcessado, insertEvent } from '../db/events.js'
import { normalizeCheckin, isUnmapped } from '../domain/checkin.js'
import { idReserva } from '../domain/reserva-id.js'
import { tratarEvento } from '../resumos/evento.js'
import { requireToken } from './auth.js'

const SOURCE = 'nova-reserva'

/**
 * Sufixo de status para a chave de deduplicação.
 *
 * O cancelamento chega com o MESMO id da reserva original. Sem isto, a segunda
 * mensagem seria descartada como duplicata e o grupo nunca saberia do
 * cancelamento. `confirmed` (e a ausência de status) não recebem sufixo, para
 * não invalidar as chaves já gravadas das reservas existentes.
 */
function statusSuffix(payload: unknown): string {
  if (payload === null || typeof payload !== 'object') return ''
  const status = (payload as Record<string, unknown>)['status']
  if (typeof status !== 'string') return ''
  const normalizado = status.trim().toLowerCase()
  if (normalizado === '' || normalizado === 'confirmed') return ''
  return `:${normalizado}`
}

function resolveDedupeKey(payload: unknown, rawJson: string): string {
  if (payload !== null && typeof payload === 'object') {
    const id = idReserva(payload)
    if (id !== undefined) return `${SOURCE}:${id}${statusSuffix(payload)}`
  }
  // O hash já cobre o corpo inteiro, status incluso — não precisa de sufixo.
  return `${SOURCE}:sha256:${createHash('sha256').update(rawJson).digest('hex')}`
}

export async function webhookRoutes(app: FastifyInstance) {
  app.post(
    '/webhooks/nova-reserva',
    { preHandler: requireToken },
    async (req, reply) => {
      const payload = req.body ?? {}
      const rawJson = JSON.stringify(payload)

      // Responder rápido é o ponto: o provedor não deve esperar o WhatsApp.
      const dedupeKey = resolveDedupeKey(payload, rawJson)
      const event = await insertEvent({ dedupeKey, source: SOURCE, rawPayload: rawJson })

      // Um evento já visto só é ignorado se de fato foi tratado (virou mensagem ou entrou na
      // reserva do resumo). Eventos gravados antes do grupo existir (`stored_no_target`)
      // ficariam presos como duplicados para sempre — aqui eles são recuperados no reenvio.
      if (event.isDuplicate && (await eventoProcessado(event.id))) {
        req.log.info({ dedupeKey, eventId: event.id }, 'evento duplicado, ignorado')
        return reply.code(200).send({ status: 'duplicate', eventId: event.id })
      }

      const evt = normalizeCheckin(payload)
      const resultado = await tratarEvento({
        eventId: event.id,
        reservaId: idReserva(payload),
        evt,
        destino: await grupoDestino(),
      })

      // Nenhum campo reconhecido significa mensagem crua no grupo — o sinal de
      // que `domain/checkin.ts` precisa ser ajustado ao payload real.
      if (isUnmapped(evt)) {
        req.log.warn(
          { eventId: event.id, dedupeKey, campos: Object.keys(payload as object) },
          'payload sem campos reconhecidos — ajuste normalizeCheckin (veja GET /events)',
        )
      }

      if (resultado.acao === 'sem_destino') {
        // O evento fica gravado; sem destino não há o que enfileirar.
        req.log.error(
          { eventId: event.id, motivo: resultado.motivo },
          'nenhum grupo do WhatsApp escolhido — evento salvo sem envio',
        )
        return reply.code(200).send({
          status: 'stored_no_target',
          eventId: event.id,
          hint: 'escolha o grupo na tela WhatsApp da plataforma',
        })
      }

      if (resultado.acao === 'resumo') {
        req.log.info(
          { eventId: event.id, dedupeKey, motivo: resultado.motivo },
          'reserva guardada para o resumo diário',
        )
        return reply.code(200).send({ status: 'scheduled', eventId: event.id })
      }

      req.log.info(
        { eventId: event.id, outboxId: resultado.outboxId, dedupeKey, motivo: resultado.motivo, bytes: rawJson.length },
        'evento enfileirado',
      )
      return reply.code(200).send({ status: 'queued', eventId: event.id, outboxId: resultado.outboxId })
    },
  )
}
