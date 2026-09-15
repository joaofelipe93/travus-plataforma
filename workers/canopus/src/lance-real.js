'use strict';

/**
 * LANCE REAL (Etapa 3). Só é carregado pelo worker quando LANCE_REAL_HABILITADO=true e a
 * execução é do tipo "real" (aprovada por um admin a partir de um dry-run revisado).
 *
 * Regra de ouro: a plataforma grava "confirmacao_iniciada" ANTES do clique em Confirmar.
 * Se não conseguir gravar (recusa ou falha de rede), NÃO clica. Depois do clique, nenhum
 * erro volta a cota para um estado que permita repetir: vira erro_apos_confirmar.
 */

const fs = require('fs');
const { resumirHistorico } = require('./leitura-credenciamento');
const { prazoEncerrado, extrairProtocolo, parcelasEmAtraso, jaCredenciado } = require('./avisos-newcon');

const primeiraLinha = (e) => String((e && e.message) || e).split('\n')[0];
const tagCota = (c) => `${c.grupo}-${c.cota}-${c.versao}`;
const esperar = (ms) => new Promise((r) => setTimeout(r, ms));

/** Problema antes do clique: nada foi confirmado. */
class ErroAntesDeConfirmar extends Error {
  constructor(mensagem, conhecido = true) {
    super(mensagem);
    this.name = 'ErroAntesDeConfirmar';
    this.conhecido = conhecido;
  }
}

async function conferirAssembleia(leitura, newcon, cota) {
  const dados = await leitura.lerDadosCredenciamento(newcon.page);
  if (!dados.assembleia_data || dados.assembleia_data !== cota.assembleia_aprovada) {
    throw new ErroAntesDeConfirmar(
      `a assembleia na tela (${dados.assembleia_data || 'não lida'}) não é a aprovada na revisão (${cota.assembleia_aprovada}): nada foi confirmado`
    );
  }
  return dados;
}

/**
 * @param {object} ctx { newcon, registro, plataforma, cota, voltarAoFiltro, config, leitura,
 *                       enviarScreenshot(cota, arquivo), concluir(cota, conclusao) }
 */
