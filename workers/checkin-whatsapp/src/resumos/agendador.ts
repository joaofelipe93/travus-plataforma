import { env } from '../config/env.js'
import { grupoDestino } from '../db/configuracao.js'
import { comTravaDoResumo } from '../db/index.js'
import { enqueue } from '../db/outbox.js'
import {
  aplicarReserva,
  contaReservas,
  registrarResumo,
  reservasConfirmadasPara,
  resumoExiste,
} from '../db/reservas.js'
import { isCancelamento, isUnmapped, normalizeCheckin } from '../domain/checkin.js'
import { lerData, relogioLocal, somarDias } from '../domain/datas.js'
import { idReserva } from '../domain/reserva-id.js'
import { formatResumo, type TipoResumo } from '../domain/template.js'
import { logger } from '../logger.js'

const log = logger.child({ mod: 'resumo' })

const TICK_MS = 60_000

let timer: NodeJS.Timeout | undefined
let cicloAtual: Promise<void> | undefined
/** Resumos que não saíram por falta de grupo: avisa uma vez, não a cada minuto. */
const avisadosSemGrupo = new Set<string>()

/**
 * Resumos que já deviam ter saído a esta hora: "para Hoje" a partir de RESUMO_HORA_HOJE e "para
 * Amanhã" a partir de RESUMO_HORA_AMANHA, até o fim do dia. Com o serviço fora do ar na hora, o
 * resumo sai quando ele voltar, no mesmo dia (depois disso a data já passou).
 */
export function resumosDevidos(agora: Date): { tipo: TipoResumo; data: string }[] {
  const { data: hoje, hora } = relogioLocal(agora, env.RESUMO_FUSO)
  const devidos: { tipo: TipoResumo; data: string }[] = []
  if (hora >= env.RESUMO_HORA_HOJE) devidos.push({ tipo: 'hoje', data: hoje })
  if (hora >= env.RESUMO_HORA_AMANHA) devidos.push({ tipo: 'amanha', data: somarDias(hoje, 1) })
  return devidos
}

/**
 * Monta e enfileira os resumos devidos que ainda não saíram. Cada resumo fica registrado em
 * `checkin.resumos` (mesmo sem reserva, e aí sem mensagem) e nunca sai duas vezes. O envio é
 * do laço da fila, como qualquer mensagem: WhatsApp fora do ar só atrasa.
 */
export async function verificarResumos(agora: Date = new Date()): Promise<void> {
  for (const { tipo, data } of resumosDevidos(agora)) {
    if (await resumoExiste(tipo, data)) continue

    const destino = await grupoDestino()
    if (!destino) {
      const chave = `${tipo}:${data}`
      if (!avisadosSemGrupo.has(chave)) {
        avisadosSemGrupo.add(chave)
        log.error({ tipo, data }, 'resumo diário sem grupo do WhatsApp escolhido — sai quando houver grupo')
      }
      continue
    }

    const resultado = await comTravaDoResumo(async (db) => {
      const reservas = await reservasConfirmadasPara(db, data)
      const resumoId = await registrarResumo(db, tipo, data, reservas.length)
      if (resumoId === null || reservas.length === 0) return { resumoId, reservas: reservas.length }
      const outboxId = await enqueue(
        { resumoId, targetJid: destino.jid, body: formatResumo(tipo, reservas) },
        db,
      )
      return { resumoId, reservas: reservas.length, outboxId }
    })
    if (resultado.resumoId === null) continue
    log.info({ tipo, data, ...resultado }, resultado.reservas ? 'resumo diário enfileirado' : 'resumo diário sem reservas — nada a enviar')
  }
}

/**
 * Primeira subida com o resumo diário: `checkin.reservas` nasce vazia e as reservas já recebidas
 * (antes desta versão) ficariam fora dos resumos. Preenche a partir de `checkin.eventos`, em
 * ordem, sem mandar nada. Não marca os eventos como processados: esses já viraram mensagem.
 * Roda no boot, antes do HTTP aceitar webhook: com uma reserva nova na tabela, não preencheria.
 */
export async function preencherReservas(): Promise<void> {
  if ((await contaReservas()) > 0) return
  // Numa transação só: parar no meio deixaria a tabela pela metade, e a próxima subida (que só
  // preenche tabela vazia) não completaria.
  const { eventos, aplicadas } = await comTravaDoResumo(async (db) => {
    const { rows } = await db.query<{ id: number; payload: unknown }>(
      `SELECT id, payload FROM checkin.eventos ORDER BY id`,
    )
    let aplicadas = 0
    for (const { id, payload } of rows) {
      const evt = normalizeCheckin(payload)
      const reservaId = idReserva(payload)
      const checkIn = lerData(evt.checkIn)
      if (reservaId === undefined || checkIn === undefined || isUnmapped(evt)) continue
      await aplicarReserva(db, {
        reservaId,
        status: isCancelamento(evt) ? 'cancelada' : 'confirmada',
        checkIn,
        checkOut: lerData(evt.checkOut),
        evt,
        eventId: id,
      })
      aplicadas += 1
    }
    return { eventos: rows.length, aplicadas }
  })
  if (aplicadas > 0) log.info({ eventos, aplicadas }, 'reservas preenchidas com os eventos já recebidos')
}

async function ciclo() {
  try {
    await verificarResumos()
  } catch (err) {
    // Postgres fora do ar, por exemplo: o próximo ciclo tenta de novo.
    log.error({ err }, 'erro no ciclo do resumo diário')
  }
}

export function startResumoDiario() {
  if (timer) return
  const disparar = () => {
    if (cicloAtual) return
    cicloAtual = ciclo().finally(() => {
      cicloAtual = undefined
    })
  }
  // O de hoje pode estar devido já na subida.
  disparar()
  timer = setInterval(disparar, TICK_MS)
  log.info(
    { fuso: env.RESUMO_FUSO, horaHoje: env.RESUMO_HORA_HOJE, horaAmanha: env.RESUMO_HORA_AMANHA },
    'resumo diário ligado',
  )
}

export async function stopResumoDiario(): Promise<void> {
  if (!timer) return
  clearInterval(timer)
  timer = undefined
  await cicloAtual
}
