import './helpers/setup.js'
import test from 'node:test'
import assert from 'node:assert/strict'
import { TOKEN_ADMIN_TESTE } from './helpers/setup.js'
import { buildServer } from '../src/server.js'
import { criarGerente } from '../src/whatsapp/sessoes.js'
import { fabricaFalsa, type SessaoFalsa } from './helpers/dubles.js'

// As rotas de administração. Elas devolvem o QR e mandam mensagem para telefone de verdade:
// sem o token, nada. **Nenhuma delas vai para o gateway.**

async function servidor(bots: string[] = ['vendas']) {
  const { fabrica, criadas } = fabricaFalsa()
  const gerente = criarGerente({
    fabrica,
    buscarBots: async () => bots.map((slug) => ({ slug, nome: slug, ativo: true, pausa_resposta_segundos: 0 })),
    entregar: async () => ({}),
  })
  await gerente.reconciliar()
  await new Promise((r) => setTimeout(r, 5))
  const app = buildServer(gerente)
  await app.ready()
  return { app, gerente, criadas }
}

test('as rotas de administração exigem o token', async () => {
  const { app } = await servidor()
  try {
    assert.equal((await app.inject({ method: 'GET', url: '/whatsapp/status' })).statusCode, 401)
    assert.equal(
      (await app.inject({ method: 'GET', url: '/whatsapp/status', headers: { 'x-admin-token': 'errado' } })).statusCode,
      401,
    )
    // Os dois formatos que a API pode usar.
    for (const headers of [
      { 'x-admin-token': TOKEN_ADMIN_TESTE },
      { authorization: `Bearer ${TOKEN_ADMIN_TESTE}` },
    ]) {
      assert.equal((await app.inject({ method: 'GET', url: '/whatsapp/status', headers })).statusCode, 200)
    }
  } finally {
    await app.close()
  }
})

test('o status descreve cada sessão e quem ficou sem', async () => {
  const { app } = await servidor(['vendas', 'suporte'])
  try {
    const r = await app.inject({
      method: 'GET',
      url: '/whatsapp/status',
      headers: { 'x-admin-token': TOKEN_ADMIN_TESTE },
    })
    const corpo = r.json() as {
      max_sessoes: number
      sem_sessao: string[]
      sessoes: { bot: string; conectado: boolean; qr: string | null }[]
    }
    assert.equal(corpo.max_sessoes, 3)
    assert.deepEqual(corpo.sem_sessao, [])
    assert.deepEqual(corpo.sessoes.map((s) => s.bot).sort(), ['suporte', 'vendas'])
    assert.ok(corpo.sessoes.every((s) => s.conectado))
  } finally {
    await app.close()
  }
})

test('teste e desconectar recusam bot sem sessão', async () => {
  const { app } = await servidor()
  try {
    for (const url of ['/whatsapp/nao-existe/teste', '/whatsapp/nao-existe/desconectar']) {
      const r = await app.inject({
        method: 'POST',
        url,
        headers: { 'x-admin-token': TOKEN_ADMIN_TESTE },
        payload: { telefone: '5511900000000', confirmar: true },
      })
      assert.equal(r.statusCode, 409, url)
      assert.equal((r.json() as { error: string }).error, 'sem_sessao')
    }
  } finally {
    await app.close()
  }
})

test('a mensagem de teste sai pelo número do bot', async () => {
  const { app, criadas } = await servidor()
  try {
    const r = await app.inject({
      method: 'POST',
      url: '/whatsapp/vendas/teste',
      headers: { 'x-admin-token': TOKEN_ADMIN_TESTE },
      payload: { telefone: '5511987654321' },
    })
    assert.equal(r.statusCode, 202)
    const sessao = criadas.get('vendas') as SessaoFalsa
    assert.equal(sessao.enviados.length, 1)
    assert.equal(sessao.enviados[0]!.telefone, '5511987654321')
    assert.match(sessao.enviados[0]!.texto, /teste/i)
  } finally {
    await app.close()
  }
})

test('telefone inválido no teste é 400, não mensagem perdida', async () => {
  const { app, criadas } = await servidor()
  try {
    const r = await app.inject({
      method: 'POST',
      url: '/whatsapp/vendas/teste',
      headers: { 'x-admin-token': TOKEN_ADMIN_TESTE },
      payload: { telefone: '11987654321' }, // sem DDI
    })
    assert.equal(r.statusCode, 400)
    assert.equal((criadas.get('vendas') as SessaoFalsa).enviados.length, 0)
  } finally {
    await app.close()
  }
})

test('desconectar exige confirmação e só mexe no bot pedido', async () => {
  const { app, criadas } = await servidor(['vendas', 'suporte'])
  try {
    const semConfirmar = await app.inject({
      method: 'POST',
      url: '/whatsapp/vendas/desconectar',
      headers: { 'x-admin-token': TOKEN_ADMIN_TESTE },
      payload: {},
    })
    assert.equal(semConfirmar.statusCode, 400)

    const ok = await app.inject({
      method: 'POST',
      url: '/whatsapp/vendas/desconectar',
      headers: { 'x-admin-token': TOKEN_ADMIN_TESTE },
      payload: { confirmar: true },
    })
    assert.equal(ok.statusCode, 200)
    assert.equal((criadas.get('vendas') as SessaoFalsa).vezesReiniciada(), 1)
    assert.equal((criadas.get('suporte') as SessaoFalsa).vezesReiniciada(), 0, 'o outro número não se mexe')
  } finally {
    await app.close()
  }
})

test('/health responde sem token (é o healthcheck do container)', async () => {
  const { app } = await servidor()
  try {
    const r = await app.inject({ method: 'GET', url: '/health' })
    assert.equal(r.statusCode, 200)
    const corpo = r.json() as { servico: string; sessoes: number; conectadas: number }
    assert.equal(corpo.servico, 'atendimento-whatsapp')
    assert.equal(corpo.sessoes, 1)
    assert.equal(corpo.conectadas, 1)
  } finally {
    await app.close()
  }
})
