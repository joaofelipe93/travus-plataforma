import { TEST_SECRET, TEST_GROUP_JID } from '../helpers/setup.js'
import {
  resetDb,
  getOutboxRow,
  contaMensagens,
  getEventRow,
  listaMensagens,
  listaReservas,
  seedResumo,
} from '../helpers/db.js'
import { buildWebhookApp } from '../helpers/app.js'
import { test, describe, before, beforeEach, after } from 'node:test'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import type { FastifyInstance } from 'fastify'
import { env } from '../../src/config/env.js'
import { listRecentEvents } from '../../src/db/events.js'
import { definirGrupoDestino } from '../../src/db/configuracao.js'
import { relogioLocal, somarDias } from '../../src/domain/datas.js'

const URL = '/webhooks/nova-reserva'

// A regra do resumo compara com o dia de hoje no fuso das pousadas: datas relativas a hoje.
const HOJE = relogioLocal(new Date(), 'America/Sao_Paulo').data
const ONTEM = somarDias(HOJE, -1)
const AMANHA = somarDias(HOJE, 1)
const DEPOIS_DE_AMANHA = somarDias(HOJE, 2)
const DAQUI_A_UMA_SEMANA = somarDias(HOJE, 7)

/** `AAAA-MM-DD` → `dd/mm/aaaa`, como o provedor real manda. */
function br(data: string): string {
  const [ano, mes, dia] = data.split('-')
  return `${dia}/${mes}/${ano}`
}

/** Payload no formato do provedor real (dados fictícios). */
function reservaReal(extra: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    guest_name: 'Fulano de Tal',
    guest_phone: '+55 11 90000 1234',
    property_name: 'Chalé 01',
    check_in: br(AMANHA),
    check_out: br(DEPOIS_DE_AMANHA),
    guests: '2',
    channel: 'booking',
    status: 'confirmed',
    cancellation_reason: '',
    booking_uuid: '0a0b0c0d-0000-4000-8000-000000000001',
    _workflow_execution_id: 968,
    ...extra,
  }
}

let app: FastifyInstance

function post(payload: unknown, headers: Record<string, string> = {}) {
  return app.inject({
    method: 'POST',
    url: URL,
    headers: { 'x-webhook-token': TEST_SECRET, ...headers },
    payload: payload as object,
  })
}

async function dedupeKeys(): Promise<string[]> {
  return (await listRecentEvents(100)).map((e) => e.chave_dedup)
}

before(async () => {
  app = await buildWebhookApp()
})
after(async () => {
  await app.close()
})

describe('POST /webhooks/nova-reserva — autenticação', () => {
  beforeEach(async () => {
    await resetDb()
    env.WHATSAPP_GROUP_JID = TEST_GROUP_JID
  })

  test('401 sem token, e nada é gravado', async () => {
    const res = await app.inject({ method: 'POST', url: URL, payload: { id: 'RES-1' } })

    assert.equal(res.statusCode, 401)
    assert.deepEqual((await listRecentEvents()), [])
  })

  test('401 com token errado', async () => {
    const res = await post({ id: 'RES-1' }, { 'x-webhook-token': 'errado' })
    assert.equal(res.statusCode, 401)
  })

  test('aceita Bearer', async () => {
    const res = await app.inject({
      method: 'POST',
      url: URL,
      headers: { authorization: `Bearer ${TEST_SECRET}` },
      payload: { id: 'RES-1' },
    })
    assert.equal(res.statusCode, 200)
  })
})

