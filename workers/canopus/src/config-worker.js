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

  // As duas grafias da tela de login funcionam (medido em 16/11/2026). O que derruba a conexão
  // é abrir a página sem cabeçalho Referer, e disso cuida o NewconClient — por isso a grafia
  // frmCorCCCnsLogin.aspx, que é a do botão "NewCon" no Portal do Parceiro, não é mais recusada.
  const newconUrl = obrigatoria(env, 'NEWCON_URL');

  const tmp = env.WORKER_TMP_DIR || path.join(os.tmpdir(), 'travus-worker');
  return {
    // Lance real só com "true" explícito. A API tem a sua própria chave com o mesmo nome.
    lanceRealHabilitado: String(env.LANCE_REAL_HABILITADO || '').trim() === 'true',
    api: { url: obrigatoria(env, 'API_INTERNA_URL').replace(/\/+$/, ''), token },
    worker: {
      nome: String(env.WORKER_NOME || `canopus@${os.hostname()}`).slice(0, 100),
      intervaloFilaMs: Number(env.WORKER_INTERVALO_FILA_MS || 5000),
      renovarTravaMs: 30000,
    },
    newcon: {
      url: newconUrl,
      user: obrigatoria(env, 'NEWCON_USER'),
      pass: obrigatoria(env, 'NEWCON_PASS'),
      // Em branco, o NewconClient usa o endereço do Portal do Parceiro (REFERER_PADRAO).
      referer: String(env.NEWCON_REFERER || '').trim(),
    },
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
