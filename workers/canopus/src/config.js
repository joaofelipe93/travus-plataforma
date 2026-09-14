'use strict';

const path = require('path');
const fs = require('fs');
const dotenv = require('dotenv');
const yargs = require('yargs/yargs');
const { hideBin } = require('yargs/helpers');

dotenv.config();

function required(name) {
  const v = process.env[name];
  if (!v || !String(v).trim()) {
    throw new Error(`Variável de ambiente obrigatória ausente: ${name} (veja .env.example)`);
  }
  return String(v).trim();
}

function optional(name, fallback) {
  const v = process.env[name];
  return v && String(v).trim() ? String(v).trim() : fallback;
}

function toBool(v, fallback) {
  if (v === undefined || v === null || v === '') return fallback;
  return /^(1|true|yes|sim|y|s)$/i.test(String(v).trim());
}

const argv = yargs(hideBin(process.argv))
  .option('dry-run', {
    type: 'boolean',
    describe: 'Não clica em Confirmar. Para na tela de credenciamento e salva screenshot para validação.',
    // Sem `default`: no yargs 17 um default faz o .conflicts() disparar sempre.
  })
  .option('confirm', {
    type: 'boolean',
    describe: 'MODO REAL: clica em Confirmar (registra o lance) e baixa o PDF gerado.',
  })
  .option('limit', {
    type: 'number',
    describe: 'Processa apenas as N primeiras linhas do CSV (útil pra testar).',
  })
  .conflicts('dry-run', 'confirm')
  .check((args) => {
    if (!args['dry-run'] && !args.confirm) {
      throw new Error('Escolha um modo: --dry-run (seguro) ou --confirm (registra lance real).');
    }
    return true;
  })
  .strict()
  .help()
  .parse();

const root = path.resolve(__dirname, '..');
const resolve = (p) => (path.isAbsolute(p) ? p : path.resolve(root, p));

const config = {
  mode: argv.confirm ? 'confirm' : 'dry-run',
  limit: argv.limit,
  newcon: {
    url: required('NEWCON_URL'),
    user: required('NEWCON_USER'),
    pass: required('NEWCON_PASS'),
  },
  csvPath: resolve(optional('CSV_PATH', './cotasreal.csv')),
  drive: {
    folderId: optional('DRIVE_FOLDER_ID', ''),
    clientId: optional('GOOGLE_CLIENT_ID', ''),
    clientSecret: optional('GOOGLE_CLIENT_SECRET', ''),
    credentialsPath: resolve(optional('GOOGLE_CREDENTIALS_PATH', './credentials.json')),
    tokenPath: resolve(optional('GOOGLE_TOKEN_PATH', './token.json')),
  },
  playwright: {
    timeoutMs: Number(optional('PLAYWRIGHT_TIMEOUT_SECONDS', '30')) * 1000,
    headless: !toBool(process.env.HEADFUL, true),
  },
  paths: {
    downloadDir: resolve(optional('DOWNLOAD_DIR', './downloads')),
    screenshotDir: resolve(optional('SCREENSHOT_DIR', './screenshots')),
    logDir: resolve(optional('LOG_DIR', './logs')),
  },
};

for (const dir of [config.paths.downloadDir, config.paths.screenshotDir, config.paths.logDir]) {
  fs.mkdirSync(dir, { recursive: true });
}

if (!fs.existsSync(config.csvPath)) {
  throw new Error(`CSV não encontrado em: ${config.csvPath} (defina CSV_PATH no .env)`);
}

if (config.mode === 'confirm' && !config.drive.folderId) {
  throw new Error('DRIVE_FOLDER_ID é obrigatório no modo --confirm (o script vai fazer upload dos PDFs).');
}

module.exports = config;