describe('POST /webhooks/nova-reserva — enfileiramento', () => {
  beforeEach(async () => {
    await resetDb()
    env.WHATSAPP_GROUP_JID = TEST_GROUP_JID
  })

  test('enfileira a mensagem e devolve 200 imediatamente', async () => {
    // Um resumo do dia de check-in já saiu: a reserva vai avulsa (ver "resumo diário" abaixo).
    await seedResumo('amanha', AMANHA)
    const res = await post({
      id: 'RES-001',
      guest: { name: 'João Silva' },
      listing: { name: 'Apto 302' },
      check_in: AMANHA,
      check_out: DEPOIS_DE_AMANHA,
      guests: 2,
    })

    assert.equal(res.statusCode, 200)
    const body = res.json() as { status: string; eventId: number; outboxId: number }
    assert.equal(body.status, 'queued')
    assert.ok(body.eventId > 0)
    assert.ok(body.outboxId > 0)

    const row = await getOutboxRow(body.outboxId)
    assert.equal(row.evento_id, body.eventId)
    assert.equal(row.destino_jid, TEST_GROUP_JID)
    assert.equal(row.status, 'pendente')
    assert.match(row.texto, /👤 João Silva/)
    assert.match(row.texto, new RegExp(`📅 ${br(AMANHA)} → ${br(DEPOIS_DE_AMANHA)}`))
  })

  test('grava o payload como veio', async () => {
    const payload = { id: 'RES-2', extra: { lista: [1, 2, 3] } }
    await post(payload)

    const [evento] = (await listRecentEvents())
    assert.ok(evento)
    assert.deepEqual(evento.payload, payload)
    assert.equal(evento.origem, 'nova-reserva')
  })

  test('aceita payload sem nenhum campo conhecido', async () => {
    const res = await post({ formato: 'desconhecido' })

    assert.equal(res.json().status, 'queued')
    const row = await getOutboxRow((res.json() as { outboxId: number }).outboxId)
    assert.match(row.texto, /Formato não reconhecido/)
  })

  test('aceita corpo vazio sem virar 500', async () => {
    const res = await app.inject({
      method: 'POST',
      url: URL,
      headers: { 'x-webhook-token': TEST_SECRET },
    })

    assert.equal(res.statusCode, 200)
    assert.equal(res.json().status, 'queued')
    assert.deepEqual((await dedupeKeys()), ['nova-reserva:sha256:' + sha256('{}')])
  })
})

