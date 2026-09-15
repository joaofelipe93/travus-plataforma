'use strict';

const os = require('os');
const path = require('path');

/**
 * Configuração do worker da plataforma, só por variáveis de ambiente (sem CSV nem
 * argumentos: as cotas vêm da API). O objeto tem o mesmo formato que o NewconClient espera.
 */

function obrigatoria(env, nome) {
  const valor = String(env[nome] || '').trim();
  if (!valor) throw new Error(`Variável de ambiente obrigatória ausente: ${nome}`);
  return valor;
}

function booleano(valor, padrao) {
  if (valor === undefined || valor === null || valor === '') return padrao;
  return /^(1|true|yes|sim|y|s)$/i.test(String(valor).trim());
}

function carregarConfig(env = process.env) {
  const token = obrigatoria(env, 'WORKER_TOKEN');
  if (token.length < 32) throw new Error('WORKER_TOKEN precisa ter pelo menos 32 caracteres');

  const newconUrl = obrigatoria(env, 'NEWCON_URL');
  // O servidor derruba a conexão com a grafia do próprio redirect (frmCorCCCnsLogin).
  if (/frmCorCCCnsLogin\.aspx/.test(newconUrl)) {
    throw new Error('NEWCON_URL deve terminar em frmCorCcCnsLogin.aspx (com "Cc"): a grafia frmCorCCCnsLogin.aspx tem a conexão derrubada');
  }

  const tmp = env.WORKER_TMP_DIR || path.join(os.tmpdir(), 'travus-worker');
  return {
    api: { url: obrigatoria(env, 'API_INTERNA_URL').replace(/\/+$/, ''), token },
    worker: {
      nome: String(env.WORKER_NOME || `canopus@${os.hostname()}`).slice(0, 100),
      intervaloFilaMs: Number(env.WORKER_INTERVALO_FILA_MS || 5000),
      renovarTravaMs: 30000,
    },
    newcon: { url: newconUrl, user: obrigatoria(env, 'NEWCON_USER'), pass: obrigatoria(env, 'NEWCON_PASS') },
    playwright: {
      timeoutMs: Number(env.PLAYWRIGHT_TIMEOUT_SECONDS || 30) * 1000,
      headless: !booleano(env.HEADFUL, false),
    },
    paths: {
      screenshotDir: path.join(tmp, 'screenshots'),
      downloadDir: path.join(tmp, 'downloads'),
    },
  };
}

module.exports = { carregarConfig };
