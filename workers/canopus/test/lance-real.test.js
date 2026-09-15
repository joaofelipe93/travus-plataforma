'use strict';

// Fluxo do lance real com Newcon e API falsos. Nada aqui acessa o Newcon.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { processarCotaReal } = require('../src/lance-real');
const { Registro } = require('../src/registro');

const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'lance-real-teste-'));
const silencio = { log() {} };
const PROTOCOLO = 'Anote os números dos protocolos: 2º Lance Fixo (Automático): 1889071 ';
const ATRASO = 'Cota com Parcelas em Atraso. Deseja prosseguir?';

class NewconCotaError extends Error {
  constructor(m) {
    super(m);
    this.name = 'NewconCotaError';
  }
}

function cenario(opcoes = {}) {
  const ordem = [];
  const conclusoes = [];
  const registro = new Registro({ plataforma: { enviarEventos: async () => ({ ok: true }) }, execucaoId: 9, saida: silencio });
  const assembleias = opcoes.assembleias || ['15/09/2026', '15/09/2026'];
  let leituras = 0;

  const newcon = {
    page: {},
    async backToFilter() { ordem.push('voltar'); },
    async searchCota() { ordem.push('buscar'); if (opcoes.erroBusca) throw opcoes.erroBusca; },
    async selectSegundoFixo() { ordem.push('2º fixo'); },
    async captureBeforeConfirm() { ordem.push('screenshot'); const f = path.join(tmp, 's.png'); fs.writeFileSync(f, 'x'); return f; },
    async snapshotError() { return null; },
    async confirmAndWaitReport() {
      ordem.push('CONFIRMAR');
      for (const m of opcoes.dialogos || [PROTOCOLO]) registro.info('dialog', { type: 'alert', message: m });
      if (opcoes.erroRelatorio) throw opcoes.erroRelatorio;
    },
    async downloadReportPdf() {
      ordem.push('pdf');
      if (opcoes.erroPdf) throw opcoes.erroPdf;
      const f = path.join(tmp, 'r.pdf');
      fs.writeFileSync(f, '%PDF-1.4');
      return f;
    },
  };
  const leitura = {
    async lerDadosCredenciamento() {
      ordem.push('dados');
      return { assembleia_data: assembleias[Math.min(leituras++, assembleias.length - 1)], assembleia_numero: '027', percentual_segundo_fixo: '30.0000' };
    },
    async lerHistorico() {
      ordem.push('histórico');
      if (opcoes.erroHistorico) throw opcoes.erroHistorico;
      return opcoes.historico || [{ protocolo: '1849221', assembleia: '17/08/2026', modalidade: '2º Lance Fixo' }];
    },
  };
  const plataforma = {
    async marcarConfirmacaoIniciada(id, corpo) {
      ordem.push('marcar');
      if (opcoes.marcar) return opcoes.marcar(corpo);
      return { ok: true };
    },
    async enviarPdf() { ordem.push('enviar pdf'); return 'pdf-uuid'; },
    async concluirConfirmacao(id, c) {
      ordem.push(`concluir-confirmacao ${c.status}`);
      if (opcoes.falhasAoConcluir && opcoes.falhasAoConcluir-- > 0) throw new Error('rede');
      conclusoes.push(c);
      return { ok: true };
    },
  };
  const ctx = {
    newcon, registro, plataforma, leitura,
    cota: { id: 5, grupo: '006650', cota: '0236', versao: '00', cliente_nome: 'CLIENTE', assembleia_aprovada: '15/09/2026', permitir_lance_existente: !!opcoes.permitir },
    voltarAoFiltro: false,
    config: { playwright: { timeoutMs: 1000 } },
    pausasMs: [1, 1, 1],
    enviarScreenshot: async () => ordem.push('enviar screenshot'),
    concluir: async (c, conclusao) => { ordem.push(`concluir ${conclusao.status}`); conclusoes.push(conclusao); },
  };
  return { ctx, ordem, conclusoes };
}

test('sucesso: grava confirmacao_iniciada ANTES do clique, captura protocolo e PDF', async () => {
  const { ctx, ordem, conclusoes } = cenario({ dialogos: [ATRASO, PROTOCOLO] });
  await processarCotaReal(ctx);
  assert.deepEqual(ordem, [
    'buscar', 'dados', 'histórico', 'voltar', 'buscar', 'dados', '2º fixo', 'screenshot', 'enviar screenshot',
    'marcar', 'CONFIRMAR', 'pdf', 'enviar pdf', 'concluir-confirmacao confirmada',
  ]);
  assert.equal(conclusoes[0].protocolo, '1889071');
  assert.equal(conclusoes[0].parcelas_em_atraso, true);
  assert.equal(conclusoes[0].lance_existente, false);
  assert.equal(conclusoes[0].pdf_id, 'pdf-uuid');
});

test('API recusa marcar confirmacao_iniciada: não clica', async () => {
  const { ctx, ordem, conclusoes } = cenario({ marcar: () => ({ ok: false, erro: 'lance real desligado' }) });
  await processarCotaReal(ctx);
  assert.ok(!ordem.includes('CONFIRMAR'));
  assert.equal(conclusoes[0].status, 'erro_antes_confirmar');
  assert.match(conclusoes[0].erro, /não cliquei em Confirmar/);
});

