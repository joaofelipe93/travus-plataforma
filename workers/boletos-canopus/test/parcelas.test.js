'use strict';

// Leitura da resposta do generate-bill. O corpo de referência é o que o site devolveu de
// verdade em 16/10/2026 (com a linha "DIF" de valor zero junto da parcela boa), com os
// identificadores trocados por fictícios.

const test = require('node:test');
const assert = require('node:assert/strict');
const { paraCentavos, dataValida, lerCotas, lerParcelas, urlDoPdf, pdfValido, ErroBoletoFacil } = require('../src/parcelas');

const BASE = 'https://exemplo.invalid/extensions/boleto-facil-rolledback/public/index.php';

// Mesma forma da resposta real: price como número, parcelNumber como texto, type "overdue"
// mesmo vencendo no futuro, e uma linha DIF com valor zero.
const RESPOSTA_REAL = JSON.stringify({
  bills: [
    {
      id: 39871, parcelNumber: '018', date: '2026-11-16', price: 644.62, emitBillet: false,
      type: 'overdue', group: '006650', cota: '0924', transactionId: 179831728,
    },
    {
      id: 39870, parcelNumber: 'DIF', date: '2026-11-16', price: 0, emitBillet: false,
      type: 'overdue', group: '006650', cota: '0924', transactionId: 179831728,
    },
  ],
});

test('paraCentavos não se perde no ponto flutuante', () => {
  assert.equal(paraCentavos(644.62), 64462);
  assert.equal(paraCentavos(1360.5), 136050);
  assert.equal(paraCentavos(0), 0);
  assert.equal(paraCentavos('644,62'), 64462);
  assert.equal(paraCentavos('abc'), null);
});

test('dataValida aceita só o formato ISO que o site manda', () => {
  assert.equal(dataValida('2026-11-16'), '2026-11-16');
  assert.equal(dataValida('16/11/2026'), null);
  assert.equal(dataValida(''), null);
});

test('lerParcelas descarta a linha de valor zero e converte o resto', () => {
  const { parcelas, descartadas } = lerParcelas(RESPOSTA_REAL, { base: BASE });
  assert.equal(parcelas.length, 1);
  assert.deepEqual(parcelas[0], {
    parcela: '018',
    vencimento: '2026-11-16',
    valorCentavos: 64462,
    tipo: 'overdue',
    chargeId: 39871,
    transactionId: 179831728,
    urlPdf: `${BASE}/print-overdue-bill/006650/0924/39871/179831728`,
  });
  assert.deepEqual(descartadas, [{ parcela: 'DIF', motivo: 'valor zero' }]);
});

test('lerParcelas explica quando o formato muda', () => {
  assert.throws(() => lerParcelas('<html>bloqueado</html>', { base: BASE }), ErroBoletoFacil);
  assert.throws(() => lerParcelas('{"outra":[]}', { base: BASE }), ErroBoletoFacil);
});

test('lerCotas exige a lista de cotas', () => {
  assert.deepEqual(lerCotas('{"cotas":[]}'), []);
  assert.equal(lerCotas('{"cotas":[{"CD_Grupo":"006650"}]}').length, 1);
  assert.throws(() => lerCotas('não é json'), ErroBoletoFacil);
  assert.throws(() => lerCotas('{}'), ErroBoletoFacil);
});

test('urlDoPdf escolhe a rota pelo type', () => {
  // O ramo visto de verdade: overdue, com grupo/cota/id/transação.
  assert.equal(
    urlDoPdf({ type: 'overdue', group: '006650', cota: '0924', id: 1, transactionId: 2 }, BASE),
    `${BASE}/print-overdue-bill/006650/0924/1/2`,
  );
  // O ramo que está no código do site mas ainda não apareceu.
  assert.equal(urlDoPdf({ type: 'duplicate', id: 55 }, BASE), `${BASE}/print-bill/55`);
  // Faltando dado, melhor não montar URL nenhuma.
  assert.equal(urlDoPdf({ type: 'overdue', group: '006650', cota: '0924', id: 1 }, BASE), null);
  assert.equal(urlDoPdf({ type: 'duplicate' }, BASE), null);
});

test('pdfValido confere a assinatura do arquivo', () => {
  assert.equal(pdfValido(Buffer.from('%PDF-1.4\n...')), true);
  assert.equal(pdfValido(Buffer.from('<html>')), false);
  assert.equal(pdfValido(Buffer.alloc(0)), false);
});

test('parcela sem vencimento legível é descartada com motivo', () => {
  const corpo = JSON.stringify({ bills: [{ id: 1, parcelNumber: '1', date: '16/11/2026', price: 10, type: 'overdue' }] });
  const { parcelas, descartadas } = lerParcelas(corpo, { base: BASE });
  assert.equal(parcelas.length, 0);
  assert.deepEqual(descartadas, [{ parcela: '1', motivo: 'vencimento ilegível' }]);
});
