'use strict';

// Reimpressão de comprovante com Newcon e API falsos.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { processarReimpressao } = require('../src/reimpressao');
const { Registro } = require('../src/registro');

const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'reimpressao-teste-'));

class NewconCotaError extends Error {
  constructor(m) {
    super(m);
    this.name = 'NewconCotaError';
  }
}

function cenario({ paginaDoHistorico = false, protocolo = '1788714' } = {}) {
  const ordem = [];
  const conclusoes = [];
  const ctx = {
    registro: new Registro({ plataforma: { enviarEventos: async () => ({ ok: true }) }, execucaoId: 3, saida: { log() {} } }),
    cota: { id: 11, grupo: '006650', cota: '2068', versao: '00', cliente_nome: 'CLIENTE', protocolo },
    voltarAoFiltro: false,
    config: { playwright: { timeoutMs: 1000 } },
    newcon: {
      page: {},
      async backToFilter() { ordem.push('voltar'); },
      async searchCota() {
        ordem.push('buscar');
        if (paginaDoHistorico) throw new NewconCotaError('Newcon abriu só o Histórico das Ofertas: não há credenciamento disponível para esta cota');
      },
      async downloadReportPdf() { ordem.push('pdf'); const f = path.join(tmp, 'c.pdf'); fs.writeFileSync(f, '%PDF-'); return f; },
      async snapshotError() { return null; },
      async confirmAndWaitReport() { ordem.push('CONFIRMAR'); },
      async selectSegundoFixo() { ordem.push('2º fixo'); },
    },
    leitura: {
      async abrirHistorico() { ordem.push('abrir histórico'); },
      async lerGradeHistorico() {
        ordem.push('ler grade');
        return [{ protocolo: '1788714', assembleia: '16/06/2026', credenciamento: '11/06/2026 20:27:14', modalidade: '2º Lance Fixo', percentual: '30,0000%' }];
      },
      async reimprimirProtocolo(_page, p) { ordem.push(`reimprimir ${p}`); },
    },
    plataforma: {
      async enviarPdf() { ordem.push('enviar pdf'); return 'pdf-1'; },
      async concluirReimpressao(_id, c) { ordem.push('concluir-reimpressao'); conclusoes.push(c); return { ok: true }; },
    },
    enviarScreenshot: async () => {},
    concluir: async (_c, conclusao) => { conclusoes.push(conclusao); },
  };
  return { ctx, ordem, conclusoes };
}

test('cota sem credenciamento: usa a página do Histórico direto', async () => {
  const { ctx, ordem, conclusoes } = cenario({ paginaDoHistorico: true });
  await processarReimpressao(ctx);
  assert.deepEqual(ordem, ['buscar', 'ler grade', 'reimprimir 1788714', 'pdf', 'enviar pdf', 'concluir-reimpressao']);
  assert.deepEqual(conclusoes[0], {
    status: 'reimpressa', protocolo: '1788714', pdf_id: 'pdf-1', assembleia_data: '16/06/2026',
    credenciamento: '11/06/2026 20:27:14', modalidade: '2º Lance Fixo', percentual: '30,0000%',
  });
});

test('tela de credenciamento: abre o painel do Histórico antes; nunca confirma nem marca modalidade', async () => {
  const { ctx, ordem } = cenario();
  await processarReimpressao(ctx);
  assert.deepEqual(ordem.slice(0, 3), ['buscar', 'abrir histórico', 'ler grade']);
  assert.ok(!ordem.includes('CONFIRMAR') && !ordem.includes('2º fixo'));
});

test('protocolo que não está no Histórico: erro conhecido, sem reimprimir', async () => {
  const { ctx, ordem, conclusoes } = cenario({ protocolo: '1111111' });
  await processarReimpressao(ctx);
  assert.ok(!ordem.some((o) => o.startsWith('reimprimir')));
  assert.equal(conclusoes[0].erro_tipo, 'conhecido');
  assert.match(conclusoes[0].erro, /protocolo 1111111 não aparece no Histórico/);
});
