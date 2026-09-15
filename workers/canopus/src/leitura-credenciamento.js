'use strict';

/**
 * Leituras e navegação no Histórico do Newcon. Nada aqui preenche campos, marca modalidade
 * ou clica em Confirmar. Seletores descobertos em dry-runs em 2026-09-15
 * (ver docs/canopus-newcon.md).
 */

async function texto(page, seletor) {
  const valor = await page.locator(seletor).first().textContent({ timeout: 2000 }).catch(() => null);
  return valor && valor.trim() ? valor.replace(/\s+/g, ' ').trim() : null;
}

/**
 * Dados da assembleia na tela de credenciamento (antes de marcar a modalidade).
 * edtDT_Assembleia é um campo editável: só lemos o valor, nunca preenchemos.
 */
async function lerDadosCredenciamento(page) {
  const data = await page.locator('#ctl00_Conteudo_edtDT_Assembleia').first().inputValue({ timeout: 2000 }).catch(() => null);
  return {
    assembleia_data: data && data.trim() ? data.trim() : null,
    assembleia_numero: await texto(page, '#ctl00_Conteudo_lblNO_Assembleia'),
    percentual_segundo_fixo: await texto(page, '#ctl00_Conteudo_lblVA_Lance_Fixo_2'),
    ultimo_lance: await texto(page, '#ctl00_Conteudo_lblNM_Ocorrencia'),
  };
}

/** Abre o painel do Histórico na tela de credenciamento (#ctl00_Conteudo_btnHistorico). */
async function abrirHistorico(page, timeoutMs = 30000) {
  await page.locator('#ctl00_Conteudo_btnHistorico').click();
  await page.locator('table[id*="grdHistLances"]').first().waitFor({ state: 'visible', timeout: timeoutMs });
}

/**
 * Lê a grade de ofertas (painel do Histórico ou página frmConCpCnsHistoricoOfertaLance.aspx,
 * que usam a mesma grade). A coluna "Usuário" não é guardada.
 */
async function lerGradeHistorico(page, timeoutMs = 30000) {
  const grade = page.locator('table[id*="grdHistLances"]').first();
  await grade.waitFor({ state: 'visible', timeout: timeoutMs });
  const linhas = await grade.locator('tr').evaluateAll((trs) =>
    trs.map((tr) => Array.from(tr.querySelectorAll('th, td')).map((c) => (c.textContent || '').replace(/\s+/g, ' ').trim()))
  );
  if (!linhas.length) return [];
  const [cabecalho, ...dados] = linhas;
  const coluna = (nome) => cabecalho.findIndex((h) => h.toLowerCase() === nome.toLowerCase());
  const indices = {
    protocolo: coluna('Protocolo'),
    assembleia: coluna('Assembleia'),
    credenciamento: coluna('Credenciamento'),
    modalidade: coluna('Modalidade'),
    percentual: coluna('% Lance'),
  };
  if (indices.protocolo < 0 || indices.assembleia < 0) {
    throw new Error(`grade do Histórico com colunas inesperadas: ${cabecalho.join(', ')}`);
  }
  return dados
    .map((l) => Object.fromEntries(Object.entries(indices).map(([campo, i]) => [campo, i >= 0 ? l[i] || null : null])))
    .filter((l) => /^\d+$/.test(l.protocolo || '')); // ignora linha de paginação
}

/** Abre o Histórico e lê a grade. Chamado no dry-run só DEPOIS do screenshot. */
async function lerHistorico(page, timeoutMs = 30000) {
  await abrirHistorico(page, timeoutMs);
  return lerGradeHistorico(page, timeoutMs);
}

/**
 * Reimprime o comprovante de um protocolo: botão de imagem input[id$="srcPrint"] da linha na
 * grade. Descoberto em 2026-09-15: navega na mesma aba para CONCM/frmConCmNewconReports.aspx
 * (o mesmo visualizador Stimulsoft do lance). Depois, use newcon.downloadReportPdf().
 */
async function reimprimirProtocolo(page, protocolo, timeoutMs = 30000) {
  if (!/^\d+$/.test(String(protocolo || ''))) throw new Error(`protocolo inválido: ${protocolo}`);
  const grade = page.locator('table[id*="grdHistLances"]').first();
  const linha = grade.locator('tr').filter({ has: page.locator('td', { hasText: new RegExp(`^\\s*${protocolo}\\s*$`) }) }).first();
  const botao = linha.locator('input[id$="srcPrint"]').first();
  if (!(await botao.count())) throw new Error(`botão de reimprimir não encontrado para o protocolo ${protocolo}`);
  await Promise.all([page.waitForURL(/frmConCmNewconReports\.aspx/i, { timeout: timeoutMs }), botao.click()]);
  await page.locator('iframe[id*="StiWebRelatorio"]').first().waitFor({ state: 'attached', timeout: timeoutMs });
  await page.waitForLoadState('networkidle', { timeout: timeoutMs }).catch(() => {});
}

/** Lances já registrados na assembleia atual, para o aviso da revisão. */
function resumirHistorico(linhas, assembleiaData) {
  const desta = assembleiaData ? linhas.filter((l) => l.assembleia === assembleiaData) : [];
  return {
    historico_lido: true,
    lances_no_historico: linhas.length,
    lances_nesta_assembleia: desta.length,
    lances: desta.slice(0, 10),
  };
}

module.exports = { lerDadosCredenciamento, abrirHistorico, lerGradeHistorico, lerHistorico, reimprimirProtocolo, resumirHistorico };