test('falha de rede ao marcar confirmacao_iniciada: não clica', async () => {
  const { ctx, ordem, conclusoes } = cenario({ marcar: () => { throw new Error('ECONNRESET'); } });
  await processarCotaReal(ctx);
  assert.ok(!ordem.includes('CONFIRMAR'));
  assert.deepEqual([conclusoes[0].status, conclusoes[0].erro_tipo], ['erro_antes_confirmar', 'inesperado']);
});

test('assembleia diferente da aprovada: não clica (antes e depois de reabrir a cota)', async () => {
  for (const assembleias of [['16/10/2026'], ['15/09/2026', '16/10/2026']]) {
    const { ctx, ordem, conclusoes } = cenario({ assembleias });
    await processarCotaReal(ctx);
    assert.ok(!ordem.includes('marcar') && !ordem.includes('CONFIRMAR'), ordem.join(', '));
    assert.match(conclusoes[0].erro, /não é a aprovada na revisão/);
  }
});

test('lance já existente na assembleia: pula sem autorização, confirma com autorização', async () => {
  const historico = [{ protocolo: '1889156', assembleia: '15/09/2026', modalidade: '2º Lance Fixo' }];
  const sem = cenario({ historico });
  await processarCotaReal(sem.ctx);
  assert.ok(!sem.ordem.includes('CONFIRMAR'));
  assert.match(sem.conclusoes[0].erro, /já tem 1 lance\(s\) na assembleia de 15\/09\/2026 \(1889156\).*pulada/);

  const com = cenario({ historico, permitir: true });
  await processarCotaReal(com.ctx);
  assert.ok(com.ordem.includes('CONFIRMAR'));
  assert.equal(com.conclusoes[0].status, 'confirmada');
});

test('Histórico ilegível ou prazo encerrado: não clica', async () => {
  const historico = cenario({ erroHistorico: new Error('Timeout') });
  await processarCotaReal(historico.ctx);
  assert.ok(!historico.ordem.includes('CONFIRMAR'));
  assert.match(historico.conclusoes[0].erro, /não foi possível conferir o Histórico/);

  const prazo = cenario({ erroBusca: new NewconCotaError('Newcon não abriu a cota após "Localizar"') });
  prazo.ctx.registro.info('dialog', { type: 'alert', message: 'Oferta de Lance só poderá ser realizada até 02:30 hora(s) antes da assembleia.\nTérmino da oferta de lance: 15/09/2026 à(s) 14:00 hora(s).' });
  prazo.ctx.registro.dialogos.length = 0; // aviso anterior à cota não conta
  const antes = prazo.ctx.newcon.searchCota;
  prazo.ctx.newcon.searchCota = async (c) => {
    prazo.ctx.registro.info('dialog', { type: 'alert', message: 'Oferta de Lance só poderá ser realizada até 02:30 hora(s) antes da assembleia.\nTérmino da oferta de lance: 15/09/2026 à(s) 14:00 hora(s).' });
    return antes(c);
  };
  await processarCotaReal(prazo.ctx);
  assert.deepEqual([prazo.conclusoes[0].erro_tipo, prazo.conclusoes[0].erro], ['conhecido', 'prazo de oferta de lance encerrado (término em 15/09/2026 às 14:00)']);
});

test('erro depois do clique: erro_apos_confirmar com o protocolo que apareceu', async () => {
  const { ctx, conclusoes } = cenario({ erroRelatorio: new Error('Timeout 60000ms exceeded.\nlinha') });
  await processarCotaReal(ctx);
  assert.equal(conclusoes[0].status, 'erro_apos_confirmar');
  assert.equal(conclusoes[0].protocolo, '1889071');
  assert.equal(conclusoes[0].erro, 'erro depois de clicar em Confirmar (Timeout 60000ms exceeded.); o Newcon mostrou o protocolo 1889071: o lance pode ter sido registrado, confira no Histórico');
});

test('sem protocolo depois do clique: erro_apos_confirmar', async () => {
  const { ctx, conclusoes, ordem } = cenario({ dialogos: [] });
  await processarCotaReal(ctx);
  assert.equal(conclusoes[0].status, 'erro_apos_confirmar');
  assert.ok(!ordem.includes('pdf'));
});

test('PDF falhou: lance confirmado sem PDF', async () => {
  const { ctx, conclusoes } = cenario({ erroPdf: new Error('download não é PDF') });
  await processarCotaReal(ctx);
  assert.deepEqual([conclusoes[0].status, conclusoes[0].pdf_id, conclusoes[0].protocolo], ['confirmada', null, '1889071']);
});

test('insiste em informar o resultado da confirmação', async () => {
  const { ctx, conclusoes, ordem } = cenario({ falhasAoConcluir: 2 });
  await processarCotaReal(ctx);
  assert.equal(ordem.filter((o) => o === 'concluir-confirmacao confirmada').length, 3);
  assert.equal(conclusoes.length, 1);
});
