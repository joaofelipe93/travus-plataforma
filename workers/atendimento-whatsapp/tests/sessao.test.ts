import './helpers/setup.js'
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  criarGuardaDeEnviadas,
  ehLid,
  enderecoDaConversa,
  jidDe,
  mascarar,
  telefoneDeQuemEscreveu,
  telefoneDoJid,
  textoDaMensagem,
} from '../src/whatsapp/sessao.js'
import type { proto } from 'baileys'

// As funções puras da sessão. Duas delas decidem o que o bot NÃO responde — e isso é o que
// separa um atendimento de um robô falando em grupo alheio.

test('só conversa privada vira telefone', () => {
  assert.equal(telefoneDoJid('5511900000000@s.whatsapp.net'), '5511900000000')
  // Com o sufixo de aparelho, que o WhatsApp às vezes manda.
  assert.equal(telefoneDoJid('5511900000000:12@s.whatsapp.net'), '5511900000000')
  // O que o bot ignora:
  assert.equal(telefoneDoJid('123456789-987654321@g.us'), undefined, 'grupo')
  assert.equal(telefoneDoJid('status@broadcast'), undefined, 'status')
  assert.equal(telefoneDoJid('123@newsletter'), undefined, 'canal')
  assert.equal(telefoneDoJid(undefined), undefined)
  assert.equal(telefoneDoJid(null), undefined)
  assert.equal(telefoneDoJid('123@s.whatsapp.net'), undefined, 'curto demais para ser telefone')
})

test('mensagem endereçada por LID vira telefone pelo sender_pn', () => {
  // O caso que quebrou de verdade em 04/11/2026: a conta do bot está migrada para LID, a pessoa
  // escreveu, o jid da conversa não tinha telefone nenhum e a mensagem foi descartada em silêncio.
  assert.equal(
    telefoneDeQuemEscreveu({
      remoteJid: '213090591355025@lid',
      senderPn: '5511900000000@s.whatsapp.net',
    }),
    '5511900000000',
  )
  // Só com participantPn também serve.
  assert.equal(
    telefoneDeQuemEscreveu({
      remoteJid: '213090591355025@lid',
      participantPn: '5511900000000@s.whatsapp.net',
    }),
    '5511900000000',
  )
  // Endereço clássico continua pelo caminho curto.
  assert.equal(
    telefoneDeQuemEscreveu({ remoteJid: '5511900000000@s.whatsapp.net' }),
    '5511900000000',
  )
})

test('o telefone do grupo não abre a porta do bot', () => {
  // Mensagem de grupo TEM participantPn (foi o que apareceu no log de produção). Aceitá-la aqui
  // poria o bot respondendo em grupo alheio, que é exatamente o que o serviço não faz.
  assert.equal(
    telefoneDeQuemEscreveu({
      remoteJid: '120363039023977288@g.us',
      participantPn: '5511900000000@s.whatsapp.net',
    }),
    undefined,
    'grupo',
  )
  assert.equal(
    telefoneDeQuemEscreveu({
      remoteJid: 'status@broadcast',
      participantPn: '5511900000000@s.whatsapp.net',
    }),
    undefined,
    'status',
  )
  // LID sem telefone à parte: descarte, mas com aviso no log (a sessão registra).
  assert.equal(telefoneDeQuemEscreveu({ remoteJid: '213090591355025@lid' }), undefined)
  assert.equal(ehLid('213090591355025@lid'), true)
  assert.equal(ehLid('5511900000000@s.whatsapp.net'), false)
})

