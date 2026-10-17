'use strict';

// Casamento das cotas do site com as do cadastro. Os formatos aqui são os da resposta real
// medida em 16/10/2026 (números, cota sem zeros à esquerda, campo Versao).

const test = require('node:test');
const assert = require('node:assert/strict');
const { normalizar, chave, cotaDoSite, casarCotas } = require('../src/cotas');

test('normalizar preenche com zeros à esquerda e tira pontuação', () => {
  assert.equal(normalizar('6650', 6), '006650');
  assert.equal(normalizar(924, 4), '0924');
  assert.equal(normalizar(0, 2), '00');
  assert.equal(normalizar('6.650', 6), '006650');
  assert.equal(normalizar('', 6), null);
  assert.equal(normalizar(null, 4), null);
  // Mais dígitos do que cabe: devolve como está, para não casar por engano.
  assert.equal(normalizar('1234567', 6), '1234567');
});

test('chave ignora versão ausente', () => {
  assert.equal(chave('006650', 924, 0).tag, '006650-0924-00');
  assert.equal(chave('006650', 924, null).tag, '006650-0924');
  assert.equal(chave('', 924, 0), null);
});

test('cotaDoSite exige o ID_Cota, que é o que o generate-bill pede', () => {
  const c = cotaDoSite({ CD_Grupo: '006650', CD_Cota: 924, Versao: 0, ID_Cota: 468379 });
  assert.deepEqual({ grupo: c.grupo, cota: c.cota, versao: c.versao, idCota: c.idCota },
    { grupo: '006650', cota: '0924', versao: '00', idCota: '468379' });
  assert.equal(cotaDoSite({ CD_Grupo: '006650', CD_Cota: 924, Versao: 0 }), null);
  assert.equal(cotaDoSite({ CD_Grupo: '006650', ID_Cota: 1 }), null);
});

test('casa cota de 3 dígitos do site com a de 4 do cadastro', () => {
  // É o caso que quebraria numa comparação de texto: o site manda 924, o cadastro guarda "0924".
  const site = [{ CD_Grupo: '006650', CD_Cota: 924, Versao: 0, ID_Cota: 468379 }];
  const cadastro = [{ id: 7, grupo: '006650', cota: '0924', versao: '00' }];
  const r = casarCotas(site, cadastro);
  assert.equal(r.casadas.length, 1);
  assert.equal(r.casadas[0].cadastro.id, 7);
  assert.equal(r.casadas[0].site.idCota, '468379');
  assert.deepEqual(r.soNoSite, []);
  assert.deepEqual(r.soNoCadastro, []);
});

test('separa o que só existe no site e o que só existe no cadastro', () => {
  const site = [
    { CD_Grupo: '006650', CD_Cota: 924, Versao: 0, ID_Cota: 1 },
    { CD_Grupo: '006620', CD_Cota: 1372, Versao: 0, ID_Cota: 2 },
  ];
  const cadastro = [
    { id: 1, grupo: '006650', cota: '0924', versao: '00' },
    { id: 2, grupo: '005678', cota: '0012', versao: '00' },
  ];
  const r = casarCotas(site, cadastro);
  assert.deepEqual(r.casadas.map((c) => c.cadastro.id), [1]);
  assert.deepEqual(r.soNoSite.map((c) => c.site.tag), ['006620-1372-00']);
  assert.deepEqual(r.soNoCadastro.map((c) => c.id), [2]);
});

test('sem versão no site, duas versões cadastradas viram ambiguidade em vez de palpite', () => {
  const site = [{ CD_Grupo: '006650', CD_Cota: 924, ID_Cota: 1 }];
  const cadastro = [
    { id: 1, grupo: '006650', cota: '0924', versao: '00' },
    { id: 2, grupo: '006650', cota: '0924', versao: '01' },
  ];
  const r = casarCotas(site, cadastro);
  assert.equal(r.casadas.length, 0);
  assert.equal(r.soNoSite.length, 1);
  assert.equal(r.soNoSite[0].ambiguo, true);
});

test('sem versão no site e uma só cadastrada, casa', () => {
  const r = casarCotas(
    [{ CD_Grupo: '006650', CD_Cota: 924, ID_Cota: 1 }],
    [{ id: 9, grupo: '006650', cota: '0924', versao: '00' }],
  );
  assert.equal(r.casadas.length, 1);
  assert.equal(r.casadas[0].cadastro.id, 9);
});

test('cota do site sem dados utilizáveis fica em invalidas', () => {
  const r = casarCotas([{ CD_Grupo: '', CD_Cota: '', ID_Cota: '' }], []);
  assert.equal(r.invalidas.length, 1);
  assert.equal(r.casadas.length, 0);
});
