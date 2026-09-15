'use strict';

/**
 * Reimpressão de comprovante pelo Histórico do Newcon: não registra lance.
 * "Localizar" abre a tela de credenciamento (aí abrimos o Histórico) ou, para cotas sem
 * credenciamento disponível, direto a página do Histórico. Na grade, o botão de reimprimir
 * da linha do protocolo abre o visualizador Stimulsoft, de onde vem o PDF.
 */

const fs = require('fs');
const { prazoEncerrado } = require('./avisos-newcon');

const primeiraLinha = (e) => String((e && e.message) || e).split('\n')[0];
const tagCota = (c) => `${c.grupo}-${c.cota}-${c.versao}`;

class ErroReimpressao extends Error {
  constructor(mensagem) {
    super(mensagem);
    this.name = 'ErroReimpressao';
  }
}

/** @param {object} ctx mesmo contexto de processarCotaReal. */
async function processarReimpressao(ctx) {
  const { newcon, registro, plataforma, cota, config, leitura } = ctx;
  const tag = tagCota(cota);
  const protocolo = cota.protocolo;
  const dialogosAntes = registro.dialogos.length;

  try {
    if (ctx.voltarAoFiltro) await newcon.backToFilter();
    let paginaDoHistorico = false;
    try {
      await newcon.searchCota(cota);
    } catch (e) {
      if (!(e && e.name === 'NewconCotaError' && /Histórico das Ofertas/.test(e.message))) throw e;
      paginaDoHistorico = true; // cota sem credenciamento: a grade já está na tela
    }
    if (!paginaDoHistorico) await leitura.abrirHistorico(newcon.page, config.playwright.timeoutMs);

    const linhas = await leitura.lerGradeHistorico(newcon.page, config.playwright.timeoutMs);
    const linha = linhas.find((l) => l.protocolo === protocolo);
    if (!linha) {
      throw new ErroReimpressao(`o protocolo ${protocolo} não aparece no Histórico da cota (${linhas.length} lance(s) listados)`);
    }

    await leitura.reimprimirProtocolo(newcon.page, protocolo, config.playwright.timeoutMs);
    const pdf = await newcon.downloadReportPdf({ ...cota, nome: cota.cliente_nome });
    let pdfId;
    try {
      pdfId = await plataforma.enviarPdf(cota.id, pdf);
    } finally {
      fs.rm(pdf, { force: true }, () => {});
    }

    const r = await plataforma.concluirReimpressao(cota.id, {
      status: 'reimpressa',
      protocolo,
      pdf_id: pdfId,
      assembleia_data: linha.assembleia,
      credenciamento: linha.credenciamento,
      modalidade: linha.modalidade,
      percentual: linha.percentual,
    });
    if (!r.ok) registro.warn(`cota ${tag}: a plataforma recusou o comprovante (${r.erro})`);
  } catch (e) {
    const prazo = prazoEncerrado(registro.dialogos.slice(dialogosAntes));
    const conhecido = !!prazo || ['NewconCotaError', 'ErroReimpressao'].includes(e && e.name);
    const arquivo = await newcon.snapshotError(tag);
    if (arquivo) await ctx.enviarScreenshot(cota, arquivo);
    await ctx.concluir(cota, {
      status: 'erro_antes_confirmar',
      erro_tipo: conhecido ? 'conhecido' : 'inesperado',
      erro: prazo || (conhecido ? e.message : primeiraLinha(e)),
      detalhes: {},
    });
  }
}

module.exports = { processarReimpressao };
