import './helpers/setup.js'
import test, { after, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import Fastify from 'fastify'
import { TOKEN_ADMIN_TESTE } from './helpers/setup.js'
import { limparBanco, pool } from './helpers/db.js'
import { exigirTokenAdmin } from '../src/routes/auth.js'

beforeEach(limparBanco)
after(async () => {
  await pool.end()
})

async function appProtegido() {
  const app = Fastify({ logger: false })
  app.get('/protegida', { preHandler: exigirTokenAdmin }, async () => ({ ok: true }))
  await app.ready()
  return app
}

test('rota de administração exige o token', async () => {
  const app = await appProtegido()
  try {
    assert.equal((await app.inject({ method: 'GET', url: '/protegida' })).statusCode, 401)
    assert.equal(
      (await app.inject({ method: 'GET', url: '/protegida', headers: { 'x-admin-token': 'errado' } })).statusCode,
      401,
    )
    // Os dois formatos que a API pode usar.
    assert.equal(
      (await app.inject({ method: 'GET', url: '/protegida', headers: { 'x-admin-token': TOKEN_ADMIN_TESTE } })).statusCode,
      200,
    )
    assert.equal(
      (await app.inject({
        method: 'GET',
        url: '/protegida',
        headers: { authorization: `Bearer ${TOKEN_ADMIN_TESTE}` },
      })).statusCode,
      200,
    )
  } finally {
    await app.close()
  }
})

// O teste de envio entra na fila (e não manda nada na hora): é o mesmo caminho do envio de
// verdade, então respeita a pausa e os freios.
test('POST /whatsapp/teste enfileira em vez de enviar', async () => {
  const { rotasWhatsapp } = await import('../src/routes/whatsapp.js')
  const app = Fastify({ logger: false })
  await app.register(rotasWhatsapp)
  await app.ready()
  try {
    const recusado = await app.inject({
      method: 'POST',
      url: '/whatsapp/teste',
      headers: { 'x-admin-token': TOKEN_ADMIN_TESTE },
      payload: { telefone: '(11) 90000-0000' },
    })
    assert.equal(recusado.statusCode, 400, 'a API manda o telefone já normalizado, só com dígitos')

    const aceito = await app.inject({
      method: 'POST',
      url: '/whatsapp/teste',
      headers: { 'x-admin-token': TOKEN_ADMIN_TESTE },
      payload: { telefone: '5511900000000' },
    })
    assert.equal(aceito.statusCode, 202)

    const { rows } = await pool.query<{ tipo: string; status: string; destino_telefone: string }>(
      `SELECT tipo, status, destino_telefone FROM boletos.mensagens`,
    )
    assert.equal(rows.length, 1)
    assert.equal(rows[0]!.tipo, 'teste')
    assert.equal(rows[0]!.status, 'pendente')
  } finally {
    await app.close()
  }
})

test('desconectar exige confirmação explícita', async () => {
  const { rotasWhatsapp } = await import('../src/routes/whatsapp.js')
  const app = Fastify({ logger: false })
  await app.register(rotasWhatsapp)
  await app.ready()
  try {
    const sem = await app.inject({
      method: 'POST',
      url: '/whatsapp/desconectar',
      headers: { 'x-admin-token': TOKEN_ADMIN_TESTE },
      payload: {},
    })
    assert.equal(sem.statusCode, 400)
  } finally {
    await app.close()
  }
})
