'use strict';

// Testes do worker com Newcon e API falsos: nada aqui acessa o Newcon.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { Worker } = require('../src/worker');
const { Registro } = require('../src/registro');
const { resumirHistorico } = require('../src/leitura-credenciamento');
const { carregarConfig } = require('../src/config-worker');

const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'worker-teste-'));
const config = {
  worker: { nome: 'teste', intervaloFilaMs: 10, renovarTravaMs: 60000 },
  playwright: { timeoutMs: 1000, headless: true },
  paths: { screenshotDir: tmp, downloadDir: tmp },
};
const silencio = { log() {} };

class NewconCotaError extends Error {
  constructor(m) {
    super(m);
    this.name = 'NewconCotaError';
  }
}

// comportamento: { '006650-0236-00': 'ok' | 'conhecido' | 'inesperado' }
function newconFalso(comportamento, chamadas, { falharLogin = false, aoBuscar } = {}) {
  let atual = null;
  return (opcoes) => ({
    page: {},
    async start() { chamadas.push('start'); },
    async login() { chamadas.push('login'); if (falharLogin) throw new Error('usuário ou senha inválidos'); },
    async goToCredenciamento() { chamadas.push('credenciamento'); },
    async backToFilter() { chamadas.push('voltar'); },
    async searchCota(c) {
      const tag = `${c.grupo}-${c.cota}-${c.versao}`;
      chamadas.push(`buscar ${tag}`);
      atual = tag;
      if (aoBuscar) aoBuscar(tag);
      if (comportamento[tag] === 'inesperado') throw new Error('Timeout 30000ms exceeded.\nlinha 2');
      if (comportamento[tag] === 'prazo') {
        // Como no Newcon real depois do prazo (2026-09-15): alert aceito e página no filtro.
        opcoes.logger.info('dialog', { type: 'alert', message: 'Oferta de Lance só poderá ser realizada até 02:30 hora(s) antes da assembleia.\nTérmino da oferta de lance: 15/09/2026 à(s) 14:00 hora(s).' });
        throw new NewconCotaError('Newcon não abriu a cota após "Localizar" (ficou em frmConCpCadCredenciamentoLance_Filtro.aspx). Confira grupo, cota e versão.');
      }
    },
    // Como no Newcon real (grupo 6620): a tela abre, mas o "2º Fixo" está desabilitado.
    async selectSegundoFixo() {
      chamadas.push('2º fixo');
      if (comportamento[atual] === 'conhecido') throw new NewconCotaError('"2º Fixo" está desabilitado para esta cota no Newcon');
    },
    async captureBeforeConfirm(c) {
      const arquivo = path.join(tmp, `${c.grupo}-${c.cota}.png`);
      fs.writeFileSync(arquivo, 'png');
      return arquivo;
    },
    async snapshotError(tag) {
      const arquivo = path.join(tmp, `erro-${tag}.png`);
      fs.writeFileSync(arquivo, 'png');
      return arquivo;
    },
    // Proibidos nesta etapa: se forem chamados, o teste falha.
    async confirmAndWaitReport() { chamadas.push('CONFIRMAR'); throw new Error('PROIBIDO'); },
    async downloadReportPdf() { chamadas.push('PDF'); throw new Error('PROIBIDO'); },
    async close() { chamadas.push('close'); },
  });
}

const leituraFalsa = {
  async lerDadosCredenciamento() {
    return { assembleia_data: '15/09/2026', assembleia_numero: '027', percentual_segundo_fixo: '30.0000', ultimo_lance: 'Último lance ofertado em: 13/09/2026' };
  },
  async lerHistorico() {
    return [
      { protocolo: '1889156', assembleia: '15/09/2026', modalidade: '2º Lance Fixo' },
      { protocolo: '1849221', assembleia: '17/08/2026', modalidade: '2º Lance Fixo' },
    ];
  },
};