describe('POST /webhooks/nova-reserva — chave de deduplicação', () => {
  beforeEach(async () => {
    await resetDb()
    env.WHATSAPP_GROUP_JID = TEST_GROUP_JID
  })

  test('usa o primeiro campo de id disponível, na ordem definida', async () => {
    await post({ id: 'do-id', reservation_id: 'do-reservation' })
    assert.deepEqual((await dedupeKeys()), ['nova-reserva:do-id'])
  })

  test('cai para os campos alternativos quando não há `id`', async () => {
    const casos: [Record<string, unknown>, string][] = [
      [{ event_id: 'E1' }, 'nova-reserva:E1'],
      [{ reservation_id: 'R1' }, 'nova-reserva:R1'],
      [{ booking_id: 'B1' }, 'nova-reserva:B1'],
      [{ confirmation_code: 'C1' }, 'nova-reserva:C1'],
      [{ reservation_code: 'RC1' }, 'nova-reserva:RC1'],
      [{ uuid: 'U1' }, 'nova-reserva:U1'],
      [{ booking_uuid: 'BU1' }, 'nova-reserva:BU1'],
    ]

    for (const [payload, esperado] of casos) {
      await resetDb()
      await post(payload)
      assert.deepEqual((await dedupeKeys()), [esperado], JSON.stringify(payload))
    }
  })

  test('aceita id numérico e faz trim de id textual', async () => {
    await post({ id: 42 })
    await post({ id: '  RES-3  ' })
    assert.deepEqual((await dedupeKeys()).sort(), ['nova-reserva:42', 'nova-reserva:RES-3'].sort())
  })

  test('sem campo de id, usa o sha256 do payload', async () => {
    const payload = { guest_name: 'Ana', check_in: '2026-01-01' }
    await post(payload)
    assert.deepEqual((await dedupeKeys()), ['nova-reserva:sha256:' + sha256(JSON.stringify(payload))])
  })

  test('id vazio não vira chave — cai no hash', async () => {
    await post({ id: '   ' })
    const [chave] = (await dedupeKeys())
    assert.match(chave ?? '', /^nova-reserva:sha256:/)
  })

  test('cancelamento não é engolido como duplicata da reserva', async () => {
    // O provedor manda o MESMO booking_uuid na reserva e no cancelamento. Sem
    // o status na chave, o segundo evento casaria com o primeiro e o grupo
    // nunca saberia do cancelamento.
    const uuid = '0a0b0c0d-0000-4000-8000-000000000001'
    const base = { guest_name: 'Fulano', property_name: 'Chalé 01', booking_uuid: uuid }

    const reserva = await post({ ...base, status: 'confirmed', cancellation_reason: '' })
    assert.equal(reserva.json().status, 'queued')

    const cancelamento = await post({ ...base, status: 'cancelled', cancellation_reason: 'Desistiu' })
    assert.equal(cancelamento.json().status, 'queued', 'o cancelamento precisa gerar mensagem própria')

    assert.deepEqual((await dedupeKeys()).sort(), [
      `nova-reserva:${uuid}`,
      `nova-reserva:${uuid}:cancelled`,
    ].sort())

    // E cada um gerou a sua mensagem, com o título certo.
    const msgReserva = (await getOutboxRow((reserva.json() as { outboxId: number }).outboxId)).texto
    const msgCancel = (await getOutboxRow((cancelamento.json() as { outboxId: number }).outboxId)).texto
    assert.match(msgReserva, /✅ \*Nova Reserva Realizada\*/)
    assert.match(msgCancel, /❌ \*Cancelamento de Reserva\*/)
  })

  test('o cancelamento repetido continua sendo deduplicado', async () => {
    const base = {
      guest_name: 'Fulano',
      booking_uuid: '0a0b0c0d-0000-4000-8000-000000000001',
      status: 'cancelled',
    }
    assert.equal((await post(base)).json().status, 'queued')
    assert.equal((await post(base)).json().status, 'duplicate')
  })

  test('status confirmed não muda a chave já usada pelas reservas existentes', async () => {
    // Sufixo só para status que não é confirmação: um `.env` em produção já tem
    // eventos gravados com a chave sem sufixo, e mudá-la duplicaria mensagens.
    await post({ booking_uuid: 'BU-9', status: 'confirmed' })
    assert.deepEqual((await dedupeKeys()), ['nova-reserva:BU-9'])
  })

  test('reprocessamento no provedor não escapa da deduplicação', async () => {
    // Payload real: o id da reserva vem em `booking_uuid`, e o corpo carrega
    // `_workflow_execution_id`, que muda a cada execução do workflow. Enquanto
    // `booking_uuid` não era reconhecido, a chave era o hash do corpo inteiro —
    // então reprocessar a mesma reserva gerava chave nova e o grupo recebia a
    // mensagem duas vezes.
    const reserva = {
      guest_name: 'Fulano de Tal',
      property_name: 'Chalé 01',
      check_in: '26/10/2026',
      check_out: '28/10/2026',
      booking_uuid: '0a0b0c0d-0000-4000-8000-000000000001',
      _workflow_id: 38,
      _workflow_execution_id: 968,
    }

    const primeira = await post(reserva)
    assert.equal(primeira.json().status, 'scheduled')

    // Mesma reserva, outra execução do workflow: corpo diferente, reserva igual.
    const segunda = await post({ ...reserva, _workflow_execution_id: 969 })
    assert.equal(segunda.json().status, 'duplicate')

    assert.deepEqual((await dedupeKeys()), [
      'nova-reserva:0a0b0c0d-0000-4000-8000-000000000001',
    ])
  })
})