async function processarCotaReal(ctx) {
  const { newcon, registro, plataforma, cota, config, leitura } = ctx;
  const tag = tagCota(cota);
  const detalhes = {};
  const dialogosAntes = registro.dialogos.length;
  let clicou = false;

  try {
    // ----- Antes do clique: qualquer falha aqui é segura (nada foi confirmado) -----
    if (ctx.voltarAoFiltro) await newcon.backToFilter();
    await newcon.searchCota(cota);
    Object.assign(detalhes, await conferirAssembleia(leitura, newcon, cota));

    let historico;
    try {
      historico = await leitura.lerHistorico(newcon.page, config.playwright.timeoutMs);
    } catch (e) {
      throw new ErroAntesDeConfirmar(`não foi possível conferir o Histórico antes de confirmar (${primeiraLinha(e)}): nada foi confirmado`, false);
    }
    Object.assign(detalhes, resumirHistorico(historico, detalhes.assembleia_data));
    if (detalhes.lances_nesta_assembleia > 0) {
      const protocolos = detalhes.lances.map((l) => l.protocolo).join(', ');
      if (!cota.permitir_lance_existente) {
        throw new ErroAntesDeConfirmar(
          `já tem ${detalhes.lances_nesta_assembleia} lance(s) na assembleia de ${detalhes.assembleia_data} (${protocolos}) e não foi autorizada a registrar outro: pulada`
        );
      }
      registro.warn(`cota ${tag}: já tem lance nesta assembleia (${protocolos}); registrando outro porque foi autorizado na revisão`);
    }

    // Volta ao filtro e abre a cota de novo: só passos validados até o Confirmar
    // (não confirmamos com o painel do Histórico aberto).
    await newcon.backToFilter();
    await newcon.searchCota(cota);
    await conferirAssembleia(leitura, newcon, cota);
    await newcon.selectSegundoFixo();
    const screenshot = await newcon.captureBeforeConfirm(cota);
    await ctx.enviarScreenshot(cota, screenshot);

    // ----- Trava no banco ANTES do clique -----
    let marca;
    try {
      marca = await plataforma.marcarConfirmacaoIniciada(cota.id, { assembleia_data: detalhes.assembleia_data });
    } catch (e) {
      throw new ErroAntesDeConfirmar(`a plataforma não registrou o início da confirmação (${primeiraLinha(e)}): não cliquei em Confirmar`, false);
    }
    if (!marca.ok) {
      throw new ErroAntesDeConfirmar(`a plataforma não autorizou a confirmação (${marca.erro}): não cliquei em Confirmar`);
    }

    registro.warn(`cota ${tag}: clicando em Confirmar (lance real)`);
    clicou = true;
    await newcon.confirmAndWaitReport();
  } catch (e) {
    const dialogos = registro.dialogos.slice(dialogosAntes);
    const arquivo = await newcon.snapshotError(tag);
    if (arquivo) await ctx.enviarScreenshot(cota, arquivo);

    if (!clicou) {
      const prazo = prazoEncerrado(dialogos);
      const conhecido = !!prazo || e.name === 'NewconCotaError' || (e.name === 'ErroAntesDeConfirmar' && e.conhecido);
      const erro = prazo || (conhecido || e.name === 'ErroAntesDeConfirmar' ? e.message : primeiraLinha(e));
      await ctx.concluir(cota, { status: 'erro_antes_confirmar', erro_tipo: conhecido ? 'conhecido' : 'inesperado', erro, detalhes });
      return;
    }

    const lido = extrairProtocolo(dialogos);
    await concluirConfirmacao(ctx, {
      status: 'erro_apos_confirmar',
      erro:
        `erro depois de clicar em Confirmar (${primeiraLinha(e)})` +
        (lido ? `; o Newcon mostrou o protocolo ${lido.protocolo}` : '') +
        ': o lance pode ter sido registrado, confira no Histórico',
      protocolo: lido ? lido.protocolo : null,
      texto_protocolo: lido ? lido.texto : null,
      detalhes,
    });
    return;
  }

  // ----- Depois do clique, sem erro -----
  const dialogos = registro.dialogos.slice(dialogosAntes);
  const lido = extrairProtocolo(dialogos);
  if (!lido) {
    await concluirConfirmacao(ctx, {
      status: 'erro_apos_confirmar',
      erro: 'o Newcon não mostrou o protocolo depois de Confirmar: o lance pode ter sido registrado, confira no Histórico',
      detalhes,
    });
    return;
  }

  let pdfId = null;
  try {
    const pdf = await newcon.downloadReportPdf({ ...cota, nome: cota.cliente_nome });
    try {
      pdfId = await plataforma.enviarPdf(cota.id, pdf);
    } finally {
      fs.rm(pdf, { force: true }, () => {});
    }
  } catch (e) {
    registro.warn(`cota ${tag}: comprovante (PDF) não obtido (${primeiraLinha(e)}); dá para reimprimir pelo Histórico`);
  }

  await concluirConfirmacao(ctx, {
    status: 'confirmada',
    protocolo: lido.protocolo,
    texto_protocolo: lido.texto,
    parcelas_em_atraso: parcelasEmAtraso(dialogos),
    lance_existente: jaCredenciado(dialogos),
    pdf_id: pdfId,
    assembleia_numero: detalhes.assembleia_numero || null,
    percentual: detalhes.percentual_segundo_fixo || null,
    detalhes,
  });
}

/**
 * O lance já foi (ou pode ter sido) registrado: insiste em informar a plataforma. Se não
 * conseguir, a finalização marca a cota como erro_apos_confirmar (conferência manual).
 */
async function concluirConfirmacao(ctx, conclusao) {
  const { plataforma, registro, cota } = ctx;
  const pausas = ctx.pausasMs || [2000, 5000, 15000];
  for (let tentativa = 0; ; tentativa++) {
    try {
      const r = await plataforma.concluirConfirmacao(cota.id, conclusao);
      if (!r.ok) registro.error(`cota ${tagCota(cota)}: a plataforma recusou o resultado da confirmação (${r.erro})`);
      return;
    } catch (e) {
      if (tentativa >= pausas.length) {
        registro.error(
          `cota ${tagCota(cota)}: resultado da confirmação não chegou à plataforma (${primeiraLinha(e)})` +
            (conclusao.protocolo ? `; protocolo ${conclusao.protocolo}` : '')
        );
        return;
      }
      await esperar(pausas[tentativa]);
    }
  }
}

module.exports = { processarCotaReal, ErroAntesDeConfirmar };
