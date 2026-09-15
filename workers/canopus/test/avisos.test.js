'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const { prazoEncerrado, extrairProtocolo, parcelasEmAtraso, jaCredenciado } = require('../src/avisos-newcon');

// Textos reais vistos no Newcon (logs de 13/09/2026 e descoberta de 15/09/2026).
const PRAZO = 'Oferta de Lance só poderá ser realizada até 02:30 hora(s) antes da assembleia.\nTérmino da oferta de lance: 15/09/2026 à(s) 14:00 hora(s).';
const PROTOCOLO = 'Anote os números dos protocolos: 2º Lance Fixo (Automático): 1889071 ';
const ATRASO = 'Cota com Parcelas em Atraso. Deseja prosseguir?';
const JA_CREDENCIADO = 'Consorciado já credenciado nesta assembleia na modalidade segundo lance fixo. Deseja continuar?';
const d = (mensagem, tipo = 'alert') => ({ tipo, mensagem });

test('prazo encerrado', () => {
  assert.equal(prazoEncerrado([d(PRAZO)]), 'prazo de oferta de lance encerrado (término em 15/09/2026 às 14:00)');
  assert.equal(prazoEncerrado([d(ATRASO, 'confirm')]), null);
  assert.match(prazoEncerrado([d('Oferta de Lance só poderá ser realizada em outro horário.')]), /^prazo de oferta de lance encerrado: Oferta/);
  assert.equal(prazoEncerrado([]), null);
});

test('protocolo do alerta', () => {
  assert.deepEqual(extrairProtocolo([d(JA_CREDENCIADO, 'confirm'), d(PROTOCOLO)]), {
    protocolo: '1889071',
    texto: 'Anote os números dos protocolos: 2º Lance Fixo (Automático): 1889071',
  });
  assert.equal(extrairProtocolo([d('Anote os números dos protocolos:')]), null);
  assert.equal(extrairProtocolo([d(ATRASO)]), null);
});

test('parcelas em atraso e já credenciado', () => {
  assert.equal(parcelasEmAtraso([d(ATRASO, 'confirm'), d(PROTOCOLO)]), true);
  assert.equal(parcelasEmAtraso([d(PROTOCOLO)]), false);
  assert.equal(jaCredenciado([d(JA_CREDENCIADO, 'confirm')]), true);
  assert.equal(jaCredenciado([d(ATRASO, 'confirm')]), false);
});
