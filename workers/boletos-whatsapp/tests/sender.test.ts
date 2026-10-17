import './helpers/setup.js'
import test from 'node:test'
import assert from 'node:assert/strict'
import { jidDe, mascarar } from '../src/whatsapp/sender.js'

test('jid individual, não de grupo', () => {
  // O destino aqui é o privado de um cliente: @s.whatsapp.net, nunca @g.us (que é o do
  // notificador de check-in).
  assert.equal(jidDe('5511900000000'), '5511900000000@s.whatsapp.net')
  assert.equal(jidDe('+55 (11) 90000-0000'), '5511900000000@s.whatsapp.net')
})

test('telefone em log sai mascarado', () => {
  // Telefone é dado de cliente e o log vai para o container e para a tela.
  assert.equal(mascarar('5511900000000'), '5511***00')
  assert.equal(mascarar('123'), '***')
})
