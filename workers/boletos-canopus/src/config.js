'use strict';

// Configuração do worker de boletos: só variáveis de ambiente, validadas na subida (o container
// morre cedo em vez de falhar no meio de uma rodada). Nada de segredo da Canopus aqui: o Boleto
// Fácil não tem login — a credencial é o CPF do cliente, que vem da API a cada cliente.

const os = require('os');

const SITE_PADRAO = 'https://www.consorciocanopus.com.br';
const BASE_PADRAO = '/extensions/boleto-facil-rolledback/public/index.php';
// Chrome normal de propósito: o padrão do Playwright ("HeadlessChrome") leva 403 da Cloudflare
// até de um IP que o curl atravessa (medido em 16/10/2026). Ver docs/canopus-boleto-facil.md.
const UA_PADRAO =
  'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36';

function texto(nome, padrao) {
  const v = (process.env[nome] || '').trim();
  return v === '' ? padrao : v;
}

function obrigatorio(nome) {
  const v = texto(nome, '');
  if (v === '') throw new Error(`${nome} é obrigatória`);
  return v;
}

function inteiro(nome, padrao, { min = 0, max = Number.MAX_SAFE_INTEGER } = {}) {
  const bruto = texto(nome, '');
  if (bruto === '') return padrao;
  const n = Number(bruto);
  if (!Number.isInteger(n) || n < min || n > max) {
    throw new Error(`${nome} precisa ser um inteiro entre ${min} e ${max} (recebido ${bruto})`);
  }
  return n;
}

function booleano(nome, padrao) {
  const v = texto(nome, '').toLowerCase();
  if (v === '') return padrao;
  return v === 'true' || v === '1' || v === 'sim';
}

function carregar() {
  const site = texto('BOLETO_SITE', SITE_PADRAO).replace(/\/+$/, '');
  const modo = texto('BOLETOS_MODO', 'navegador').toLowerCase();
  if (modo !== 'navegador' && modo !== 'http') {
    throw new Error(`BOLETOS_MODO precisa ser "navegador" ou "http" (recebido ${modo})`);
  }
  const token = obrigatorio('WORKER_TOKEN');
  if (token.length < 32) throw new Error('WORKER_TOKEN precisa ter ao menos 32 caracteres');

  return {
    api: {
      url: obrigatorio('API_INTERNA_URL').replace(/\/+$/, ''),
      token,
      timeoutMs: inteiro('BOLETOS_API_TIMEOUT_MS', 30000, { min: 1000 }),
    },
    worker: {
      nome: texto('WORKER_NOME', `boletos-${os.hostname()}`).slice(0, 100),
      // Intervalo de espera quando não há rodada na fila.
      intervaloFilaMs: inteiro('BOLETOS_INTERVALO_FILA_MS', 15000, { min: 1000 }),
      renovarTravaMs: inteiro('BOLETOS_RENOVAR_TRAVA_MS', 30000, { min: 5000 }),
    },
    site: {
      modo,
      site,
      base: texto('BOLETO_BASE', site + BASE_PADRAO).replace(/\/+$/, ''),
      userAgent: texto('BOLETO_UA', UA_PADRAO),
      timeoutMs: inteiro('BOLETOS_TIMEOUT_MS', 45000, { min: 5000 }),
      headless: !booleano('HEADFUL', false),
      // O site tem limite de taxa (429). Estas três variáveis são o que impede a rodada de
      // virar uma rajada: pausa entre clientes, entre cotas e recuo quando toma 429.
      pausaClienteMs: inteiro('BOLETOS_PAUSA_CLIENTE_MS', 4000, { min: 0 }),
      pausaCotaMs: inteiro('BOLETOS_PAUSA_COTA_MS', 1500, { min: 0 }),
      tentativas429: inteiro('BOLETOS_TENTATIVAS_429', 3, { min: 1, max: 10 }),
      espera429Ms: inteiro('BOLETOS_ESPERA_429_MS', 20000, { min: 1000 }),
    },
  };
}

module.exports = { carregar, SITE_PADRAO, BASE_PADRAO, UA_PADRAO };
