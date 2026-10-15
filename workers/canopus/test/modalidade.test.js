'use strict';

// src/modalidade.js: traduz a modalidade do CADASTRO da cota no radio do Newcon. Nada aqui abre
// navegador nem fala com o Newcon — a página é um dublê.

const { test } = require('node:test');
const assert = require('node:assert/strict');

const { daCota, marcar, MODALIDADES } = require('../src/modalidade');

function newconFalso({ desabilitado = false, disponiveis = [] } = {}) {
  const cliente = {
    marcados: [],
    logger: { info() {}, ok() {} },
    config: { playwright: { timeoutMs: 1000 } },
  };
  cliente.page = {
    locator(seletor) {
      return {
        async waitFor() {},
        // Só o radio da modalidade pode estar desabilitado.
        async isDisabled() { return desabilitado && seletor.startsWith('#ctl00_Conteudo_rgLance'); },
        async check() { cliente.marcados.push(seletor); },
        async evaluateAll() { return disponiveis; },
      };
    },
    async waitForLoadState() {},
  };
  return cliente;
}

test('daCota devolve o radio da modalidade cadastrada', () => {
  assert.equal(daCota({ modalidade: 'fixo' }).radio, '#ctl00_Conteudo_rgLance_1');
  assert.equal(daCota({ modalidade: 'segundo_fixo' }).radio, '#ctl00_Conteudo_rgLance_2');
  assert.equal(daCota({ modalidade: ' fixo ' }).codigo, 'fixo');
});

// O percentual de cada modalidade tem rótulo próprio: usar o do 2º Fixo num lance Fixo
// gravaria um percentual errado no lance.
test('cada modalidade lê o percentual do seu próprio rótulo', () => {
  assert.equal(MODALIDADES.segundo_fixo.percentual, '#ctl00_Conteudo_lblVA_Lance_Fixo_2');
  assert.equal(MODALIDADES.fixo.percentual, '#ctl00_Conteudo_lblVA_Lance_Fixo');
});

test('modalidade que a plataforma não dá é erro conhecido, com o nome na mensagem', () => {
  for (const [codigo, nome] of [['livre', 'Livre'], ['limitado', 'Limitado'], ['fidelidade', 'Fidelidade']]) {
    assert.throws(() => daCota({ modalidade: codigo }), (e) => {
      assert.equal(e.name, 'NewconCotaError');
      assert.match(e.message, new RegExp(nome));
      return true;
    });
  }
});

// A regressão que importa: antes desta mudança, qualquer cota levava 2º Fixo.
test('modalidade vazia ou desconhecida não vira 2º Fixo por engano', () => {
  assert.throws(() => daCota({}), { name: 'NewconCotaError' });
  assert.throws(() => daCota({ modalidade: '' }), { name: 'NewconCotaError' });
  assert.throws(() => daCota({ modalidade: 'inventada' }), { name: 'NewconCotaError' });
});

test('marcar clica no radio da modalidade pedida', async () => {
  const newcon = newconFalso();
  await marcar(newcon, daCota({ modalidade: 'fixo' }));
  assert.deepEqual(newcon.marcados, ['#ctl00_Conteudo_rgLance_1']);

  const outro = newconFalso();
  await marcar(outro, daCota({ modalidade: 'segundo_fixo' }));
  assert.deepEqual(outro.marcados, ['#ctl00_Conteudo_rgLance_2']);
});

// Como no grupo 6620, em que o 2º Fixo vem desabilitado: erro conhecido listando o que há.
test('radio desabilitado é erro conhecido e não marca nada', async () => {
  const newcon = newconFalso({ desabilitado: true, disponiveis: ['Livre', 'Fixo', 'Limitado'] });
  await assert.rejects(() => marcar(newcon, daCota({ modalidade: 'segundo_fixo' })), { name: 'NewconCotaError' });
  assert.deepEqual(newcon.marcados, [], 'não pode marcar nada quando o radio está desabilitado');
});

test('mensagem do radio desabilitado nomeia a modalidade e as disponíveis', async () => {
  const newcon = newconFalso({ desabilitado: true, disponiveis: ['Livre', 'Fixo', 'Limitado'] });
  await assert.rejects(() => marcar(newcon, daCota({ modalidade: 'fixo' })), (e) => {
    assert.equal(e.name, 'NewconCotaError');
    assert.match(e.message, /"Fixo" está desabilitado/);
    assert.match(e.message, /Livre, Fixo, Limitado/);
    return true;
  });
});
