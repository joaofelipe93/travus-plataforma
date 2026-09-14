#!/usr/bin/env node
'use strict';

/**
 * Executa apenas o fluxo de OAuth do Google Drive, gerando/atualizando o token.json.
 * Uso:
 *   node src/auth.js            (interativo — imprime URL para abrir no navegador)
 *
 * No Docker, defina DRIVE_OAUTH_PORT e BIND_HOST e mapeie a porta:
 *   docker compose run --rm --service-ports newcon npm run auth
 */

// Reaproveita o config existente, mas ignora a validação de dry-run/confirm.
process.argv.push('--dry-run'); // truque: satisfaz o check do config sem afetar nada

const config = require('./config');
const { Logger } = require('./logger');
const { DriveClient } = require('./drive');

async function main() {
  const logger = new Logger(config.paths.logDir);
  logger.info('auth: iniciando fluxo OAuth Google Drive');
  const drive = new DriveClient({ config, logger });
  await drive.init();
  logger.ok('auth: pronto. token salvo em ' + config.drive.tokenPath);
  logger.close();
}

main().catch((e) => {
  console.error('\nErro no OAuth:', e.message);
  console.error(e.stack);
  process.exit(1);
});
