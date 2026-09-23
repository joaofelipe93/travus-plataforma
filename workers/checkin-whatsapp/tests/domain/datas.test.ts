import '../helpers/setup.js'
import { test, describe } from 'node:test'
import assert from 'node:assert/strict'
import { lerData, relogioLocal, somarDias } from '../../src/domain/datas.js'

describe('lerData', () => {
  test('dd/mm/aaaa do provedor real e ISO viram AAAA-MM-DD', () => {
    assert.equal(lerData('26/10/2026'), '2026-10-26')
    assert.equal(lerData(' 01/02/2027 '), '2027-02-01')
    assert.equal(lerData('2026-10-26'), '2026-10-26')
    assert.equal(lerData('2026-10-26T15:00:00-03:00'), '2026-10-26')
  })

  test('o que não é data vira undefined', () => {
    for (const valor of [undefined, '', 'amanhã', '31/02/2026', '2026-13-01', '26-10-2026', '6/9/2026']) {
      assert.equal(lerData(valor), undefined, String(valor))
    }
  })
})

describe('somarDias', () => {
  test('atravessa mês e ano', () => {
    assert.equal(somarDias('2026-09-30', 1), '2026-10-01')
    assert.equal(somarDias('2026-12-31', 1), '2027-01-01')
    assert.equal(somarDias('2026-03-01', -1), '2026-02-28')
  })
})

describe('relogioLocal', () => {
  test('usa o fuso da pousada, não o do servidor', () => {
    // 02:30 UTC ainda é o dia anterior em São Paulo (UTC-3).
    assert.deepEqual(relogioLocal(new Date('2026-09-23T02:30:00Z'), 'America/Sao_Paulo'), {
      data: '2026-09-22',
      hora: 23,
    })
    assert.deepEqual(relogioLocal(new Date('2026-09-22T11:00:00Z'), 'America/Sao_Paulo'), {
      data: '2026-09-22',
      hora: 8,
    })
    assert.deepEqual(relogioLocal(new Date('2026-09-22T03:00:00Z'), 'America/Sao_Paulo'), {
      data: '2026-09-22',
      hora: 0,
    })
  })
})
