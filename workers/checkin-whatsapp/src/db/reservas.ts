import type { CheckinEvent } from '../domain/checkin.js'
import type { TipoResumo } from '../domain/template.js'
import { pool, type Executor } from './index.js'

export type StatusReserva = 'confirmada' | 'cancelada'

export type ReservaAplicada = {
  /** Status antes deste evento; null se a reserva é nova. */
  statusAnterior: StatusReserva | null
  /** False quando um evento mais novo da mesma reserva já tinha sido aplicado. */
  aplicada: boolean
}

/**
 * Grava a situação da reserva vinda de um evento. Um evento mais antigo que o último aplicado
 * (reprocessamento fora de ordem) não sobrescreve: o cancelamento não volta a ser confirmação.
 */
export async function aplicarReserva(
  db: Executor,
  input: {
    reservaId: string
    status: StatusReserva
    checkIn: string
    checkOut?: string
    evt: CheckinEvent
    eventId: number
  },
): Promise<ReservaAplicada> {
  const anterior = await db.query<{ status: StatusReserva }>(
    `SELECT status FROM checkin.reservas WHERE reserva_id = $1`,
    [input.reservaId],
  )
  const { rowCount } = await db.query(
    `INSERT INTO checkin.reservas
       (reserva_id, status, check_in, check_out, imovel, hospede, hospedes, canal, telefone, ultimo_evento_id)
     VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
     ON CONFLICT (reserva_id) DO UPDATE
       SET status = excluded.status, check_in = excluded.check_in, check_out = excluded.check_out,
           imovel = excluded.imovel, hospede = excluded.hospede, hospedes = excluded.hospedes,
           canal = excluded.canal, telefone = excluded.telefone,
           ultimo_evento_id = excluded.ultimo_evento_id, atualizada_em = now()
       WHERE checkin.reservas.ultimo_evento_id < excluded.ultimo_evento_id`,
    [
      input.reservaId,
      input.status,
      input.checkIn,
      input.checkOut ?? null,
      input.evt.imovel ?? null,
      input.evt.hospede ?? null,
      input.evt.hospedes === undefined ? null : Math.trunc(input.evt.hospedes),
      input.evt.canal ?? null,
      input.evt.telefone ?? null,
      input.eventId,
    ],
  )
  return { statusAnterior: anterior.rows[0]?.status ?? null, aplicada: (rowCount ?? 0) > 0 }
}

/** True se algum resumo (de hoje ou da véspera) já foi montado para este dia de check-in. */
export async function resumoJaMontado(db: Executor, data: string): Promise<boolean> {
  const { rowCount } = await db.query(`SELECT 1 FROM checkin.resumos WHERE data = $1 LIMIT 1`, [data])
  return (rowCount ?? 0) > 0
}

/** Reservas confirmadas com check-in no dia, em ordem de chalé e hóspede, prontas para o texto. */
export async function reservasConfirmadasPara(db: Executor, data: string): Promise<CheckinEvent[]> {
  const { rows } = await db.query<{
    imovel: string | null
    hospede: string | null
    hospedes: number | null
    canal: string | null
    telefone: string | null
    check_in: string
    check_out: string | null
  }>(
    // Datas como texto: o pg converteria `date` num Date no fuso do processo.
    `SELECT imovel, hospede, hospedes, canal, telefone,
            check_in::text AS check_in, check_out::text AS check_out
     FROM checkin.reservas
     WHERE status = 'confirmada' AND check_in = $1
     ORDER BY imovel NULLS LAST, hospede NULLS LAST, id`,
    [data],
  )
  return rows.map((r) => ({
    imovel: r.imovel ?? undefined,
    hospede: r.hospede ?? undefined,
    hospedes: r.hospedes ?? undefined,
    canal: r.canal ?? undefined,
    telefone: r.telefone ?? undefined,
    checkIn: r.check_in,
    checkOut: r.check_out ?? undefined,
    raw: {},
  }))
}

/** Registra o resumo; null se ele já existia (outro ciclo montou antes). */
export async function registrarResumo(
  db: Executor,
  tipo: TipoResumo,
  data: string,
  reservas: number,
): Promise<number | null> {
  const { rows } = await db.query<{ id: number }>(
    `INSERT INTO checkin.resumos (tipo, data, reservas) VALUES ($1, $2, $3)
     ON CONFLICT (tipo, data) DO NOTHING
     RETURNING id`,
    [tipo, data, reservas],
  )
  return rows[0]?.id ?? null
}

export async function resumoExiste(tipo: TipoResumo, data: string): Promise<boolean> {
  const { rowCount } = await pool.query(
    `SELECT 1 FROM checkin.resumos WHERE tipo = $1 AND data = $2`,
    [tipo, data],
  )
  return (rowCount ?? 0) > 0
}

/** Quantas reservas já estão na tabela (vazia = preencher com os eventos antigos). */
export async function contaReservas(): Promise<number> {
  const { rows } = await pool.query<{ n: number }>(`SELECT count(*) AS n FROM checkin.reservas`)
  return rows[0]?.n ?? 0
}
