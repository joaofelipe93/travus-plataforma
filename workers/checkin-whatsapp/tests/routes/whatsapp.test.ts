import { TEST_ADMIN_TOKEN, TEST_SECRET, TEST_GROUP_JID } from '../helpers/setup.js'
import { resetDb, pool } from '../helpers/db.js'
import { test, describe, before, beforeEach, after, mock } from 'node:test'
import assert from 'node:assert/strict'
import Fastify, { type FastifyInstance } from 'fastify'
import { env } from '../../src/config/env.js'

/** Rotas de administração com client.js e sender.js trocados por dublês (sem WhatsApp). */
const GRUPOS = [
  { jid: TEST_GROUP_JID, subject: 'Reservas Chalés', participants: 4 },
  { jid: '5511900000000-1600000000@g.us', subject: 'Outro grupo', participants: 2 },
]

let conectado = true
let qr: string | undefined
let enviados: { jid: string; texto: string }[] = []
let reinicios = 0

class NotConnectedError extends Error {}

mock.module('../../src/whatsapp/client.js', {
  exports: {
    getStatus: () => (conectado ? 'open' : qr ? 'qr' : 'disconnected'),
    isConnected: () => conectado,
    getQr: () => qr,
    getConta: () => (conectado ? '5511900000000:3@s.whatsapp.net' : undefined),
    reiniciarSessao: async () => {
      reinicios += 1
      conectado = false
      qr = 'qr-novo'
    },
  },
})

mock.module('../../src/whatsapp/sender.js', {
  exports: {
    NotConnectedError,
    listGroups: async () => {
      if (!conectado) throw new NotConnectedError()
      return GRUPOS
    },
    sendText: async (jid: string, texto: string) => {
      if (!conectado) throw new NotConnectedError()
      enviados.push({ jid, texto })
      return 'msg-id'
    },
  },
})

const { whatsappRoutes } = await import('../../src/routes/whatsapp.js')

let app: FastifyInstance

function req(method: 'GET' | 'PUT' | 'POST', url: string, payload?: object, token = TEST_ADMIN_TOKEN) {
  return app.inject({ method, url, payload, headers: { authorization: `Bearer ${token}` } })
}

before(async () => {
  app = Fastify({ logger: false })
  await app.register(whatsappRoutes)
  await app.ready()
})
after(async () => {
  await app.close()
})

beforeEach(async () => {
  await resetDb()
  delete env.WHATSAPP_GROUP_JID
  conectado = true
  qr = undefined
  enviados = []
  reinicios = 0
})

describe('rotas de administração do WhatsApp', () => {
  test('exigem o token de administração (o do webhook não serve)', async () => {
    for (const [method, url] of [
      ['GET', '/whatsapp/status'],
      ['GET', '/whatsapp/groups'],
      ['PUT', '/whatsapp/grupo'],
      ['POST', '/whatsapp/test'],
      ['POST', '/whatsapp/desconectar'],
      ['GET', '/events'],
    ] as const) {
      const res = await req(method, url, method === 'GET' ? undefined : {}, TEST_SECRET)
      assert.equal(res.statusCode, 401, `${method} ${url}`)
    }
    assert.equal(reinicios, 0)
  })

  test('status conectado: conta, grupo e fila', async () => {
    const res = await req('GET', '/whatsapp/status')
    assert.equal(res.statusCode, 200)
    assert.deepEqual(res.json(), {
      status: 'open',
      connected: true,
      account: '5511900000000:3@s.whatsapp.net',
      qr: null,
      group: null,
      outbox: {},
    })
  })

  test('status aguardando pareamento devolve o QR', async () => {
    conectado = false
    qr = '2@abc,def'
    const body = (await req('GET', '/whatsapp/status')).json() as { status: string; qr: string; account: null }
    assert.equal(body.status, 'qr')
    assert.equal(body.qr, '2@abc,def')
    assert.equal(body.account, null)
  })

  test('grupo do ambiente aparece enquanto ninguém escolheu pela tela', async () => {
    env.WHATSAPP_GROUP_JID = TEST_GROUP_JID
    const body = (await req('GET', '/whatsapp/status')).json() as { group: unknown }
    assert.deepEqual(body.group, { jid: TEST_GROUP_JID, nome: null, origem: 'ambiente' })
  })

  test('escolher grupo grava jid e nome, e vale mais que o do ambiente', async () => {
    env.WHATSAPP_GROUP_JID = '5511900000000-1600000000@g.us'
    const res = await req('PUT', '/whatsapp/grupo', { jid: TEST_GROUP_JID })
    assert.equal(res.statusCode, 200)
    const esperado = { jid: TEST_GROUP_JID, nome: 'Reservas Chalés', origem: 'tela' }
    assert.deepEqual((res.json() as { group: unknown }).group, esperado)
    assert.deepEqual(((await req('GET', '/whatsapp/status')).json() as { group: unknown }).group, esperado)

    // Trocar de novo atualiza a mesma linha.
    await req('PUT', '/whatsapp/grupo', { jid: '5511900000000-1600000000@g.us' })
    const { rows } = await pool.query('SELECT grupo_jid, grupo_nome FROM checkin.configuracao')
    assert.deepEqual(rows, [{ grupo_jid: '5511900000000-1600000000@g.us', grupo_nome: 'Outro grupo' }])
  })

  test('recusa grupo de que o número não participa, jid inválido e WhatsApp desconectado', async () => {
    assert.equal((await req('PUT', '/whatsapp/grupo', { jid: '999-999@g.us' })).statusCode, 422)
    assert.equal((await req('PUT', '/whatsapp/grupo', { jid: '5511900000000@s.whatsapp.net' })).statusCode, 400)
    assert.equal((await req('PUT', '/whatsapp/grupo', {})).statusCode, 400)
    conectado = false
    assert.equal((await req('PUT', '/whatsapp/grupo', { jid: TEST_GROUP_JID })).statusCode, 503)
    const { rows } = await pool.query('SELECT 1 FROM checkin.configuracao')
    assert.equal(rows.length, 0)
  })

  test('mensagem de teste vai para o grupo escolhido', async () => {
    assert.equal((await req('POST', '/whatsapp/test')).statusCode, 400, 'sem grupo')
    await req('PUT', '/whatsapp/grupo', { jid: TEST_GROUP_JID })

    const res = await req('POST', '/whatsapp/test')
    assert.equal(res.statusCode, 200)
    assert.equal(enviados.length, 1)
    assert.equal(enviados[0]!.jid, TEST_GROUP_JID)
    assert.match(enviados[0]!.texto, /Teste do notificador/)

    conectado = false
    assert.equal((await req('POST', '/whatsapp/test')).statusCode, 503)
  })

  test('desconectar reinicia a sessão e mantém o grupo escolhido', async () => {
    await req('PUT', '/whatsapp/grupo', { jid: TEST_GROUP_JID })
    const res = await req('POST', '/whatsapp/desconectar')
    assert.equal(res.statusCode, 202)
    assert.equal(reinicios, 1)
    const body = (await req('GET', '/whatsapp/status')).json() as { qr: string; group: { jid: string } }
    assert.equal(body.qr, 'qr-novo')
    assert.equal(body.group.jid, TEST_GROUP_JID)
  })
})
