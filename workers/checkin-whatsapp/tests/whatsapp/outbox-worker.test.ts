import { resetDb, seedEvent, getOutboxRow, setNextAttempt, pool } from '../helpers/db.js'
import { test, describe, beforeEach, afterEach, mock } from 'node:test'
import assert from 'node:assert/strict'
import { enqueue, MAX_ATTEMPTS } from '../../src/db/outbox.js'

const JID = '1234567890-1234567890@g.us'

/**
 * O worker fala com `client.js` e `sender.js` — ambos trocados por dublês aqui.
 * Exige `--experimental-test-module-mocks` (já no script `npm test`).
 */
let conectado = true
let enviados: { jid: string; body: string }[] = []
let falharCom: Error | undefined
/** Chamado depois de cada envio — usado para derrubar a conexão no meio do lote. */
let aposEnvio: (() => void | Promise<void>) | undefined

mock.module('../../src/whatsapp/client.js', {
  exports: {
    isConnected: () => conectado,
    getStatus: () => (conectado ? 'open' : 'disconnected'),
  },
})

mock.module('../../src/whatsapp/sender.js', {
  exports: {
    sendText: async (jid: string, body: string) => {
      if (falharCom) throw falharCom
      enviados.push({ jid, body })
      await aposEnvio?.()
      return 'msg-id'
    },
  },
})

const { startOutboxWorker, stopOutboxWorker, aguardarCiclo } = await import('../../src/whatsapp/outbox.js')

const TICK_MS = 5_000

/** Avança um tick do worker e espera o ciclo assíncrono (envios e banco) terminar. */
async function tick(): Promise<void> {
  mock.timers.tick(TICK_MS)
  await aguardarCiclo()
}

async function enfileira(body: string): Promise<number> {
  return enqueue({ eventId: await seedEvent(), targetJid: JID, body })
}

