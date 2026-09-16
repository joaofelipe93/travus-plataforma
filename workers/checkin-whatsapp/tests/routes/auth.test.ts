import { TEST_SECRET, TEST_ADMIN_TOKEN } from '../helpers/setup.js'
import { buildProtectedApp } from '../helpers/app.js'
import { test, describe, before, after } from 'node:test'
import assert from 'node:assert/strict'
import type { FastifyInstance } from 'fastify'

describe('requireToken', () => {
  let app: FastifyInstance

  before(async () => {
    app = await buildProtectedApp()
  })
  after(async () => {
    await app.close()
  })

  const get = (headers: Record<string, string>) =>
    app.inject({ method: 'GET', url: '/protegida', headers })

  test('aceita x-webhook-token correto', async () => {
    const res = await get({ 'x-webhook-token': TEST_SECRET })
    assert.equal(res.statusCode, 200)
    assert.deepEqual(res.json(), { ok: true })
  })

  test('aceita Authorization: Bearer correto', async () => {
    const res = await get({ authorization: `Bearer ${TEST_SECRET}` })
    assert.equal(res.statusCode, 200)
  })

  test('rejeita requisição sem token', async () => {
    const res = await get({})
    assert.equal(res.statusCode, 401)
    assert.deepEqual(res.json(), { error: 'unauthorized' })
  })

  test('rejeita token errado do mesmo tamanho', async () => {
    const errado = 'x'.repeat(TEST_SECRET.length)
    assert.equal(errado.length, TEST_SECRET.length)
    const res = await get({ 'x-webhook-token': errado })
    assert.equal(res.statusCode, 401)
  })

  test('rejeita token de tamanho diferente sem estourar no timingSafeEqual', async () => {
    for (const token of ['curto', `${TEST_SECRET}extra`, '']) {
      const res = await get({ 'x-webhook-token': token })
      assert.equal(res.statusCode, 401, `token ${JSON.stringify(token)} deveria dar 401`)
    }
  })

  test('rejeita Authorization sem o prefixo Bearer', async () => {
    const res = await get({ authorization: TEST_SECRET })
    assert.equal(res.statusCode, 401)
  })

  test('x-webhook-token tem precedência sobre o Bearer', async () => {
    // Header presente e inválido não deve ser "salvo" por um Bearer correto:
    // o provedor precisa corrigir o que está mandando de fato.
    const res = await get({
      'x-webhook-token': 'invalido',
      authorization: `Bearer ${TEST_SECRET}`,
    })
    assert.equal(res.statusCode, 401)
  })

  test('não vaza o segredo no corpo da resposta', async () => {
    const res = await get({ 'x-webhook-token': 'invalido' })
    assert.equal(res.body.includes(TEST_SECRET), false)
  })
})

describe('requireAdminToken', () => {
  let app: FastifyInstance

  before(async () => {
    app = await buildProtectedApp()
  })
  after(async () => {
    await app.close()
  })

  test('aceita o token de administração', async () => {
    const res = await app.inject({ method: 'GET', url: '/admin', headers: { authorization: `Bearer ${TEST_ADMIN_TOKEN}` } })
    assert.equal(res.statusCode, 200)
  })

  test('o token do webhook não abre as rotas de administração (QR, payloads)', async () => {
    for (const headers of [{ 'x-webhook-token': TEST_SECRET }, { authorization: `Bearer ${TEST_SECRET}` }]) {
      const res = await app.inject({ method: 'GET', url: '/admin', headers })
      assert.equal(res.statusCode, 401)
    }
  })

  test('o token de administração não serve para o webhook', async () => {
    const res = await app.inject({ method: 'GET', url: '/protegida', headers: { 'x-webhook-token': TEST_ADMIN_TOKEN } })
    assert.equal(res.statusCode, 401)
  })
})
