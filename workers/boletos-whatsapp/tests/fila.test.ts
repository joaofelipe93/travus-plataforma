import './helpers/setup.js'
import test, { after, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import { buscar, carteiroFalso, desligarEnvio, enfileirar, limparBanco, pool } from './helpers/db.js'
import { rodarUmCiclo, _zerarEstado } from '../src/whatsapp/fila.js'
import { SemWhatsappError, NaoConectadoError } from '../src/whatsapp/sender.js'

beforeEach(async () => {
  await limparBanco()
  _zerarEstado()
})

after(async () => {
  await pool.end()
})

test('manda o boleto como documento e solta o anexo depois', async () => {
  const id = await enfileirar({})
  const carteiro = carteiroFalso()

  await rodarUmCiclo(carteiro)

  assert.equal(carteiro.enviados.length, 1)
  assert.equal(carteiro.enviados[0]!.telefone, '5511900000000')
  assert.ok((carteiro.enviados[0]!.anexo ?? 0) > 0, 'o PDF devia ir como anexo')

  const m = await buscar(id)
  assert.equal(m.status, 'enviada')
  assert.equal(m.tentativas, 1)
  assert.ok(m.enviada_em)
  // O anexo sai do banco: a cópia na fila só existe enquanto a mensagem está pendente.
  assert.equal(m.anexo_pdf, null)
})

test('cobrança sem anexo vai como texto', async () => {
  await enfileirar({ tipo: 'cobranca', anexo: null, texto: 'Oi Cliente, ainda está disponível para pagamento.' })
  const carteiro = carteiroFalso()

  await rodarUmCiclo(carteiro)

  assert.equal(carteiro.enviados.length, 1)
  assert.equal(carteiro.enviados[0]!.anexo, null)
})

test('desconectado não gasta tentativa', async () => {
  const id = await enfileirar({})
  const carteiro = carteiroFalso({ conectado: false })

  await rodarUmCiclo(carteiro)

  assert.equal(carteiro.enviados.length, 0)
  const m = await buscar(id)
  assert.equal(m.status, 'pendente')
  assert.equal(m.tentativas, 0, 'sem conexão a mensagem espera, sem queimar tentativa')
})

test('envio desligado na configuração segura a fila', async () => {
  const id = await enfileirar({})
  await desligarEnvio()
  const carteiro = carteiroFalso()

  await rodarUmCiclo(carteiro)

  assert.equal(carteiro.enviados.length, 0)
  assert.equal((await buscar(id)).status, 'pendente')
})

test('número sem WhatsApp é falha definitiva, sem gastar as cinco tentativas', async () => {
  const id = await enfileirar({})
  const carteiro = carteiroFalso({ falhar: new SemWhatsappError('5511900000000') })

  await rodarUmCiclo(carteiro)

  const m = await buscar(id)
  assert.equal(m.status, 'falhou')
  assert.equal(m.tentativas, 1)
  assert.match(m.ultimo_erro ?? '', /não tem WhatsApp/)
  // O telefone no erro sai mascarado: é dado de cliente e esse texto aparece na tela.
  assert.doesNotMatch(m.ultimo_erro ?? '', /5511900000000/)
  assert.equal(m.anexo_pdf, null, 'ninguém mais vai mandar esta mensagem: o anexo sai do banco')
})

test('falha de rede reagenda com espera', async () => {
  const id = await enfileirar({})
  const carteiro = carteiroFalso({ falhar: new Error('timeout no WhatsApp') })

  await rodarUmCiclo(carteiro)

  const m = await buscar(id)
  assert.equal(m.status, 'pendente')
  assert.equal(m.tentativas, 1)
  assert.match(m.ultimo_erro ?? '', /timeout/)
  assert.ok(m.proxima_tentativa_em.getTime() > Date.now(), 'a próxima tentativa fica no futuro')
  // Ainda pendente: o anexo continua, senão a retentativa iria sem o boleto.
  assert.ok(m.anexo_pdf && m.anexo_pdf.length > 0)
})

test('desistir depois de cinco tentativas', async () => {
  const id = await enfileirar({})
  const carteiro = carteiroFalso({ falhar: new Error('WhatsApp recusou') })

  for (let i = 0; i < 5; i++) {
    // Cada ciclo precisa que a mensagem esteja elegível: o backoff é encurtado à mão.
    await pool.query(`UPDATE boletos.mensagens SET proxima_tentativa_em = now() WHERE id = $1`, [id])
    _zerarEstado()
    await rodarUmCiclo(carteiro)
  }

  const m = await buscar(id)
  assert.equal(m.status, 'falhou')
  assert.equal(m.tentativas, 5)
})

test('respeita a ordem e a pausa entre mensagens', async () => {
  // A pausa do banco é 5s no helper; aqui ela é baixada para o teste não esperar.
  await pool.query(`UPDATE boletos.configuracao SET pausa_envio_segundos = 5 WHERE id`)
  const primeiro = await enfileirar({ telefone: '5511900000001' })
  const segundo = await enfileirar({ telefone: '5511900000002' })
  const carteiro = carteiroFalso()

  const inicio = Date.now()
  await rodarUmCiclo(carteiro)
  const duracao = Date.now() - inicio

  assert.deepEqual(carteiro.enviados.map((e) => e.telefone), ['5511900000001', '5511900000002'])
  assert.ok(duracao >= 5_000, `a pausa entre mensagens precisa valer (levou ${duracao}ms)`)
  assert.equal((await buscar(primeiro)).status, 'enviada')
  assert.equal((await buscar(segundo)).status, 'enviada')
})

test('conexão que cai no meio do lote para o ciclo sem queimar a próxima', async () => {
  await pool.query(`UPDATE boletos.configuracao SET pausa_envio_segundos = 5 WHERE id`)
  const primeiro = await enfileirar({ telefone: '5511900000001' })
  const segundo = await enfileirar({ telefone: '5511900000002' })

  let conectado = true
  const carteiro = {
    enviados: [] as { telefone: string }[],
    conectado: () => conectado,
    situacao: () => (conectado ? 'open' : 'disconnected'),
    async enviarTexto() {
      return 'id'
    },
    async enviarDocumento(telefone: string) {
      carteiro.enviados.push({ telefone })
      conectado = false // cai depois da primeira
      return 'id'
    },
  }

  await rodarUmCiclo(carteiro)

  assert.equal(carteiro.enviados.length, 1)
  assert.equal((await buscar(primeiro)).status, 'enviada')
  const parada = await buscar(segundo)
  assert.equal(parada.status, 'pendente')
  assert.equal(parada.tentativas, 0)
})

test('mensagem de teste usa o mesmo caminho da fila', async () => {
  const id = await enfileirar({ tipo: 'teste', anexo: null, texto: 'teste' })
  const carteiro = carteiroFalso()
  await rodarUmCiclo(carteiro)
  assert.equal((await buscar(id)).status, 'enviada')
  assert.equal(carteiro.enviados.length, 1)
})

test('NaoConectadoError no meio do envio não gasta tentativa', async () => {
  const id = await enfileirar({})
  const carteiro = carteiroFalso({ falhar: new NaoConectadoError() })
  await rodarUmCiclo(carteiro)
  const m = await buscar(id)
  assert.equal(m.status, 'pendente')
  assert.equal(m.tentativas, 0)
})