function plataformaFalsa({ iniciar } = {}) {
  const p = {
    chamadas: [],
    conclusoes: {},
    screenshots: [],
    eventos: [],
    async proximaTarefa() { return null; },
    async renovar() { return { ok: true, cancelamentoSolicitado: false }; },
    async enviarEventos(_id, lote) { p.eventos.push(...lote); return { ok: true }; },
    async iniciarCota(id) {
      p.chamadas.push(`iniciar ${id}`);
      return iniciar ? iniciar(id) : { ok: true };
    },
    async concluirCota(id, c) { p.conclusoes[id] = c; return { ok: true }; },
    async enviarScreenshot(id) { p.screenshots.push(id); return 'uuid'; },
    async finalizar(id, erro) { p.chamadas.push(`finalizar ${id}${erro ? `: ${erro}` : ''}`); return 'concluida'; },
    async liberar(id) { p.chamadas.push(`liberar ${id}`); },
  };
  return p;
}

const cota = (id, grupo, numero) => ({ id, grupo, cota: numero, versao: '00', status: 'pendente' });
const tarefa = (cotas, tipo = 'dry_run') => ({ execucao: { id: 7, tipo }, cotas });

test('processa as cotas: verificada, erro conhecido e erro inesperado', async () => {
  const chamadas = [];
  const plataforma = plataformaFalsa();
  const comportamento = { '006650-0236-00': 'ok', '006620-1372-00': 'conhecido', '006650-2068-00': 'inesperado' };
  const worker = new Worker({ config, plataforma, criarNewcon: newconFalso(comportamento, chamadas), leitura: leituraFalsa, saida: silencio });

  await worker.processar(tarefa([cota(1, '006650', '0236'), cota(2, '006620', '1372'), cota(3, '006650', '2068')]));

  assert.equal(plataforma.conclusoes[1].status, 'verificada');
  assert.equal(plataforma.conclusoes[1].detalhes.assembleia_numero, '027');
  assert.equal(plataforma.conclusoes[1].detalhes.lances_nesta_assembleia, 1);
  assert.deepEqual(plataforma.conclusoes[2], {
    status: 'erro_antes_confirmar', erro_tipo: 'conhecido', erro: '"2º Fixo" está desabilitado para esta cota no Newcon',
    detalhes: { assembleia_data: '15/09/2026', assembleia_numero: '027', percentual_segundo_fixo: '30.0000', ultimo_lance: 'Último lance ofertado em: 13/09/2026' },
  });
  assert.equal(plataforma.conclusoes[3].erro_tipo, 'inesperado');
  assert.equal(plataforma.conclusoes[3].erro, 'Timeout 30000ms exceeded.');
  assert.deepEqual(plataforma.screenshots, [1, 2, 3]);
  // Volta ao filtro a partir da 2ª cota, como o script legado.
  assert.equal(chamadas.filter((c) => c === 'voltar').length, 2);
  assert.ok(plataforma.chamadas.includes('finalizar 7'));
  assert.ok(chamadas.includes('close'));
  assert.ok(plataforma.eventos.some((e) => e.nivel === 'aviso' && /já tem 1 lance/.test(e.mensagem)));
});

test('nunca chama confirmar nem baixa PDF', async () => {
  const chamadas = [];
  const worker = new Worker({ config, plataforma: plataformaFalsa(), criarNewcon: newconFalso({}, chamadas), leitura: leituraFalsa, saida: silencio });
  await worker.processar(tarefa([cota(1, '006650', '0236'), cota(2, '006650', '0924')]));
  assert.ok(!chamadas.includes('CONFIRMAR') && !chamadas.includes('PDF'), `chamadas: ${chamadas.join(', ')}`);

  const fonte = fs.readFileSync(path.join(__dirname, '../src/worker.js'), 'utf8').replace(/\/\*[\s\S]*?\*\/|\/\/.*$/gm, '');
  for (const proibido of ['confirmAndWaitReport', 'downloadReportPdf', 'btnConfirma']) {
    assert.ok(!fonte.includes(proibido), `worker.js não pode usar ${proibido}`);
  }
});

