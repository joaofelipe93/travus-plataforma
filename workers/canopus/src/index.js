#!/usr/bin/env node
'use strict';

const config = require('./config');
const { Logger } = require('./logger');
const { readCotas } = require('./csv');
const { NewconClient, NewconCotaError } = require('./newcon');
const { DriveClient } = require('./drive');

async function main() {
  const logger = new Logger(config.paths.logDir);
  logger.info('inicio', { mode: config.mode, csv: config.csvPath });

  const cotas = readCotas(config.csvPath);
  const list = config.limit ? cotas.slice(0, config.limit) : cotas;
  logger.info('cotas carregadas', { total: cotas.length, processando: list.length });

  const drive = config.mode === 'confirm' ? new DriveClient({ config, logger }) : null;
  if (drive) await drive.init();

  const client = new NewconClient({ config, logger });
  await client.start();

  const results = [];

  try {
    await client.login();
    await client.goToCredenciamento();

    for (let i = 0; i < list.length; i++) {
      const cota = list[i];
      const tag = `${cota.grupo}-${cota.cota}-${cota.versao}`;
      logger.info(`--- (${i + 1}/${list.length}) ${tag}${cota.nome ? ` ${cota.nome}` : ''} ---`);
      const result = { ...cota, status: 'pending' };

      try {
        if (i > 0) await client.backToFilter();
        await client.searchCota(cota);
        await client.selectSegundoFixo();

        if (config.mode === 'dry-run') {
          const shot = await client.captureBeforeConfirm(cota);
          result.status = 'dry-run-ok';
          result.screenshot = shot;
        } else {
          result.confirmAttempted = true; // daqui em diante o lance pode ter sido registrado
          await client.confirmAndWaitReport();
          const pdfPath = await client.downloadReportPdf(cota);
          const uploaded = await drive.uploadPdf(pdfPath);
          result.status = 'confirmed';
          result.localPdf = pdfPath;
          result.driveFileId = uploaded.id;
          result.driveLink = uploaded.webViewLink;
        }
        logger.ok('cota processada', { tag, status: result.status });
      } catch (err) {
        result.status = 'error';
        // Situações conhecidas já vêm explicadas; nos demais erros, a 1ª linha basta no resumo.
        const known = err instanceof NewconCotaError;
        result.error = known ? err.message : err.message.split('\n')[0];
        const shot = await client.snapshotError(tag);
        if (shot) result.errorScreenshot = shot;
        logger.error('cota falhou', known ? { tag, motivo: result.error } : { tag, err: err.message, stack: err.stack });
      }
      results.push(result);
    }
  } finally {
    await client.close();
    logger.info('encerrando');
    logger.close();
  }

  // Resumo final
  const ok = results.filter((r) => r.status === 'dry-run-ok' || r.status === 'confirmed').length;
  const err = results.filter((r) => r.status === 'error').length;
  console.log(`\nResumo: ${ok} ok, ${err} erro(s), ${results.length} total.`);
  for (const r of results.filter((x) => x.status === 'error')) {
    const lance = r.confirmAttempted
      ? 'ATENÇÃO: erro depois de clicar em Confirmar — o lance pode ter sido registrado, confira no Histórico'
      : 'lance não registrado';
    console.log(`  x ${r.grupo}-${r.cota}-${r.versao}${r.nome ? ` ${r.nome}` : ''}: ${r.error} [${lance}]`);
  }
  if (config.mode === 'dry-run') {
    console.log('Modo dry-run — nenhum lance foi confirmado.');
    console.log(`Screenshots em: ${config.paths.screenshotDir}`);
  }

  if (err > 0) process.exit(1);
}

main().catch((e) => {
  console.error('\nErro fatal:', e.message);
  console.error(e.stack);
  process.exit(2);
});