describe('POST /webhooks/nova-reserva — idempotência', () => {
  beforeEach(async () => {
    await resetDb()
    env.WHATSAPP_GROUP_JID = TEST_GROUP_JID
  })

  test('reenvio do mesmo id não duplica a mensagem no grupo', async () => {
    const primeira = await post({ id: 'RES-1', guest_name: 'Ana' })
    const segunda = await post({ id: 'RES-1', guest_name: 'Ana' })

    assert.equal(primeira.json().status, 'queued')
    assert.equal(segunda.json().status, 'duplicate')
    assert.equal(segunda.json().eventId, primeira.json().eventId)
    assert.equal((await contaMensagens()), 1)
  })

  test('mesmo id com corpo diferente ainda é duplicado', async () => {
    await post({ id: 'RES-1', guest_name: 'Ana' })
    const segunda = await post({ id: 'RES-1', guest_name: 'Outro Nome' })

    assert.equal(segunda.json().status, 'duplicate')
    assert.equal((await contaMensagens()), 1)
  })

  test('payloads idênticos sem id são deduplicados pelo hash', async () => {
    await post({ guest_name: 'Ana' })
    const segunda = await post({ guest_name: 'Ana' })

    assert.equal(segunda.json().status, 'duplicate')
    assert.equal((await contaMensagens()), 1)
  })

  test('payloads diferentes sem id geram mensagens separadas', async () => {
    await post({ guest_name: 'Ana' })
    const segunda = await post({ guest_name: 'Bruno' })

    assert.equal(segunda.json().status, 'queued')
    assert.equal((await contaMensagens()), 2)
  })

  test('ids diferentes geram mensagens separadas', async () => {
    await post({ id: 'RES-1' })
    const segunda = await post({ id: 'RES-2' })

    assert.equal(segunda.json().status, 'queued')
    assert.equal((await contaMensagens()), 2)
  })
})

describe('POST /webhooks/nova-reserva — sem grupo configurado', () => {
  beforeEach(async () => {
    await resetDb()
    delete env.WHATSAPP_GROUP_JID
  })

  test('grava o evento e responde stored_no_target', async () => {
    const res = await post({ id: 'RES-1', guest_name: 'Ana' })
    const body = res.json() as { status: string; eventId: number; hint: string }

    assert.equal(res.statusCode, 200)
    assert.equal(body.status, 'stored_no_target')
    assert.ok(body.eventId > 0)
    assert.match(body.hint, /escolha o grupo/)
    assert.equal((await contaMensagens()), 0)
    assert.equal((await listRecentEvents()).length, 1)
  })

  test('o grupo escolhido pela tela vale mais que o do ambiente', async () => {
    env.WHATSAPP_GROUP_JID = TEST_GROUP_JID
    await definirGrupoDestino('5511900000000-1600000000@g.us', 'Grupo da tela')

    const res = await post({ id: 'RES-TELA' })
    assert.equal(res.json().status, 'queued')
    const row = await getOutboxRow((res.json() as { outboxId: number }).outboxId)
    assert.equal(row.destino_jid, '5511900000000-1600000000@g.us')
  })

  test('reenvio depois de configurar o grupo recupera o evento preso', async () => {
    // É o ponto do `eventoProcessado` no handler: um evento gravado antes de
    // existir destino não pode ficar marcado como duplicado para sempre.
    const payload = { id: 'RES-1', guest_name: 'Ana' }

    const antes = await post(payload)
    assert.equal(antes.json().status, 'stored_no_target')

    env.WHATSAPP_GROUP_JID = TEST_GROUP_JID
    const depois = await post(payload)

    assert.equal(depois.json().status, 'queued')
    assert.equal(depois.json().eventId, antes.json().eventId, 'reaproveita o evento já gravado')
    assert.equal((await contaMensagens()), 1)
    assert.equal((await listRecentEvents()).length, 1, 'não duplica o evento')

    // E a partir daí volta a deduplicar normalmente.
    const terceira = await post(payload)
    assert.equal(terceira.json().status, 'duplicate')
    assert.equal((await contaMensagens()), 1)
  })
})