test('recusa execução real sem abrir o Newcon', async () => {
  const chamadas = [];
  const plataforma = plataformaFalsa();
  const worker = new Worker({ config, plataforma, criarNewcon: newconFalso({}, chamadas), leitura: leituraFalsa, saida: silencio });
  await worker.processar(tarefa([cota(1, '006650', '0236')], 'real'));
  assert.deepEqual(chamadas, []);
  assert.match(plataforma.chamadas.join('|'), /finalizar 7: lance real desligado neste worker/);
});

test('falha no login finaliza com erro e não inicia cotas', async () => {
  const plataforma = plataformaFalsa();
  const worker = new Worker({ config, plataforma, criarNewcon: newconFalso({}, [], { falharLogin: true }), leitura: leituraFalsa, saida: silencio });
  await worker.processar(tarefa([cota(1, '006650', '0236')]));
  assert.deepEqual(plataforma.chamadas, ['finalizar 7: não foi possível entrar no Newcon: usuário ou senha inválidos']);
});

test('cancelamento: para antes da próxima cota', async () => {
  const plataforma = plataformaFalsa({ iniciar: (id) => (id === 2 ? { ok: false, cancelada: true, erro: 'cancelada' } : { ok: true }) });
  const worker = new Worker({ config, plataforma, criarNewcon: newconFalso({}, []), leitura: leituraFalsa, saida: silencio });
  await worker.processar(tarefa([cota(1, '006650', '0236'), cota(2, '006650', '0924'), cota(3, '006660', '1488')]));
  assert.deepEqual(plataforma.chamadas, ['iniciar 1', 'iniciar 2', 'finalizar 7']);
});

test('SIGTERM no meio da cota: termina a cota, não começa a próxima e devolve à fila', async () => {
  const plataforma = plataformaFalsa();
  let worker;
  const criarNewcon = newconFalso({}, [], { aoBuscar: (tag) => tag === '006650-0236-00' && worker.pedirParada('SIGTERM') });
  worker = new Worker({ config, plataforma, criarNewcon, leitura: leituraFalsa, saida: silencio });
  await worker.processar(tarefa([cota(1, '006650', '0236'), cota(2, '006650', '0924')]));
  assert.equal(plataforma.conclusoes[1].status, 'verificada');
  assert.deepEqual(plataforma.chamadas, ['iniciar 1', 'liberar 7']);
});

test('trava perdida: abandona sem finalizar', async () => {
  const plataforma = plataformaFalsa({ iniciar: () => ({ ok: false, parar: true, erro: 'não é mais deste worker' }) });
  const worker = new Worker({ config, plataforma, criarNewcon: newconFalso({}, []), leitura: leituraFalsa, saida: silencio });
  await worker.processar(tarefa([cota(1, '006650', '0236')]));
  assert.deepEqual(plataforma.chamadas, ['iniciar 1']);
});

test('cotas que não estão pendentes não são iniciadas', async () => {
  const plataforma = plataformaFalsa();
  const worker = new Worker({ config, plataforma, criarNewcon: newconFalso({}, []), leitura: leituraFalsa, saida: silencio });
  await worker.processar(tarefa([{ ...cota(1, '006650', '0236'), status: 'confirmacao_iniciada' }, cota(2, '006650', '0924')]));
  assert.deepEqual(plataforma.chamadas, ['iniciar 2', 'finalizar 7']);
});

test('registro: diálogo do Newcon vira aviso e caminho de arquivo não vai para a tela', async () => {
  const enviados = [];
  const registro = new Registro({ plataforma: { enviarEventos: async (_id, l) => { enviados.push(...l); return { ok: true }; } }, execucaoId: 1, saida: silencio });
  registro.info('dialog', { type: 'confirm', message: 'Cota com Parcelas em Atraso. Deseja prosseguir?' });
  registro.ok('dry-run: screenshot salvo', { file: '/tmp/x.png' });
  await registro.enviar();
  assert.deepEqual(enviados.map((e) => [e.nivel, e.mensagem]), [
    ['aviso', 'Aviso do Newcon (confirm): Cota com Parcelas em Atraso. Deseja prosseguir?'],
    ['ok', 'dry-run: screenshot salvo'],
  ]);
});

