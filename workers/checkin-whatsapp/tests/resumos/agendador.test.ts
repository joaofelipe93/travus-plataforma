import { TEST_GROUP_JID } from '../helpers/setup.js'
import { resetDb, listaMensagens, listaReservas, pool } from '../helpers/db.js'
import { test, describe, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import { env } from '../../src/config/env.js'
import { preencherReservas, resumosDevidos, verificarResumos } from '../../src/resumos/agendador.js'

// Horários em UTC; São Paulo é UTC-3 (sem horário de verão desde 2019).
const AS_07_59 = new Date('2026-09-22T10:59:00Z')
const AS_08_00 = new Date('2026-09-22T11:00:00Z')
const AS_16_59 = new Date('2026-09-22T19:59:00Z')
const AS_17_00 = new Date('2026-09-22T20:00:00Z')
const AS_23_59 = new Date('2026-09-23T02:59:00Z')

let seq = 0

/** Reserva direto no banco (evento + reserva), com dados fictícios. */
async function reserva(r: {
  checkIn: string
  imovel: string
  hospede: string
  status?: 'confirmada' | 'cancelada'
}): Promise<void> {
  seq += 1
  const { rows } = await pool.query<{ id: number }>(
    `INSERT INTO checkin.eventos (chave_dedup, origem, payload) VALUES ($1, 'teste', '{}') RETURNING id`,
    [`teste:${seq}`],
  )
  await pool.query(
    `INSERT INTO checkin.reservas
       (reserva_id, status, check_in, check_out, imovel, hospede, hospedes, canal, telefone, ultimo_evento_id)
     VALUES ($1, $2, $3, $3::date + 2, $4, $5, 2, 'airbnb', '5511900000000', $6)`,
    [`R-${seq}`, r.status ?? 'confirmada', r.checkIn, r.imovel, r.hospede, rows[0]!.id],
  )
}

async function resumos(): Promise<{ tipo: string; data: string; reservas: number }[]> {
  const { rows } = await pool.query<{ tipo: string; data: string; reservas: number }>(
    `SELECT tipo, data::text AS data, reservas FROM checkin.resumos ORDER BY id`,
  )
  return rows
}

describe('resumosDevidos', () => {
  test('hoje a partir das 08h, amanhã a partir das 17h, até o fim do dia', () => {
    assert.deepEqual(resumosDevidos(AS_07_59), [])
    assert.deepEqual(resumosDevidos(AS_08_00), [{ tipo: 'hoje', data: '2026-09-22' }])
    assert.deepEqual(resumosDevidos(AS_16_59), [{ tipo: 'hoje', data: '2026-09-22' }])
    assert.deepEqual(resumosDevidos(AS_17_00), [
      { tipo: 'hoje', data: '2026-09-22' },
      { tipo: 'amanha', data: '2026-09-23' },
    ])
    assert.deepEqual(resumosDevidos(AS_23_59), resumosDevidos(AS_17_00))
  })
})

describe('verificarResumos', () => {
  beforeEach(async () => {
    await resetDb()
    env.WHATSAPP_GROUP_JID = TEST_GROUP_JID
  })

  test('às 17h junta as reservas confirmadas de amanhã numa mensagem só', async () => {
    await pool.query(`INSERT INTO checkin.resumos (tipo, data, reservas) VALUES ('hoje', '2026-09-22', 0)`)
    await reserva({ checkIn: '2026-09-23', imovel: 'Chalé 03', hospede: 'Fulano de Tal' })
    await reserva({ checkIn: '2026-09-23', imovel: 'Chalé 01', hospede: 'Beltrana Souza' })
    await reserva({ checkIn: '2026-09-23', imovel: 'Chalé 02', hospede: 'Cancelado', status: 'cancelada' })
    await reserva({ checkIn: '2026-09-24', imovel: 'Chalé 02', hospede: 'Outro Dia' })

    await verificarResumos(AS_17_00)

    const mensagens = await listaMensagens()
    assert.equal(mensagens.length, 1)
    const [msg] = mensagens
    assert.equal(msg!.evento_id, null)
    assert.ok(msg!.resumo_id)
    assert.equal(msg!.destino_jid, TEST_GROUP_JID)
    assert.match(msg!.texto, /^✅ \*Reservas Confirmadas para Amanhã\*\n\n🏠 Chalé 01\n/)
    assert.ok(msg!.texto.indexOf('Chalé 01') < msg!.texto.indexOf('Chalé 03'), 'em ordem de chalé')
    assert.doesNotMatch(msg!.texto, /Cancelado/)
    assert.doesNotMatch(msg!.texto, /Outro Dia/)
    assert.deepEqual((await resumos()).at(-1), { tipo: 'amanha', data: '2026-09-23', reservas: 2 })
  })

  test('às 08h manda o de hoje', async () => {
    await reserva({ checkIn: '2026-09-22', imovel: 'Chalé 01', hospede: 'Fulano de Tal' })
    await verificarResumos(AS_08_00)

    const [msg] = await listaMensagens()
    assert.match(msg!.texto, /^✅ \*Reservas Confirmadas para Hoje\*/)
  })

  test('antes da hora não manda nada', async () => {
    await reserva({ checkIn: '2026-09-22', imovel: 'Chalé 01', hospede: 'Fulano de Tal' })
    await verificarResumos(AS_07_59)

    assert.deepEqual(await listaMensagens(), [])
    assert.deepEqual(await resumos(), [])
  })

  test('nunca sai duas vezes, nem com reserva nova depois', async () => {
    await reserva({ checkIn: '2026-09-22', imovel: 'Chalé 01', hospede: 'Fulano de Tal' })
    await verificarResumos(AS_08_00)
    await reserva({ checkIn: '2026-09-22', imovel: 'Chalé 02', hospede: 'Atrasado' })
    await verificarResumos(AS_16_59)

    assert.equal((await listaMensagens()).length, 1)
  })

  test('dia sem reserva registra o resumo e não manda mensagem', async () => {
    await verificarResumos(AS_17_00)

    assert.deepEqual(await listaMensagens(), [])
    assert.deepEqual(await resumos(), [
      { tipo: 'hoje', data: '2026-09-22', reservas: 0 },
      { tipo: 'amanha', data: '2026-09-23', reservas: 0 },
    ])
  })

  test('sem grupo não registra, e sai quando o grupo aparece', async () => {
    delete env.WHATSAPP_GROUP_JID
    await reserva({ checkIn: '2026-09-22', imovel: 'Chalé 01', hospede: 'Fulano de Tal' })

    await verificarResumos(AS_08_00)
    assert.deepEqual(await resumos(), [])

    env.WHATSAPP_GROUP_JID = TEST_GROUP_JID
    await verificarResumos(AS_08_00)
    assert.equal((await listaMensagens()).length, 1)
  })
})

describe('preencherReservas', () => {
  beforeEach(async () => {
    await resetDb()
  })

  async function evento(chave: string, payload: object): Promise<void> {
    await pool.query(
      `INSERT INTO checkin.eventos (chave_dedup, origem, payload) VALUES ($1, 'nova-reserva', $2::jsonb)`,
      [chave, JSON.stringify(payload)],
    )
  }

  test('preenche com os eventos já recebidos, na ordem, sem mandar nada', async () => {
    const base = { guest_name: 'Fulano de Tal', property_name: 'Chalé 01', check_in: '26/10/2026' }
    await evento('nova-reserva:A', { ...base, booking_uuid: 'A', status: 'confirmed' })
    await evento('nova-reserva:B', { ...base, booking_uuid: 'B', status: 'confirmed' })
    await evento('nova-reserva:A:cancelled', { ...base, booking_uuid: 'A', status: 'cancelled' })
    await evento('nova-reserva:sha256:x', { guest_name: 'Sem Id', check_in: '26/10/2026' })

    await preencherReservas()

    assert.deepEqual(
      (await listaReservas()).map((r) => [r.reserva_id, r.status, r.check_in]),
      [
        ['A', 'cancelada', '2026-10-26'],
        ['B', 'confirmada', '2026-10-26'],
      ],
    )
    assert.deepEqual(await listaMensagens(), [])
  })

  test('com reservas na tabela não mexe', async () => {
    await reserva({ checkIn: '2026-10-26', imovel: 'Chalé 01', hospede: 'Fulano de Tal' })
    await evento('nova-reserva:Z', { booking_uuid: 'Z', guest_name: 'Outro', check_in: '26/10/2026' })

    await preencherReservas()

    assert.equal((await listaReservas()).length, 1)
  })
})