test('texto sai de mensagem simples e de legenda', () => {
  const simples = { message: { conversation: 'oi' } } as proto.IWebMessageInfo
  assert.equal(textoDaMensagem(simples), 'oi')

  const estendida = { message: { extendedTextMessage: { text: '  quero alugar  ' } } } as proto.IWebMessageInfo
  assert.equal(textoDaMensagem(estendida), 'quero alugar')

  const comFoto = { message: { imageMessage: { caption: 'esta casa' } } } as proto.IWebMessageInfo
  assert.equal(textoDaMensagem(comFoto), 'esta casa')

  // Sem texto: áudio, figurinha, localização. A plataforma não transcreve nada (ainda).
  assert.equal(textoDaMensagem({ message: { audioMessage: {} } } as proto.IWebMessageInfo), undefined)
  assert.equal(textoDaMensagem({ message: { conversation: '   ' } } as proto.IWebMessageInfo), undefined)
  assert.equal(textoDaMensagem({} as proto.IWebMessageInfo), undefined)
})

test('telefone em log sai mascarado: é dado de cliente', () => {
  assert.equal(mascarar('5511987654321'), '5511***21')
  assert.equal(mascarar('123'), '***')
})

test('jid de envio é sempre privado', () => {
  assert.equal(jidDe('+55 (11) 98765-4321'), '5511987654321@s.whatsapp.net')
})

test('a guarda de enviadas atende o pedido de reenvio', () => {
  // Sem isto a resposta do bot fica em "Aguardando mensagem" no aparelho de quem recebeu: o
  // WhatsApp pede a mensagem de novo e o Baileys não tem o que mandar.
  const guarda = criarGuardaDeEnviadas(3)
  guarda.guardar('AAA', { conversation: 'oi' })
  assert.deepEqual(guarda.buscar('AAA'), { conversation: 'oi' })
  assert.equal(guarda.buscar('nunca-enviada'), undefined)
  assert.equal(guarda.buscar(undefined), undefined)

  // Id ou conteúdo vazio não ocupam lugar.
  guarda.guardar(undefined, { conversation: 'x' })
  guarda.guardar('BBB', undefined)
  assert.equal(guarda.tamanho, 1)

  // Cheia, descarta a mais antiga — é cache, não histórico (o histórico é da API).
  guarda.guardar('BBB', { conversation: 'b' })
  guarda.guardar('CCC', { conversation: 'c' })
  guarda.guardar('DDD', { conversation: 'd' })
  assert.equal(guarda.tamanho, 3)
  assert.equal(guarda.buscar('AAA'), undefined, 'a mais antiga saiu')
  assert.deepEqual(guarda.buscar('DDD'), { conversation: 'd' })
})

test('de quem é a conversa: mensagem que chega', () => {
  // Endereço clássico: o telefone está no jid.
  assert.deepEqual(enderecoDaConversa({ remoteJid: '5511900000000@s.whatsapp.net', fromMe: false }), {
    telefone: '5511900000000',
    lid: undefined,
  })
  // Endereço LID: o telefone vem à parte, e o LID identifica a conversa.
  assert.deepEqual(
    enderecoDaConversa({
      remoteJid: '213090591355025@lid',
      fromMe: false,
      senderPn: '5511900000000@s.whatsapp.net',
    }),
    { telefone: '5511900000000', lid: '213090591355025' },
  )
})

test('de quem é a conversa: mensagem que SAI pelo número do bot', () => {
  // Saindo, quem interessa é o outro lado — e ele está no jid do chat.
  assert.deepEqual(enderecoDaConversa({ remoteJid: '5511900000000@s.whatsapp.net', fromMe: true }), {
    telefone: '5511900000000',
    lid: undefined,
  })
  // Em conversa LID, saindo, o `senderPn` é o NOSSO próprio número: usá-lo faria o bot abrir
  // conversa consigo mesmo. Só o LID vale, e a API acha a conversa por ele (issue #128).
  assert.deepEqual(
    enderecoDaConversa({
      remoteJid: '213090591355025@lid',
      fromMe: true,
      senderPn: '5511952657883@s.whatsapp.net',
    }),
    { telefone: '', lid: '213090591355025' },
  )
  // Grupo continua de fora, saindo ou chegando.
  assert.deepEqual(enderecoDaConversa({ remoteJid: '120363039023977288@g.us', fromMe: true }), {
    telefone: '',
    lid: undefined,
  })
})
