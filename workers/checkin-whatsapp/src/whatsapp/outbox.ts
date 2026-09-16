import { claimPending, countPending, markFailure, markSent, MAX_ATTEMPTS } from '../db/outbox.js'
import { getStatus, isConnected } from './client.js'
import { sendText } from './sender.js'
import { logger } from '../logger.js'

const log = logger.child({ mod: 'outbox' })

const TICK_MS = 5_000
const BATCH_SIZE = 10

// Um aviso a cada ~1 min enquanto o WhatsApp está fora do ar. A cada tick (5s)
// inundaria o log do pm2 e esconderia o resto.
const OFFLINE_WARN_TICKS = 12

let timer: NodeJS.Timeout | undefined
/**
 * Mensagens que saíram no WhatsApp mas cujo registro no banco falhou (Postgres caiu entre o
 * envio e o UPDATE). Continuam `pendente` no banco; sem esta lista, o próximo ciclo as
 * mandaria de novo ao grupo. Cada ciclo tenta registrar estas antes de enviar qualquer outra.
 */
const enviadasSemRegistro = new Set<number>()

/** Ciclo em andamento: impede sobreposição e deixa o encerramento (e os testes) esperarem. */
let cicloAtual: Promise<void> | undefined
let offlineTicks = 0
let offlineSince: number | undefined

/**
 * Loga a fila parada de tempos em tempos. Sem isto, uma desconexão longa é
 * silenciosa: o worker apenas pula o ciclo e nada aparece em `pm2 logs`.
 */
async function reportarDesconexao() {
  offlineSince ??= Date.now()
  offlineTicks += 1
  if (offlineTicks % OFFLINE_WARN_TICKS !== 0) return

  const pending = await countPending()
  if (pending === 0) return

  log.warn(
    { pending, whatsapp: getStatus(), offlineMs: Date.now() - offlineSince },
    'WhatsApp desconectado — mensagens seguram na fila',
  )
}

async function reportarReconexao() {
  if (offlineSince === undefined) return
  const pending = await countPending()
  if (pending > 0 || offlineTicks >= OFFLINE_WARN_TICKS) {
    log.info({ pending, offlineMs: Date.now() - offlineSince }, 'conexão de volta — retomando fila')
  }
  offlineTicks = 0
  offlineSince = undefined
}

async function tick() {
  try {
    // Sem conexão não adianta tentar — o item continua pendente e não gasta tentativa.
    if (!isConnected()) {
      await reportarDesconexao()
      return
    }
    await reportarReconexao()

    for (const id of enviadasSemRegistro) {
      await markSent(id) // falhando de novo, o catch externo encerra o ciclo sem enviar nada
      enviadasSemRegistro.delete(id)
      log.info({ outboxId: id }, 'envio registrado depois da falha no banco')
    }

    const pending = await claimPending(BATCH_SIZE)
    let enviadas = 0

    for (const row of pending) {
      try {
        const messageId = await sendText(row.destino_jid, row.texto)
        try {
          await markSent(row.id)
        } catch (err) {
          enviadasSemRegistro.add(row.id)
          log.error(
            { err, outboxId: row.id, eventId: row.evento_id },
            'mensagem enviada, mas o registro no banco falhou — não será reenviada',
          )
          return
        }
        enviadas += 1
        log.info(
          { outboxId: row.id, eventId: row.evento_id, messageId, jid: row.destino_jid },
          'mensagem enviada',
        )
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        const outcome = await markFailure(row, message)
        if (outcome.retrying) {
          log.warn(
            { outboxId: row.id, eventId: row.evento_id, attempts: outcome.attempts, max: MAX_ATTEMPTS, nextAttemptAt: outcome.nextAttemptAt, err: message },
            'falha no envio, reagendado',
          )
        } else {
          // Fim da linha: ninguém mais tenta esta mensagem. Precisa de olho humano.
          log.error(
            { outboxId: row.id, eventId: row.evento_id, attempts: outcome.attempts, max: MAX_ATTEMPTS, err: message },
            'falha definitiva no envio — mensagem NÃO será entregue',
          )
        }
        // Se a conexão caiu no meio do lote, para agora e retoma no próximo tick.
        if (!isConnected()) {
          log.warn(
            { restante: pending.length - enviadas },
            'conexão caiu no meio do lote — retomando no próximo ciclo',
          )
          break
        }
      }
    }

    // Lote cheio = provavelmente há mais esperando; ajuda a explicar atraso.
    if (pending.length === BATCH_SIZE) {
      log.info({ enviadas, pending: await countPending() }, 'lote cheio — ainda há fila')
    }
  } catch (err) {
    // Inclui o Postgres fora do ar: a mensagem continua pendente e o próximo ciclo tenta de novo.
    log.error({ err }, 'erro no ciclo do outbox')
  }
}

export function startOutboxWorker() {
  if (timer) return
  timer = setInterval(() => {
    // Reentrância: um envio lento não pode sobrepor o próximo tick.
    if (cicloAtual) return
    cicloAtual = tick().finally(() => {
      cicloAtual = undefined
    })
  }, TICK_MS)
  log.info({ intervalMs: TICK_MS, batchSize: BATCH_SIZE, maxAttempts: MAX_ATTEMPTS }, 'worker do outbox iniciado')
}

/** Espera o ciclo em andamento, se houver (encerramento e testes). */
export function aguardarCiclo(): Promise<void> {
  return cicloAtual ?? Promise.resolve()
}

export async function stopOutboxWorker(): Promise<void> {
  if (!timer) return
  clearInterval(timer)
  timer = undefined
  await aguardarCiclo()
  try {
    log.info({ pending: await countPending() }, 'worker do outbox parado')
  } catch (err) {
    log.warn({ err }, 'worker do outbox parado (sem contar a fila: banco indisponível)')
  }
}