describe('worker do outbox', () => {
  beforeEach(async () => {
    await resetDb()
    conectado = true
    enviados = []
    falharCom = undefined
    aposEnvio = undefined
    mock.timers.enable({ apis: ['setInterval'] })
    startOutboxWorker()
  })

  afterEach(async () => {
    await stopOutboxWorker()
    mock.timers.reset()
  })

  test('envia as pendentes e marca como sent', async () => {
    const id = await enfileira('olá grupo')
    await tick()

    assert.deepEqual(enviados, [{ jid: JID, body: 'olá grupo' }])
    assert.equal((await getOutboxRow(id)).status, 'enviada')
    assert.equal((await getOutboxRow(id)).tentativas, 1)
  })

  test('não tenta nada enquanto o WhatsApp está desconectado', async () => {
    const id = await enfileira('presa')
    conectado = false
    await tick()

    assert.deepEqual(enviados, [])
    const row = await getOutboxRow(id)
    assert.equal(row.status, 'pendente')
    assert.equal(row.tentativas, 0, 'ficar offline não pode gastar tentativa')
  })

  test('a mensagem presa sai assim que a conexão volta', async () => {
    const id = await enfileira('presa')
    conectado = false
    await tick()
    conectado = true
    await tick()

    assert.equal(enviados.length, 1)
    assert.equal((await getOutboxRow(id)).status, 'enviada')
  })

  test('falha no envio reagenda com backoff em vez de perder a mensagem', async () => {
    const id = await enfileira('vai falhar')
    falharCom = new Error('timeout do socket')
    await tick()

    const row = await getOutboxRow(id)
    assert.equal(row.status, 'pendente')
    assert.equal(row.tentativas, 1)
    assert.equal(row.ultimo_erro, 'timeout do socket')
    assert.ok(row.proxima_tentativa_em.getTime() > Date.now(), 'deve ficar agendada para o futuro')
  })

  test('o backoff segura a mensagem até a hora marcada', async () => {
    const id = await enfileira('vai falhar')
    falharCom = new Error('timeout')
    await tick()

    // Tick imediatamente depois: ainda dentro dos 5s de espera.
    await tick()
    assert.equal((await getOutboxRow(id)).tentativas, 1, 'não deve tentar de novo antes da hora')
  })

  test('esgotadas as tentativas, marca falhou e para de tentar', async () => {
    const id = await enfileira('sempre falha')
    falharCom = new Error('destino inválido')

    for (let i = 0; i < MAX_ATTEMPTS; i++) {
      // Antecipa o agendamento: o backoff usa o relógio real do banco.
      await setNextAttempt(id, new Date(Date.now() - 1000))
      await tick()
    }

    const row = await getOutboxRow(id)
    assert.equal(row.status, 'falhou')
    assert.equal(row.tentativas, MAX_ATTEMPTS)

    // E não volta mais para a fila, mesmo com o agendamento no passado.
    await setNextAttempt(id, new Date(Date.now() - 1000))
    await tick()
    assert.equal((await getOutboxRow(id)).tentativas, MAX_ATTEMPTS)
  })

  test('processa o lote em ordem', async () => {
    await enfileira('primeira')
    await enfileira('segunda')
    await enfileira('terceira')
    await tick()

    assert.deepEqual(
      enviados.map((e) => e.body),
      ['primeira', 'segunda', 'terceira'],
    )
  })

  test('conexão caindo no meio do lote não gasta tentativa das restantes', async () => {
    const ids = [await enfileira('a'), await enfileira('b'), await enfileira('c')]
    // Derruba a conexão logo depois do primeiro envio; o segundo falha e o
    // worker interrompe o lote em vez de queimar as tentativas restantes.
    aposEnvio = () => {
      conectado = false
      falharCom = new Error('conexão perdida')
    }
    await tick()

    assert.equal(enviados.length, 1)
    assert.equal((await getOutboxRow(ids[0]!)).status, 'enviada')
    assert.equal((await getOutboxRow(ids[1]!)).tentativas, 1, 'a que estava em voo conta a tentativa')
    assert.equal((await getOutboxRow(ids[2]!)).tentativas, 0, 'as seguintes ficam intactas')
    assert.equal((await getOutboxRow(ids[2]!)).status, 'pendente')
  })

  test('startOutboxWorker é idempotente', async () => {
    startOutboxWorker()
    startOutboxWorker()
    await enfileira('uma vez só')
    await tick()

    assert.equal(enviados.length, 1, 'não deve haver dois timers enviando')
  })

  test('stopOutboxWorker interrompe o ciclo', async () => {
    await stopOutboxWorker()
    await enfileira('não deve sair')
    await tick()

    assert.deepEqual(enviados, [])
  })

  test('envio que sai mas não é registrado no banco não é reenviado', async () => {
    const primeira = await enfileira('primeira')
    const segunda = await enfileira('segunda')

    // O Postgres "cai" logo depois do primeiro envio: o UPDATE para enviada falha.
    const queryOriginal = pool.query.bind(pool)
    let bancoFora = false
    aposEnvio = () => {
      bancoFora = true
    }
    const falso = mock.method(pool, 'query', ((...args: Parameters<typeof pool.query>) => {
      if (bancoFora) return Promise.reject(new Error('conexão com o Postgres perdida'))
      return (queryOriginal as (...a: unknown[]) => unknown)(...args)
    }) as typeof pool.query)

    await tick()
    assert.deepEqual(enviados.map((e) => e.body), ['primeira'], 'para o lote na falha do banco')

    // Banco de volta: registra a primeira sem reenviar e segue com a segunda.
    bancoFora = false
    aposEnvio = undefined
    falso.mock.restore()
    await tick()

    assert.deepEqual(enviados.map((e) => e.body), ['primeira', 'segunda'])
    assert.equal((await getOutboxRow(primeira)).status, 'enviada')
    assert.equal((await getOutboxRow(primeira)).tentativas, 1)
    assert.equal((await getOutboxRow(segunda)).status, 'enviada')
  })
})