describe('POST /webhooks/nova-reserva — resumo diário', () => {
  beforeEach(async () => {
    await resetDb()
    env.WHATSAPP_GROUP_JID = TEST_GROUP_JID
  })

  test('reserva com data guarda para o resumo e não manda nada agora', async () => {
    const res = await post(reservaReal())
    const body = res.json() as { status: string; eventId: number }

    assert.equal(res.statusCode, 200)
    assert.equal(body.status, 'scheduled')
    assert.equal(await contaMensagens(), 0)
    assert.deepEqual(
      (await listaReservas()).map((r) => [r.reserva_id, r.status, r.check_in, r.hospede]),
      [['0a0b0c0d-0000-4000-8000-000000000001', 'confirmada', AMANHA, 'Fulano de Tal']],
    )
    assert.ok((await getEventRow(body.eventId)).processado_em, 'evento marcado como processado')

    // Reprocessamento no provedor: sem mensagem, o que deduplica é o `processado_em`.
    const segunda = await post(reservaReal({ _workflow_execution_id: 969 }))
    assert.equal(segunda.json().status, 'duplicate')
  })

  test('cancelamento antes do resumo só tira a reserva do resumo', async () => {
    await post(reservaReal())
    const res = await post(reservaReal({ status: 'cancelled', cancellation_reason: 'Desistiu' }))

    assert.equal(res.json().status, 'scheduled')
    assert.equal(await contaMensagens(), 0)
    assert.deepEqual((await listaReservas()).map((r) => r.status), ['cancelada'])
  })

  test('reserva que chega depois do resumo do dia vai avulsa', async () => {
    await seedResumo('amanha', AMANHA)

    const res = await post(reservaReal())
    assert.equal(res.json().status, 'queued')
    const row = await getOutboxRow((res.json() as { outboxId: number }).outboxId)
    assert.match(row.texto, /✅ \*Nova Reserva Realizada\*/)
    assert.equal((await listaReservas()).length, 1, 'e fica na reserva para o resumo de hoje')
  })

  test('cancelamento de reserva que já saiu em resumo vai avulsa', async () => {
    await post(reservaReal())
    await seedResumo('amanha', AMANHA)

    const res = await post(reservaReal({ status: 'cancelled', cancellation_reason: 'Desistiu' }))
    assert.equal(res.json().status, 'queued')
    const row = await getOutboxRow((res.json() as { outboxId: number }).outboxId)
    assert.match(row.texto, /❌ \*Cancelamento de Reserva\*/)
  })

  test('cancelamento depois do resumo de reserva que o grupo nunca viu não manda nada', async () => {
    await seedResumo('amanha', AMANHA)
    const res = await post(reservaReal({ status: 'cancelled', cancellation_reason: 'Desistiu' }))

    assert.equal(res.json().status, 'scheduled')
    assert.equal(await contaMensagens(), 0)
  })

  test('resumo de outro dia não conta: reserva da semana que vem espera o resumo dela', async () => {
    await seedResumo('amanha', AMANHA)
    await seedResumo('hoje', HOJE)

    const res = await post(reservaReal({ check_in: br(DAQUI_A_UMA_SEMANA) }))
    assert.equal(res.json().status, 'scheduled')
    assert.equal(await contaMensagens(), 0)
  })

  test('check-in já passado não manda nada, nem com resumo registrado', async () => {
    await seedResumo('hoje', ONTEM)
    const res = await post(reservaReal({ check_in: br(ONTEM) }))

    assert.equal(res.json().status, 'scheduled')
    assert.equal(await contaMensagens(), 0)
  })

  test('data ilegível não dá para agendar: vai avulsa como antes', async () => {
    const res = await post(reservaReal({ check_in: 'amanhã cedo' }))

    assert.equal(res.json().status, 'queued')
    assert.deepEqual(await listaReservas(), [])
  })

  test('avulsa sem grupo desfaz a reserva e o reenvio com grupo recupera', async () => {
    await seedResumo('amanha', AMANHA)
    delete env.WHATSAPP_GROUP_JID

    const antes = await post(reservaReal())
    assert.equal(antes.json().status, 'stored_no_target')
    assert.deepEqual(await listaReservas(), [], 'nada gravado além do evento')

    env.WHATSAPP_GROUP_JID = TEST_GROUP_JID
    const depois = await post(reservaReal())
    assert.equal(depois.json().status, 'queued')
    assert.equal(depois.json().eventId, antes.json().eventId)
    assert.equal((await listaMensagens()).length, 1)
  })
})

function sha256(text: string): string {
  return createHash('sha256').update(text).digest('hex')
}
