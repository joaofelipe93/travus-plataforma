import './helpers/setup.js'
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  ehLid,
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