test('resumirHistorico conta os lances da assembleia atual', () => {
  const r = resumirHistorico(
    [{ protocolo: '1', assembleia: '15/09/2026' }, { protocolo: '2', assembleia: '15/09/2026' }, { protocolo: '3', assembleia: '17/08/2026' }],
    '15/09/2026'
  );
  assert.equal(r.lances_nesta_assembleia, 2);
  assert.equal(r.lances_no_historico, 3);
  assert.equal(resumirHistorico([{ protocolo: '1', assembleia: '15/09/2026' }], null).lances_nesta_assembleia, 0);
});

test('configuração recusa URL do Newcon com a grafia derrubada e token curto', () => {
  const base = { WORKER_TOKEN: 'x'.repeat(32), API_INTERNA_URL: 'http://api:8081', NEWCON_URL: 'https://cnp3.consorciocanopus.com.br/WWW/frmCorCcCnsLogin.aspx', NEWCON_USER: 'u', NEWCON_PASS: 'p' };
  assert.equal(carregarConfig(base).newcon.url, base.NEWCON_URL);
  assert.equal(carregarConfig(base).playwright.headless, true);
  assert.throws(() => carregarConfig({ ...base, NEWCON_URL: 'https://cnp3.consorciocanopus.com.br/WWW/frmCorCCCnsLogin.aspx' }), /frmCorCcCnsLogin/);
  assert.throws(() => carregarConfig({ ...base, WORKER_TOKEN: 'curto' }), /32 caracteres/);
  assert.equal(carregarConfig(base).lanceRealHabilitado, false);
  assert.equal(carregarConfig({ ...base, LANCE_REAL_HABILITADO: 'sim' }).lanceRealHabilitado, false, 'só "true" liga o lance real');
  assert.equal(carregarConfig({ ...base, LANCE_REAL_HABILITADO: 'true' }).lanceRealHabilitado, true);
});

test('prazo de oferta encerrado vira erro conhecido com o término', async () => {
  const plataforma = plataformaFalsa();
  const worker = new Worker({ config, plataforma, criarNewcon: newconFalso({ '006650-0236-00': 'prazo' }, []), leitura: leituraFalsa, saida: silencio });
  await worker.processar(tarefa([cota(1, '006650', '0236')]));
  assert.deepEqual(
    { status: plataforma.conclusoes[1].status, erro_tipo: plataforma.conclusoes[1].erro_tipo, erro: plataforma.conclusoes[1].erro },
    { status: 'erro_antes_confirmar', erro_tipo: 'conhecido', erro: 'prazo de oferta de lance encerrado (término em 15/09/2026 às 14:00)' }
  );
});

test('lance real desligado: recusa a execução sem carregar lance-real.js', async () => {
  const chamadas = [];
  const plataforma = plataformaFalsa();
  let carregou = false;
  const modulos = { lanceReal: () => { carregou = true; return require('../src/lance-real'); }, reimpressao: () => require('../src/reimpressao') };
  const worker = new Worker({ config: { ...config, lanceRealHabilitado: false }, plataforma, criarNewcon: newconFalso({}, chamadas), leitura: leituraFalsa, modulos, saida: silencio });
  await worker.processar(tarefa([cota(1, '006650', '0236')], 'real'));
  assert.equal(carregou, false);
  assert.deepEqual(chamadas, []);
  assert.match(plataforma.chamadas.join('|'), /finalizar 7: lance real desligado/);
  assert.ok(!Object.keys(require.cache).some((k) => k.endsWith('lance-real.js')), 'lance-real.js não pode ter sido carregado');
});

test('reimpressão e dry-run não usam o botão Confirmar', () => {
  const semComentarios = (arquivo) => fs.readFileSync(path.join(__dirname, '../src', arquivo), 'utf8').replace(/\/\*[\s\S]*?\*\/|\/\/.*$/gm, '');
  for (const proibido of ['confirmAndWaitReport', 'btnConfirma']) {
    assert.ok(!semComentarios('reimpressao.js').includes(proibido), `reimpressao.js não pode usar ${proibido}`);
    assert.ok(!semComentarios('leitura-credenciamento.js').includes(proibido), `leitura-credenciamento.js não pode usar ${proibido}`);
  }
});
