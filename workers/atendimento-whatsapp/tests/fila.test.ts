import './helpers/setup.js'
import test from 'node:test'
import assert from 'node:assert/strict'
import { criarLacoDaFila, type ApiDaFila } from '../src/fila.js'
import { criarGerente } from '../src/whatsapp/sessoes.js'
import { NaoConectadoError, SemWhatsappError } from '../src/whatsapp/sessao.js'
import { fabricaFalsa, type SessaoFalsa } from './helpers/dubles.js'
import type { MensagemParaEnviar } from '../src/plataforma.js'

// O laço de envio. O que está guardado aqui é o que não pode mudar sem que alguém receba a mesma
// mensagem duas vezes — ou não receba nada e ninguém saiba.

type Registro = { id: number; erro?: string; tentativa?: number; definitivo?: boolean }

function apiFalsa(mensagens: MensagemParaEnviar[], opcoes?: { falharAoRegistrar?: boolean }) {
  const enviadas: Registro[] = []
  const falhas: Registro[] = []
  let pedidos = 0
  let falharAoRegistrar = opcoes?.falharAoRegistrar ?? false
  const api: ApiDaFila = {
    proximasMensagens: async () => {
      pedidos += 1
      // Entrega a fila só no primeiro pedido: o ciclo não deve ficar reenviando a mesma coisa.
      return pedidos === 1 ? mensagens : []
    },
    marcarEnviada: async (id) => {
      if (falharAoRegistrar) throw new Error('API fora do ar')
      enviadas.push({ id })
    },
    marcarFalha: async (id, erro, tentativa, definitivo) => {
      falhas.push({ id, erro, tentativa, definitivo })
    },
  }
  return {
    api,
    enviadas,
    falhas,
    pedidos: () => pedidos,
    voltarAFuncionar: () => {
      falharAoRegistrar = false
    },
  }
}

function mensagem(parcial: Partial<MensagemParaEnviar> = {}): MensagemParaEnviar {
  return {
    id: 1,
    conversa_id: 10,
    bot: 'vendas',
    telefone: '5511900000000',
    texto: 'Oi! Em qual bairro você procura?',
    tentativa: 1,
    ...parcial,
  }
}

async function comUmBot() {
  const { fabrica, criadas } = fabricaFalsa()
  const gerente = criarGerente({
    fabrica,
    buscarBots: async () => [{ slug: 'vendas', nome: 'Vendas', ativo: true, pausa_resposta_segundos: 0 }],
    entregar: async () => ({}),
  })
  await gerente.reconciliar()
  await new Promise((r) => setTimeout(r, 5))
  return { gerente, sessao: criadas.get('vendas') as SessaoFalsa }
}

test('envia a resposta pela sessão do bot e registra', async () => {
  const { gerente, sessao } = await comUmBot()
  const falsa = apiFalsa([mensagem()])
  const laco = criarLacoDaFila(gerente, { api: falsa.api, pausaEntreMensagensMs: 0 })

  await laco.ciclo()

  assert.equal(sessao.enviados.length, 1)
  assert.equal(sessao.enviados[0]!.telefone, '5511900000000')
  assert.deepEqual(falsa.enviadas, [{ id: 1 }])
  assert.deepEqual(falsa.falhas, [])
})

test('sem sessão aberta não pede fila: a resposta espera', async () => {
  const { gerente, sessao } = await comUmBot()
  sessao.derrubar()
  const falsa = apiFalsa([mensagem()])
  const laco = criarLacoDaFila(gerente, { api: falsa.api, pausaEntreMensagensMs: 0 })

  await laco.ciclo()

  assert.equal(falsa.pedidos(), 0, 'nem devia perguntar à API')
  assert.equal(sessao.enviados.length, 0)
})

// Número sem WhatsApp não melhora tentando de novo: é falha definitiva e vai para a tela.
test('número sem WhatsApp é falha definitiva', async () => {
  const { gerente, sessao } = await comUmBot()
  sessao.falharCom(new SemWhatsappError('5511900000000'))
  const falsa = apiFalsa([mensagem({ tentativa: 1 })])
  const laco = criarLacoDaFila(gerente, { api: falsa.api, pausaEntreMensagensMs: 0 })

  await laco.ciclo()

  assert.equal(falsa.falhas.length, 1)
  assert.equal(falsa.falhas[0]!.definitivo, true)
  assert.deepEqual(falsa.enviadas, [])
})

