'use strict';

/**
 * Leitura da resposta do generate-bill do Boleto Fácil. Tudo aqui vem de uma resposta real
 * conferida em 16/10/2026 (docs/canopus-boleto-facil.md):
 *
 *   {"bills":[{"id":39871,"parcelNumber":"018","date":"2026-11-16","price":644.62,
 *              "emitBillet":false,"type":"overdue","group":"006650","cota":"3419",
 *              "transactionId":179831728},
 *             {"id":39870,"parcelNumber":"DIF","date":"2026-11-16","price":0, ...}]}
 *
 * Armadilhas que isso já mostrou, e que esta camada resolve:
 *   - `price` 0 existe (linha "DIF", de diferença/reajuste). Sem filtrar, o cliente receberia
 *     um boleto de R$ 0,00;
 *   - `parcelNumber` é TEXTO e nem sempre é número ("018", "DIF");
 *   - `type` veio "overdue" numa parcela que vence no futuro: NÃO quer dizer "em atraso", só
 *     escolhe a rota de impressão;
 *   - o `Content-Type` da resposta é text/html, com corpo JSON: quem decidir pelo cabeçalho
 *     erra. Por isso a interpretação começa no texto.
 */

class ErroBoletoFacil extends Error {
  constructor(mensagem, { conhecido = true } = {}) {
    super(mensagem);
    this.name = 'ErroBoletoFacil';
    this.conhecido = conhecido;
  }
}

/** Reais com centavos → centavos, sem erro de ponto flutuante (644.62 → 64462). */
function paraCentavos(price) {
  const n = typeof price === 'number' ? price : Number(String(price ?? '').replace(',', '.'));
  if (!Number.isFinite(n)) return null;
  return Math.round(n * 100);
}

function dataValida(texto) {
  const v = String(texto ?? '').trim();
  if (!/^\d{4}-\d{2}-\d{2}$/.test(v)) return null;
  const d = new Date(`${v}T12:00:00Z`);
  return Number.isNaN(d.getTime()) ? null : v;
}

/** Interpreta o corpo (texto) do find-cota. Devolve a lista crua de cotas. */
function lerCotas(texto) {
  let corpo;
  try {
    corpo = JSON.parse(texto);
  } catch {
    throw new ErroBoletoFacil('o site não devolveu JSON na consulta de cotas (mudou o formato?)');
  }
  if (!corpo || !Array.isArray(corpo.cotas)) {
    throw new ErroBoletoFacil('a consulta de cotas não trouxe a lista "cotas" (mudou o formato?)');
  }
  return corpo.cotas;
}

/**
 * Interpreta o corpo (texto) do generate-bill e devolve só as parcelas que viram boleto.
 * Cada parcela sai pronta para a plataforma (centavos) e para a impressão (urlPdf relativa).
 */
function lerParcelas(texto, { base }) {
  let corpo;
  try {
    corpo = JSON.parse(texto);
  } catch {
    throw new ErroBoletoFacil('o site não devolveu JSON ao gerar o boleto (mudou o formato?)');
  }
  if (!corpo || !Array.isArray(corpo.bills)) {
    throw new ErroBoletoFacil('a geração do boleto não trouxe a lista "bills" (mudou o formato?)');
  }

  const parcelas = [];
  const descartadas = [];
  for (const b of corpo.bills) {
    const centavos = paraCentavos(b && b.price);
    const vencimento = dataValida(b && b.date);
    const parcela = String((b && b.parcelNumber) ?? '').trim();
    const motivo =
      centavos === null ? 'valor ilegível'
      : centavos <= 0 ? 'valor zero'
      : !vencimento ? 'vencimento ilegível'
      : parcela === '' ? 'sem número de parcela'
      : null;
    if (motivo) {
      descartadas.push({ parcela: parcela || '?', motivo });
      continue;
    }
    parcelas.push({
      parcela,
      vencimento,
      valorCentavos: centavos,
      tipo: String((b && b.type) ?? '').trim() || 'desconhecido',
      chargeId: numeroOuNulo(b && b.id),
      transactionId: numeroOuNulo(b && b.transactionId),
      urlPdf: urlDoPdf(b, base),
    });
  }
  return { parcelas, descartadas };
}

function numeroOuNulo(valor) {
  const n = Number(valor);
  return Number.isSafeInteger(n) && n > 0 ? n : null;
}

/**
 * Rota da impressão: 'duplicate' tem id próprio; o resto (foi visto 'overdue', inclusive em
 * parcela que vence no futuro) usa grupo/cota/id/transação. Os dois ramos existem de propósito:
 * só o 'overdue' foi visto de verdade, e o 'duplicate' está no código do site.
 */
function urlDoPdf(bill, base) {
  const raiz = String(base || '').replace(/\/+$/, '');
  if (!bill) return null;
  if (bill.type === 'duplicate') {
    const id = numeroOuNulo(bill.id);
    return id ? `${raiz}/print-bill/${id}` : null;
  }
  const { group, cota, id, transactionId } = bill;
  const partes = [group, cota, id, transactionId].map((p) => String(p ?? '').trim());
  if (partes.some((p) => p === '')) return null;
  return `${raiz}/print-overdue-bill/${partes[0]}/${partes[1]}/${partes[2]}/${partes[3]}`;
}

/** Confere a cara do PDF antes de mandar à plataforma (a API recusa o que não for PDF). */
function pdfValido(bytes) {
  if (!bytes || bytes.length < 5) return false;
  const inicio = Buffer.from(bytes.slice(0, 5)).toString('latin1');
  return inicio === '%PDF-';
}

module.exports = { ErroBoletoFacil, paraCentavos, dataValida, lerCotas, lerParcelas, urlDoPdf, pdfValido };
