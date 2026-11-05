import './helpers/setup.js'
import test from 'node:test'
import assert from 'node:assert/strict'
import { criarGerente } from '../src/whatsapp/sessoes.js'
import { fabricaFalsa } from './helpers/dubles.js'
import type { BotDoWorker } from '../src/plataforma.js'

// O gerente: um processo, N sessões. O que importa aqui é que ele **persiga a lista da API** —
// bot ligado ganha sessão, bot desligado perde a sessão, e o teto é respeitado.

function bot(slug: string, ativo = true): BotDoWorker {
  return { slug, nome: `Bot ${slug}`, ativo, pausa_resposta_segundos: 0 }
}

test('abre sessão para cada bot ligado e nenhuma para os desligados', async () => {
  const { fabrica, criadas } = fabricaFalsa()
  const gerente = criarGerente({
    fabrica,
    buscarBots: async () => [bot('vendas'), bot('suporte'), bot('ferias', false)],
    entregar: async () => ({}),
  })

  await gerente.reconciliar()

  assert.deepEqual([...criadas.keys()].sort(), ['suporte', 'vendas'])
  assert.equal(criadas.get('ferias'), undefined, 'bot desligado não devia ganhar sessão')
})

test('bot que sai da lista tem a sessão encerrada, sem deslogar', async () => {
  const { fabrica, criadas } = fabricaFalsa()
  let lista = [bot('vendas'), bot('suporte')]
  const gerente = criarGerente({ fabrica, buscarBots: async () => lista, entregar: async () => ({}) })

  await gerente.reconciliar()
  const suporte = criadas.get('suporte')!

  // Alguém desligou o bot na tela.
  lista = [bot('vendas'), bot('suporte', false)]
  await gerente.reconciliar()

  assert.ok(suporte.foiEncerrada(), 'a sessão devia ser encerrada')
  assert.equal(suporte.vezesReiniciada(), 0, 'encerrar não é deslogar: a sessão continua válida')
  assert.equal(gerente.de('suporte'), undefined)
  assert.ok(gerente.de('vendas'), 'o outro bot não devia ser afetado')
})

test('reconciliar duas vezes não abre a sessão de novo', async () => {
  const { fabrica, criadas } = fabricaFalsa()
  const gerente = criarGerente({ fabrica, buscarBots: async () => [bot('vendas')], entregar: async () => ({}) })

  await gerente.reconciliar()
  const primeira = criadas.get('vendas')
  await gerente.reconciliar()

  assert.equal(criadas.size, 1)
  assert.equal(criadas.get('vendas'), primeira, 'devia ser a mesma sessão')
})

// O teto existe porque cada sessão do Baileys custa memória e a VM tem 2 GB: é melhor um bot
// aparecer sem WhatsApp na tela do que o processo morrer no meio de uma conversa.
test('respeita o teto de sessões e diz quem ficou de fora', async () => {
  const { fabrica, criadas } = fabricaFalsa()
  const gerente = criarGerente({
    fabrica,
    // MAX_SESSOES=3 no setup dos testes.
    buscarBots: async () => [bot('a'), bot('b'), bot('c'), bot('d'), bot('e')],
    entregar: async () => ({}),
  })

  await gerente.reconciliar()

  assert.equal(criadas.size, 3)
  assert.deepEqual(gerente.semSessao(), ['d', 'e'])
})

test('entrega à plataforma o que chega, com o nome do contato', async () => {
  const { fabrica, criadas } = fabricaFalsa()
  const recebidas: unknown[] = []
  const gerente = criarGerente({
    fabrica,
    buscarBots: async () => [bot('vendas')],
    entregar: async (m) => {
      recebidas.push(m)
      return { conversa_id: 7, status: 'pendente', motivo: '', encerrada: false }
    },
  })
  await gerente.reconciliar()

  await criadas.get('vendas')!.simularMensagem({
    telefone: '5511900000000',
    contato: 'Fulano',
    texto: 'quero alugar',
  })

  assert.deepEqual(recebidas, [
    { bot: 'vendas', telefone: '5511900000000', contato: 'Fulano', texto: 'quero alugar' },
  ])
})

// Se a plataforma recusa, não há resposta a dar — mas o worker não pode cair por isso.
test('plataforma fora do ar não derruba o worker', async () => {
  const { fabrica, criadas } = fabricaFalsa()
  const gerente = criarGerente({
    fabrica,
    buscarBots: async () => [bot('vendas')],
    entregar: async () => {
      throw new Error('API fora do ar')
    },
  })
  await gerente.reconciliar()

  await criadas.get('vendas')!.simularMensagem({ telefone: '5511900000000', texto: 'oi' })
  // Chegar aqui sem exceção é o teste.
  assert.ok(gerente.de('vendas'))
})

test('conectados lista só as sessões abertas: é o que a fila pergunta', async () => {
  const { fabrica, criadas } = fabricaFalsa()
  const gerente = criarGerente({
    fabrica,
    buscarBots: async () => [bot('vendas'), bot('suporte')],
    entregar: async () => ({}),
  })
  await gerente.reconciliar()
  // conectar() do dublê é síncrono o bastante, mas o gerente o chama sem await.
  await new Promise((r) => setTimeout(r, 5))

  assert.deepEqual(gerente.conectados().sort(), ['suporte', 'vendas'])

  criadas.get('suporte')!.derrubar()
  assert.deepEqual(gerente.conectados(), ['vendas'])
})

test('o status traz o que a tela mostra de cada sessão', async () => {
  const { fabrica, criadas } = fabricaFalsa()
  const gerente = criarGerente({ fabrica, buscarBots: async () => [bot('vendas')], entregar: async () => ({}) })
  await gerente.reconciliar()
  await new Promise((r) => setTimeout(r, 5))

  const status = gerente.status()
  assert.equal(status.length, 1)
  assert.equal(status[0]!.bot, 'vendas')
  assert.equal(status[0]!.estado, 'aberta')
  assert.equal(status[0]!.conectado, true)
  assert.ok(status[0]!.numero, 'com a conexão aberta, a tela mostra o número')

  criadas.get('vendas')!.derrubar()
  assert.equal(gerente.status()[0]!.conectado, false)
  assert.equal(gerente.status()[0]!.numero, null)
})
