'use strict';

// O laço do worker, com dublês da API e do Boleto Fácil. Garante o que não dá para ver nos
// módulos isolados: o que acontece com um cliente que dá erro, com cota que não está no
// cadastro, e que a rodada de cobrança NÃO baixa PDF.

const test = require('node:test');
const assert = require('node:assert/strict');
const { processarCliente, processarRodada } = require('../src/worker');

const PDF = Buffer.from('%PDF-1.4\nficticio\n%%EOF\n');

function registroFalso() {
  const linhas = [];
  const guardar = (nivel) => (mensagem) => {
    linhas.push({ nivel, mensagem });
    return Promise.resolve();
  };
  return {
    linhas,
    info: guardar('info'),
    ok: guardar('ok'),
    aviso: guardar('aviso'),
    erro: guardar('erro'),
    comCliente() {
      return this;
    },
    descarregar: () => Promise.resolve(),
  };
}

function plataformaFalsa() {
  const chamadas = { parcelas: [], pdfs: [], conclusoes: [], finalizada: null, liberada: null };
  let proximoId = 100;
  return {
    chamadas,
    async registrarParcelas(clienteId, parcelas) {
      chamadas.parcelas.push({ clienteId, parcelas });
      return parcelas.map(() => proximoId++);
    },
    async enviarPdf(parcelaId, bytes) {
      chamadas.pdfs.push({ parcelaId, bytes: bytes.length });
      return 'arquivo-ficticio';
    },
    async concluirCliente(clienteId, resultado) {
      chamadas.conclusoes.push({ clienteId, resultado });
      return { ok: true };
    },
    async enviarEventos() {},
    async finalizar(rodadaId, erro) {
      chamadas.finalizada = { rodadaId, erro: erro || null };
      return { ok: true, status: erro ? 'falhou' : 'concluida' };
    },
    async liberar(rodadaId) {
      chamadas.liberada = rodadaId;
      return { ok: true };
    },
    async renovar() {
      return { ok: true, cancelamentoSolicitado: false };
    },
  };
}

/** Dublê do site: uma cota com uma parcela em aberto. */
function boletoFalso({ cotas, parcelasPorCota = {}, erroEm } = {}) {
  const chamadas = { cotas: [], parcelas: [], pdfs: [] };
  return {
    chamadas,
    modo: 'dublê',
    abrir: async () => {},
    fechar: async () => {},
    async buscarCotas(documento) {
      chamadas.cotas.push(documento);
      if (erroEm === 'cotas') throw Object.assign(new Error('o site caiu'), { name: 'ErroBoletoFacil', conhecido: true });
      return cotas;
    },
    async buscarParcelas(cota) {
      chamadas.parcelas.push(cota.tag);
      const parcelas = parcelasPorCota[cota.tag] || [];
      return { parcelas, descartadas: [], semValores: parcelas.length === 0 };
    },
    async baixarPdf(parcela) {
      chamadas.pdfs.push(parcela.parcela);
      return PDF;
    },
  };
}

const CLIENTE = {
  id: 7,
  nome: 'CLIENTE DE TESTE',
  documento: '11122233396',
  cotas: [{ id: 42, grupo: '006650', cota: '0924', versao: '00' }],
};

const PARCELA = {
  parcela: '018',
  vencimento: '2026-11-16',
  valorCentavos: 64462,
  tipo: 'overdue',
  chargeId: 39871,
  transactionId: 179831728,
  urlPdf: 'https://exemplo.invalid/print-overdue-bill/006650/0924/39871/179831728',
};

test('envio: grava a parcela e manda o PDF', async () => {
  const boleto = boletoFalso({
    cotas: [{ CD_Grupo: '006650', CD_Cota: 924, Versao: 0, ID_Cota: 468379 }],
    parcelasPorCota: { '006650-0924-00': [PARCELA] },
  });
  const plataforma = plataformaFalsa();
  const r = await processarCliente({ boleto, plataforma, registro: registroFalso(), cliente: CLIENTE, tipo: 'envio' });

  assert.equal(r.status, 'consultado');
  assert.equal(r.cotas_no_site, 1);
  assert.equal(r.cotas_casadas, 1);
  assert.equal(plataforma.chamadas.parcelas.length, 1);
  assert.deepEqual(plataforma.chamadas.parcelas[0].parcelas[0], {
    cota_id: 42,
    parcela: '018',
    vencimento: '2026-11-16',
    valor_centavos: 64462,
    tipo: 'overdue',
    charge_id: 39871,
    transaction_id: 179831728,
  });
  assert.equal(plataforma.chamadas.pdfs.length, 1, 'o PDF do envio é guardado');
});

test('cobrança: grava a parcela e NÃO baixa PDF', async () => {
  const boleto = boletoFalso({
    cotas: [{ CD_Grupo: '006650', CD_Cota: 924, Versao: 0, ID_Cota: 468379 }],
    parcelasPorCota: { '006650-0924-00': [PARCELA] },
  });
  const plataforma = plataformaFalsa();
  const r = await processarCliente({ boleto, plataforma, registro: registroFalso(), cliente: CLIENTE, tipo: 'cobranca' });

  assert.equal(r.status, 'consultado');
  assert.equal(plataforma.chamadas.parcelas.length, 1);
  assert.deepEqual(boleto.chamadas.pdfs, [], 'a cobrança não imprime: é um pedido a menos na rota que dá 429');
  assert.deepEqual(plataforma.chamadas.pdfs, []);
});

