import { resetDb, seedEvent, getOutboxRow, setNextAttempt } from '../helpers/db.js'
import { test, describe, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import {
  enqueue,
  claimPending,
  markSent,
  markFailure,
  hasMessageForEvent,
  countPending,
  stats,
  MAX_ATTEMPTS,
} from '../../src/db/outbox.js'

const JID = '1234567890-1234567890@g.us'

async function enfileira(eventId?: number, body = 'mensagem'): Promise<number> {
  return enqueue({ eventId: eventId ?? (await seedEvent()), targetJid: JID, body })
}

/** Segundos entre `proxima_tentativa_em` (relógio do Postgres) e agora. */
async function atrasoEmSegundos(id: number): Promise<number> {
  return ((await getOutboxRow(id)).proxima_tentativa_em.getTime() - Date.now()) / 1000
}

describe('enqueue', () => {
  beforeEach(resetDb)

  test('grava a mensagem pendente e elegível imediatamente', async () => {
    const id = await enfileira()
    const row = await getOutboxRow(id)

    assert.equal(row.status, 'pendente')
    assert.equal(row.tentativas, 0)
    assert.equal(row.destino_jid, JID)
    assert.equal(row.ultimo_erro, null)
    assert.equal(row.enviada_em, null)
    assert.ok(row.proxima_tentativa_em.getTime() <= Date.now() + 1000)
    assert.deepEqual(await claimPending(), [row])
  })

  test('exige um evento existente (FK)', async () => {
    await assert.rejects(
      () => enqueue({ eventId: 999_999, targetJid: JID, body: 'x' }),
      /foreign key/i,
    )
  })
})

describe('hasMessageForEvent', () => {
  beforeEach(resetDb)

  test('false antes de enfileirar, true depois', async () => {
    const eventId = await seedEvent()
    assert.equal(await hasMessageForEvent(eventId), false)
    await enfileira(eventId)
    assert.equal(await hasMessageForEvent(eventId), true)
  })

  test('continua true depois de enviada ou falhada', async () => {
    const eventId = await seedEvent()
    const id = await enfileira(eventId)
    await markSent(id)
    assert.equal(await hasMessageForEvent(eventId), true)
  })

  test('não confunde eventos diferentes', async () => {
    const comMensagem = await seedEvent()
    const semMensagem = await seedEvent()
    await enfileira(comMensagem)

    assert.equal(await hasMessageForEvent(comMensagem), true)
    assert.equal(await hasMessageForEvent(semMensagem), false)
  })
})

describe('claimPending', () => {
  beforeEach(resetDb)

  test('devolve em ordem de id (FIFO)', async () => {
    const ids = [await enfileira(), await enfileira(), await enfileira()]
    assert.deepEqual(
      (await claimPending()).map((r) => r.id),
      ids,
    )
  })

  test('respeita o limite do lote', async () => {
    for (let i = 0; i < 5; i++) await enfileira()
    assert.equal((await claimPending(2)).length, 2)
    assert.equal((await claimPending()).length, 5)
  })

  test('ignora mensagens agendadas para o futuro', async () => {
    const agora = await enfileira()
    const depois = await enfileira()
    await setNextAttempt(depois, new Date(Date.now() + 60_000))

    assert.deepEqual(
      (await claimPending()).map((r) => r.id),
      [agora],
    )
  })

  test('ignora mensagens já enviadas ou definitivamente falhadas', async () => {
    const enviada = await enfileira()
    const pendente = await enfileira()
    await markSent(enviada)

    assert.deepEqual(
      (await claimPending()).map((r) => r.id),
      [pendente],
    )
  })
})

describe('markSent', () => {
  beforeEach(resetDb)

  test('marca como enviada, conta a tentativa e limpa o erro', async () => {
    const id = await enfileira()
    await markFailure(await getOutboxRow(id), 'falha temporária')
    await setNextAttempt(id, new Date(Date.now() - 1000))

    await markSent(id)
    const row = await getOutboxRow(id)

    assert.equal(row.status, 'enviada')
    assert.equal(row.tentativas, 2)
    assert.equal(row.ultimo_erro, null)
    assert.ok(row.enviada_em instanceof Date)
    assert.deepEqual(await claimPending(), [])
  })
})

describe('markFailure', () => {
  beforeEach(resetDb)

  test('primeira falha reagenda em ~5s e guarda o erro', async () => {
    const id = await enfileira()
    const outcome = await markFailure(await getOutboxRow(id), 'socket caiu')

    assert.equal(outcome.retrying, true)
    assert.equal(outcome.attempts, 1)

    const row = await getOutboxRow(id)
    assert.equal(row.status, 'pendente')
    assert.equal(row.tentativas, 1)
    assert.equal(row.ultimo_erro, 'socket caiu')
    const atraso = await atrasoEmSegundos(id)
    assert.ok(atraso > 3 && atraso <= 5.5, `backoff inicial ~5s, veio ${atraso}`)
  })

  test('backoff triplica a cada tentativa: 5s, 15s, 45s, 135s, 405s', async () => {
    const id = await enfileira()
    const esperados = [5, 15, 45, 135, 405]

    for (const [i, segundos] of esperados.entries()) {
      const outcome = await markFailure(await getOutboxRow(id), `erro ${i}`)
      assert.equal(outcome.retrying, true, `tentativa ${i + 1} deveria reagendar`)

      const atraso = await atrasoEmSegundos(id)
      assert.ok(
        atraso > segundos - 2 && atraso <= segundos + 0.5,
        `tentativa ${i + 1}: esperado ~${segundos}s, veio ${atraso.toFixed(1)}s`,
      )
    }
  })

  test('esgotadas as tentativas, marca como falhou e sai da fila', async () => {
    const id = await enfileira()

    for (let i = 0; i < MAX_ATTEMPTS - 1; i++) {
      const outcome = await markFailure(await getOutboxRow(id), `erro ${i}`)
      assert.equal(outcome.retrying, true)
    }

    const ultima = await markFailure(await getOutboxRow(id), 'erro final')
    assert.equal(ultima.retrying, false)
    assert.equal(ultima.attempts, MAX_ATTEMPTS)

    const row = await getOutboxRow(id)
    assert.equal(row.status, 'falhou')
    assert.equal(row.tentativas, MAX_ATTEMPTS)
    assert.equal(row.ultimo_erro, 'erro final')

    await setNextAttempt(id, new Date(Date.now() - 1000))
    assert.deepEqual(await claimPending(), [], 'mensagem falhada não volta para a fila')
  })

  test('não altera proxima_tentativa_em ao falhar definitivamente', async () => {
    const id = await enfileira()
    for (let i = 0; i < MAX_ATTEMPTS - 1; i++) await markFailure(await getOutboxRow(id), 'e')

    const antes = (await getOutboxRow(id)).proxima_tentativa_em
    await markFailure(await getOutboxRow(id), 'final')
    assert.deepEqual((await getOutboxRow(id)).proxima_tentativa_em, antes)
  })
})

describe('stats e countPending', () => {
  beforeEach(resetDb)

  test('agrupa por status', async () => {
    const pendente = await enfileira()
    const enviada = await enfileira()
    const falhada = await enfileira()

    await markSent(enviada)
    for (let i = 0; i < MAX_ATTEMPTS; i++) await markFailure(await getOutboxRow(falhada), 'e')

    const porStatus = Object.fromEntries((await stats()).map((s) => [s.status, s.count]))
    assert.deepEqual(porStatus, { pendente: 1, enviada: 1, falhou: 1 })
    assert.equal((await getOutboxRow(pendente)).status, 'pendente')
    assert.equal(await countPending(), 1)
  })

  test('fila vazia devolve lista vazia', async () => {
    assert.deepEqual(await stats(), [])
    assert.equal(await countPending(), 0)
  })
})
