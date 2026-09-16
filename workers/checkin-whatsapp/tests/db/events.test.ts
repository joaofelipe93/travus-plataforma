import { resetDb, getEventRow } from '../helpers/db.js'
import { test, describe, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import { insertEvent, listRecentEvents } from '../../src/db/events.js'

describe('insertEvent', () => {
  beforeEach(resetDb)

  test('grava um evento novo', async () => {
    const result = await insertEvent({
      dedupeKey: 'nova-reserva:RES-1',
      source: 'nova-reserva',
      rawPayload: '{"id":"RES-1"}',
    })

    assert.equal(result.isDuplicate, false)
    assert.ok(result.id > 0)

    const row = await getEventRow(result.id)
    assert.equal(row.chave_dedup, 'nova-reserva:RES-1')
    assert.equal(row.origem, 'nova-reserva')
    assert.deepEqual(row.payload, { id: 'RES-1' })
    assert.ok(row.recebido_em instanceof Date && !Number.isNaN(row.recebido_em.getTime()))
  })

  test('a mesma dedupeKey devolve o id existente e marca duplicado', async () => {
    const first = await insertEvent({ dedupeKey: 'k', source: 's', rawPayload: '{"v":1}' })
    const second = await insertEvent({ dedupeKey: 'k', source: 's', rawPayload: '{"v":2}' })

    assert.equal(second.isDuplicate, true)
    assert.equal(second.id, first.id)
  })

  test('duplicado não sobrescreve o payload gravado da primeira vez', async () => {
    const first = await insertEvent({ dedupeKey: 'k', source: 's', rawPayload: '{"v":1}' })
    await insertEvent({ dedupeKey: 'k', source: 's', rawPayload: '{"v":2}' })

    assert.deepEqual((await getEventRow(first.id)).payload, { v: 1 })
    assert.equal((await listRecentEvents()).length, 1)
  })

  test('chaves diferentes geram eventos distintos', async () => {
    const a = await insertEvent({ dedupeKey: 'a', source: 's', rawPayload: '{}' })
    const b = await insertEvent({ dedupeKey: 'b', source: 's', rawPayload: '{}' })

    assert.equal(b.isDuplicate, false)
    assert.notEqual(a.id, b.id)
  })

  test('aceita payload que não é objeto', async () => {
    const result = await insertEvent({ dedupeKey: 'lista', source: 's', rawPayload: '[1,"dois",null]' })
    assert.deepEqual((await getEventRow(result.id)).payload, [1, 'dois', null])
  })
})

describe('listRecentEvents', () => {
  beforeEach(resetDb)

  test('devolve os mais recentes primeiro', async () => {
    for (const key of ['a', 'b', 'c']) {
      await insertEvent({ dedupeKey: key, source: 's', rawPayload: `{"k":"${key}"}` })
    }

    const events = await listRecentEvents()
    assert.deepEqual(
      events.map((e) => e.chave_dedup),
      ['c', 'b', 'a'],
    )
  })

  test('respeita o limite', async () => {
    for (let i = 0; i < 5; i++) {
      await insertEvent({ dedupeKey: `k${i}`, source: 's', rawPayload: '{}' })
    }

    assert.equal((await listRecentEvents(2)).length, 2)
    assert.equal((await listRecentEvents()).length, 5)
  })

  test('banco vazio devolve lista vazia', async () => {
    assert.deepEqual(await listRecentEvents(), [])
  })
})