test('nada em aberto vira sem_boleto', async () => {
  const boleto = boletoFalso({ cotas: [{ CD_Grupo: '006650', CD_Cota: 924, Versao: 0, ID_Cota: 1 }] });
  const plataforma = plataformaFalsa();
  const r = await processarCliente({ boleto, plataforma, registro: registroFalso(), cliente: CLIENTE, tipo: 'envio' });
  assert.equal(r.status, 'sem_boleto');
  assert.equal(plataforma.chamadas.parcelas.length, 0);
});

test('documento sem cota na Canopus avisa (é CPF errado no cadastro)', async () => {
  const boleto = boletoFalso({ cotas: [] });
  const registro = registroFalso();
  const r = await processarCliente({ boleto, plataforma: plataformaFalsa(), registro, cliente: CLIENTE, tipo: 'envio' });
  assert.equal(r.status, 'sem_boleto');
  assert.equal(r.cotas_no_site, 0);
  assert.ok(registro.linhas.some((l) => l.nivel === 'aviso' && /não encontrou cota/.test(l.mensagem)));
});

test('cota que existe no site e não no cadastro não gera boleto, mas avisa', async () => {
  const boleto = boletoFalso({
    cotas: [
      { CD_Grupo: '006650', CD_Cota: 924, Versao: 0, ID_Cota: 1 },
      { CD_Grupo: '006620', CD_Cota: 1372, Versao: 0, ID_Cota: 2 },
    ],
    parcelasPorCota: { '006650-0924-00': [PARCELA], '006620-1372-00': [PARCELA] },
  });
  const plataforma = plataformaFalsa();
  const registro = registroFalso();
  const r = await processarCliente({ boleto, plataforma, registro, cliente: CLIENTE, tipo: 'envio' });

  assert.equal(r.cotas_no_site, 2);
  assert.equal(r.cotas_casadas, 1);
  assert.deepEqual(boleto.chamadas.parcelas, ['006650-0924-00'], 'só a cota cadastrada é consultada');
  assert.ok(registro.linhas.some((l) => /006620-1372-00 existe na Canopus e não está no cadastro/.test(l.mensagem)));
});

test('erro conhecido do site vira erro do cliente, com o motivo', async () => {
  const boleto = boletoFalso({ cotas: [], erroEm: 'cotas' });
  const r = await processarCliente({ boleto, plataforma: plataformaFalsa(), registro: registroFalso(), cliente: CLIENTE, tipo: 'envio' });
  assert.equal(r.status, 'erro');
  assert.equal(r.erro_tipo, 'conhecido');
  assert.match(r.erro, /o site caiu/);
});

test('PDF que falha não derruba a parcela já registrada', async () => {
  const boleto = boletoFalso({
    cotas: [{ CD_Grupo: '006650', CD_Cota: 924, Versao: 0, ID_Cota: 1 }],
    parcelasPorCota: { '006650-0924-00': [PARCELA] },
  });
  boleto.baixarPdf = async () => {
    throw new Error('429 de novo');
  };
  const plataforma = plataformaFalsa();
  const registro = registroFalso();
  const r = await processarCliente({ boleto, plataforma, registro, cliente: CLIENTE, tipo: 'envio' });

  assert.equal(r.status, 'consultado', 'a parcela conta mesmo sem PDF: a cobrança precisa dela');
  assert.equal(plataforma.chamadas.parcelas.length, 1);
  assert.deepEqual(plataforma.chamadas.pdfs, []);
  assert.ok(registro.linhas.some((l) => l.nivel === 'aviso' && /sem PDF/.test(l.mensagem)));
});

test('a rodada percorre os clientes e finaliza', async () => {
  const clientes = [
    { ...CLIENTE, id: 1 },
    { ...CLIENTE, id: 2, nome: 'OUTRO CLIENTE' },
  ];
  const plataforma = plataformaFalsa();
  plataforma.proximoCliente = async () => clientes.shift() || null;

  const config = {
    worker: { renovarTravaMs: 60000 },
    site: {
      modo: 'http', site: 'https://exemplo.invalid', base: 'https://exemplo.invalid/index.php',
      userAgent: 'Chrome/teste', timeoutMs: 1000, headless: true,
      pausaClienteMs: 0, pausaCotaMs: 0, tentativas429: 1, espera429Ms: 1,
    },
  };
  // Dublê injetado: nenhum teste deste worker fala com o Boleto Fácil de verdade.
  const boleto = boletoFalso({ cotas: [] });
  await processarRodada({
    config, plataforma, rodada: { id: 5, tipo: 'envio' }, parar: { pedido: false },
    criarBoleto: () => boleto,
  });

  assert.deepEqual(plataforma.chamadas.conclusoes.map((c) => c.clienteId), [1, 2]);
  assert.deepEqual(boleto.chamadas.cotas, ['11122233396', '11122233396'], 'consultou os dois clientes');
  assert.deepEqual(plataforma.chamadas.finalizada, { rodadaId: 5, erro: null });
  for (const c of plataforma.chamadas.conclusoes) {
    assert.equal(c.resultado.status, 'sem_boleto', `cliente ${c.clienteId}: ${c.resultado.erro || ''}`);
  }
});