test('desconexão no meio do envio não gasta tentativa', async () => {
  const { gerente, sessao } = await comUmBot()
  sessao.falharCom(new NaoConectadoError('vendas'))
  const falsa = apiFalsa([mensagem({ tentativa: 2 })])
  const laco = criarLacoDaFila(gerente, { api: falsa.api, pausaEntreMensagensMs: 0 })

  await laco.ciclo()

  assert.equal(falsa.falhas.length, 1)
  assert.equal(falsa.falhas[0]!.definitivo, false)
  assert.equal(falsa.falhas[0]!.tentativa, 0, 'tentativa 0: a trava da API devolve a mensagem sem puni-la')
})

/**
 * O caso que justifica o `enviadasSemRegistro`: a mensagem saiu no WhatsApp e a API caiu antes de
 * registrar. Reenviar seria mandar a mesma coisa duas vezes para a pessoa.
 */
test('resposta que saiu sem registro não é reenviada, e o registro é retomado', async () => {
  const { gerente, sessao } = await comUmBot()
  const falsa = apiFalsa([mensagem()], { falharAoRegistrar: true })
  const laco = criarLacoDaFila(gerente, { api: falsa.api, pausaEntreMensagensMs: 0 })

  await laco.ciclo()
  assert.equal(sessao.enviados.length, 1, 'saiu uma vez')
  assert.equal(laco.pendenciasDeRegistro(), 1, 'o id ficou guardado')

  // Próximo ciclo com a API de volta: registra o que faltava e não envia de novo.
  falsa.voltarAFuncionar()
  await laco.ciclo()

  assert.equal(sessao.enviados.length, 1, 'não podia sair de novo')
  assert.deepEqual(falsa.enviadas, [{ id: 1 }])
  assert.equal(laco.pendenciasDeRegistro(), 0)
})

test('manda uma por vez, na ordem em que a API entregou', async () => {
  const { gerente, sessao } = await comUmBot()
  const falsa = apiFalsa([
    mensagem({ id: 1, texto: 'primeira' }),
    mensagem({ id: 2, texto: 'segunda' }),
    mensagem({ id: 3, texto: 'terceira' }),
  ])
  const laco = criarLacoDaFila(gerente, { api: falsa.api, pausaEntreMensagensMs: 0 })

  await laco.ciclo()

  assert.deepEqual(
    sessao.enviados.map((e) => e.texto),
    ['primeira', 'segunda', 'terceira'],
  )
  assert.deepEqual(falsa.enviadas.map((e) => e.id), [1, 2, 3])
})

test('a sessão que cai no meio do lote interrompe o ciclo em vez de insistir', async () => {
  const { gerente, sessao } = await comUmBot()
  const falsa = apiFalsa([mensagem({ id: 1 }), mensagem({ id: 2 })])
  const laco = criarLacoDaFila(gerente, { api: falsa.api, pausaEntreMensagensMs: 0 })
  // Derruba a sessão depois da primeira: o envio seguinte não tem para onde ir.
  const original = sessao.enviarTexto
  let enviou = 0
  sessao.enviarTexto = async (telefone: string, texto: string) => {
    enviou += 1
    const r = await original(telefone, texto)
    if (enviou === 1) sessao.derrubar()
    return r
  }

  await laco.ciclo()

  assert.equal(sessao.enviados.length, 1)
  // A segunda volta à fila sem gastar tentativa.
  assert.equal(falsa.falhas.length, 1)
  assert.equal(falsa.falhas[0]!.id, 2)
  assert.equal(falsa.falhas[0]!.definitivo, false)
})

test('mensagem de bot sem sessão volta à fila em vez de ser dada por entregue', async () => {
  const { gerente } = await comUmBot()
  const falsa = apiFalsa([mensagem({ id: 9, bot: 'outro-bot' })])
  const laco = criarLacoDaFila(gerente, { api: falsa.api, pausaEntreMensagensMs: 0 })

  await laco.ciclo()

  assert.deepEqual(falsa.enviadas, [])
  assert.equal(falsa.falhas.length, 1)
  assert.equal(falsa.falhas[0]!.id, 9)
  assert.equal(falsa.falhas[0]!.definitivo, false)
})
