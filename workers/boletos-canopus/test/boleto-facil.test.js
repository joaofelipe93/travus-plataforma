'use strict';

// Cliente do Boleto Fácil contra um servidor falso, no modo 'http'. **Nenhum teste fala com o
// site de verdade** (mesma regra do worker do Newcon): o dublê imita as respostas medidas em
// 16/10/2026, inclusive o 429 e o 403 da Cloudflare.

const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const { criarCliente, ErroLimiteDeTaxa, ErroBoletoFacil } = require('../src/boleto-facil');

const PDF = Buffer.from('%PDF-1.4\nboleto ficticio\n%%EOF\n');

/** Servidor falso. `rotas` mapeia caminho → função(req, res) ou objeto de resposta. */
async function servidorFalso(rotas) {
  const recebidos = [];
  const servidor = http.createServer((req, res) => {
    recebidos.push({ url: req.url, cabecalhos: req.headers });
    const rota = Object.keys(rotas).find((r) => req.url.startsWith(r));
    if (!rota) {
      res.writeHead(404).end('não encontrado');
      return;
    }
    const resposta = rotas[rota];
    if (typeof resposta === 'function') return resposta(req, res, recebidos);
    res.writeHead(resposta.status || 200, resposta.cabecalhos || { 'Content-Type': 'text/html' });
    res.end(resposta.corpo);
  });
  await new Promise((r) => servidor.listen(0, '127.0.0.1', r));
  const { port } = servidor.address();
  return {
    base: `http://127.0.0.1:${port}/index.php`,
    site: `http://127.0.0.1:${port}`,
    recebidos,
    fechar: () => new Promise((r) => servidor.close(r)),
  };
}

function configDe(s, extra = {}) {
  return {
    modo: 'http',
    site: s.site,
    base: s.base,
    userAgent: 'Chrome/teste',
    timeoutMs: 5000,
    headless: true,
    pausaClienteMs: 0,
    pausaCotaMs: 0,
    tentativas429: 2,
    espera429Ms: 10,
    ...extra,
  };
}

test('percorre find-cota, generate-bill e a impressão', async () => {
  const s = await servidorFalso({
    '/index.php/find-cota/': {
      corpo: JSON.stringify({ cotas: [{ CD_Grupo: '006650', CD_Cota: 924, Versao: 0, ID_Cota: 468379 }] }),
    },
    // Como no site: Content-Type text/html com corpo JSON.
    '/index.php/generate-bill/': {
      corpo: JSON.stringify({
        bills: [{ id: 39871, parcelNumber: '018', date: '2026-11-16', price: 644.62, type: 'overdue', group: '006650', cota: '0924', transactionId: 179831728 }],
      }),
    },
    '/index.php/print-overdue-bill/': (req, res) => {
      res.writeHead(200, { 'Content-Type': 'application/pdf' }).end(PDF);
    },
  });
  try {
    const cliente = criarCliente(configDe(s));
    await cliente.abrir();

    const cotas = await cliente.buscarCotas('111.222.333-96');
    assert.equal(cotas.length, 1);
    assert.ok(s.recebidos[0].url.endsWith('/find-cota/11122233396'), 'o documento vai só com dígitos');
    assert.equal(s.recebidos[0].cabecalhos['user-agent'], 'Chrome/teste');
    assert.ok(s.recebidos[0].cabecalhos.referer.endsWith('/boletofacil/'));

    const { parcelas } = await cliente.buscarParcelas({ grupo: '006650', cota: '0924', idCota: '468379' });
    assert.equal(parcelas.length, 1);

    const pdf = await cliente.baixarPdf(parcelas[0]);
    assert.deepEqual(pdf, PDF);
    await cliente.fechar();
  } finally {
    await s.fechar();
  }
});

test('documento inválido não chega a sair do worker', async () => {
  const s = await servidorFalso({});
  try {
    const cliente = criarCliente(configDe(s));
    await assert.rejects(() => cliente.buscarCotas('123'), ErroBoletoFacil);
    assert.equal(s.recebidos.length, 0, 'nada deve ser pedido ao site');
  } finally {
    await s.fechar();
  }
});

test('429 recua e tenta de novo; insistindo, desiste com erro claro', async () => {
  let tentativas = 0;
  const s = await servidorFalso({
    '/index.php/find-cota/': (req, res) => {
      tentativas += 1;
      if (tentativas === 1) {
        res.writeHead(429).end('Too Many Requests');
        return;
      }
      res.writeHead(200).end(JSON.stringify({ cotas: [] }));
    },
  });
  try {
    const cliente = criarCliente(configDe(s));
    assert.deepEqual(await cliente.buscarCotas('11122233396'), []);
    assert.equal(tentativas, 2, 'a segunda tentativa é a que passa');
  } finally {
    await s.fechar();
  }

  const sempre429 = await servidorFalso({
    '/index.php/find-cota/': (req, res) => res.writeHead(429).end('Too Many Requests'),
  });
  try {
    const cliente = criarCliente(configDe(sempre429));
    await assert.rejects(() => cliente.buscarCotas('11122233396'), ErroLimiteDeTaxa);
  } finally {
    await sempre429.fechar();
  }
});

test('403 da Cloudflare vira erro inesperado, não "cliente sem boleto"', async () => {
  const s = await servidorFalso({
    '/index.php/find-cota/': (req, res) => res.writeHead(403).end('<html>blocked</html>'),
  });
  try {
    const cliente = criarCliente(configDe(s));
    await assert.rejects(
      () => cliente.buscarCotas('11122233396'),
      (e) => e instanceof ErroBoletoFacil && e.conhecido === false && /403/.test(e.message),
    );
  } finally {
    await s.fechar();
  }
});

test('cota sem nada em aberto: o site erra e isso é normal', async () => {
  const s = await servidorFalso({
    '/index.php/generate-bill/': (req, res) => res.writeHead(500).end('erro'),
  });
  try {
    const cliente = criarCliente(configDe(s));
    const r = await cliente.buscarParcelas({ grupo: '006650', cota: '0924', idCota: '1' });
    assert.equal(r.semValores, true);
    assert.deepEqual(r.parcelas, []);
  } finally {
    await s.fechar();
  }
});

test('impressão que não devolve PDF é recusada', async () => {
  const s = await servidorFalso({
    '/index.php/print-overdue-bill/': (req, res) => res.writeHead(200, { 'Content-Type': 'text/html' }).end('<html>opa</html>'),
  });
  try {
    const cliente = criarCliente(configDe(s));
    await assert.rejects(
      () => cliente.baixarPdf({ urlPdf: `${s.base}/print-overdue-bill/1/2/3/4` }),
      (e) => e instanceof ErroBoletoFacil && /não devolveu um PDF/.test(e.message),
    );
  } finally {
    await s.fechar();
  }
});

test('o cookie da sessão volta nos pedidos seguintes', async () => {
  const s = await servidorFalso({
    '/index.php/find-cota/': (req, res) => {
      res.writeHead(200, { 'Set-Cookie': 'slim_session=abc123; path=/' }).end(JSON.stringify({ cotas: [] }));
    },
    '/index.php/generate-bill/': (req, res) => {
      assert.equal(req.headers.cookie, 'slim_session=abc123');
      res.writeHead(200).end(JSON.stringify({ bills: [] }));
    },
  });
  try {
    const cliente = criarCliente(configDe(s));
    await cliente.buscarCotas('11122233396');
    await cliente.buscarParcelas({ grupo: '006650', cota: '0924', idCota: '1' });
  } finally {
    await s.fechar();
  }
});
