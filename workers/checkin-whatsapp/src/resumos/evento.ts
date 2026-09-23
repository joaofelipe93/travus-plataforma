import { env } from '../config/env.js'
import type { GrupoDestino } from '../db/configuracao.js'
import { marcarProcessado } from '../db/events.js'
import { comTravaDoResumo, pool, type Executor } from '../db/index.js'
import { enqueue } from '../db/outbox.js'
import { aplicarReserva, resumoJaMontado } from '../db/reservas.js'
import { isCancelamento, isUnmapped, type CheckinEvent } from '../domain/checkin.js'
import { lerData, relogioLocal } from '../domain/datas.js'
import { formatCheckinMessage } from '../domain/template.js'

/**
 * - `resumo`: a reserva foi guardada e sai no resumo diário (nenhuma mensagem agora);
 * - `avulsa`: mensagem enfileirada já (ver `tratarEvento`);
 * - `sem_destino`: precisava de mensagem avulsa mas não há grupo escolhido. Nada foi gravado
 *   além do evento, e um reenvio do webhook tenta de novo.
 */
export type ResultadoEvento =
  | { acao: 'resumo'; motivo: string }
  | { acao: 'avulsa'; motivo: string; outboxId: number }
  | { acao: 'sem_destino'; motivo: string }

class SemDestino extends Error {
  constructor(readonly motivo: string) {
    super('sem grupo de destino')
  }
}

/**
 * Trata o evento de um webhook (issue #17). A regra:
 *
 * - reserva com id e data de check-in legível entra em `checkin.reservas` e **não** gera
 *   mensagem: ela sai no resumo "para Amanhã" (véspera) e "para Hoje" (manhã do dia);
 * - chegou tarde (algum resumo daquele dia de check-in já foi montado): confirmação vira a
 *   mensagem avulsa de sempre, senão o grupo só saberia no próximo resumo ou nunca;
 * - cancelamento de uma reserva que o grupo já viu num resumo (ou avulsa): avulsa também, senão o
 *   grupo continuaria esperando o hóspede. Cancelamento de reserva que ainda não saiu em resumo
 *   só a tira do próximo;
 * - check-in já passado: só atualiza a reserva, sem mensagem;
 * - sem id, sem data legível ou formato desconhecido: não dá para agendar, vai avulsa como antes
 *   (e a linha "Formato não reconhecido" continua sendo o sinal no grupo).
 *
 * Tudo sob a trava do resumo: ou o resumo já montado conta com esta reserva, ou ela vê o resumo
 * registrado e vai avulsa. Nunca as duas coisas faltam.
 */
export async function tratarEvento(input: {
  eventId: number
  reservaId: string | undefined
  evt: CheckinEvent
  destino: GrupoDestino | null
  agora?: Date
}): Promise<ResultadoEvento> {
  const { eventId, reservaId, evt, destino } = input
  const checkIn = lerData(evt.checkIn)

  if (reservaId === undefined || checkIn === undefined || isUnmapped(evt)) {
    const motivo = isUnmapped(evt)
      ? 'formato não reconhecido'
      : reservaId === undefined
        ? 'sem id da reserva'
        : 'sem data de check-in legível'
    if (!destino) return { acao: 'sem_destino', motivo }
    const outboxId = await enqueue({ eventId, targetJid: destino.jid, body: formatCheckinMessage(evt) })
    await marcarProcessado(eventId, pool)
    return { acao: 'avulsa', motivo, outboxId }
  }

  const cancelada = isCancelamento(evt)
  const hoje = relogioLocal(input.agora ?? new Date(), env.RESUMO_FUSO).data

  try {
    return await comTravaDoResumo(async (db) => {
      const { statusAnterior, aplicada } = await aplicarReserva(db, {
        reservaId,
        status: cancelada ? 'cancelada' : 'confirmada',
        checkIn,
        checkOut: lerData(evt.checkOut),
        evt,
        eventId,
      })

      let motivo: string
      if (!aplicada) {
        return concluir(db, eventId, 'evento mais antigo que a situação gravada')
      } else if (checkIn < hoje) {
        return concluir(db, eventId, 'check-in já passou')
      } else if (!(await resumoJaMontado(db, checkIn))) {
        return concluir(db, eventId, cancelada ? 'sai do próximo resumo' : 'entra no resumo')
      } else if (cancelada && statusAnterior === 'confirmada') {
        motivo = 'cancelamento depois do resumo'
      } else if (!cancelada && statusAnterior !== 'confirmada') {
        motivo = 'reserva depois do resumo'
      } else {
        return concluir(db, eventId, 'nada mudou para o grupo')
      }

      // Lança para desfazer a reserva gravada: o reenvio do webhook refaz tudo com o grupo.
      if (!destino) throw new SemDestino(motivo)
      const outboxId = await enqueue(
        { eventId, targetJid: destino.jid, body: formatCheckinMessage(evt) },
        db,
      )
      await marcarProcessado(eventId, db)
      return { acao: 'avulsa', motivo, outboxId }
    })
  } catch (err) {
    if (err instanceof SemDestino) return { acao: 'sem_destino', motivo: err.motivo }
    throw err
  }
}

async function concluir(
  db: Executor,
  eventId: number,
  motivo: string,
): Promise<ResultadoEvento> {
  await marcarProcessado(eventId, db)
  return { acao: 'resumo', motivo }
}
