'use strict';

/**
 * Modalidade do lance: a plataforma marca a modalidade **cadastrada na cota**, não mais o
 * "2º Fixo" sempre (decisão do usuário em 15/10/2026: cota cadastrada como fixo dá Fixo mesmo).
 *
 * Radios da tela de credenciamento (docs/canopus-newcon.md): rgLance_0 (LL, Livre),
 * rgLance_1 (LF, Fixo), rgLance_2 (LS, 2º Fixo), rgLance_3 (LM, Limitado),
 * rgLance_4 (LI, Fidelidade). O percentual de cada uma tem rótulo próprio.
 *
 * Só **Fixo** e **2º Fixo** são suportados. Para Livre, Limitado e Fidelidade não se sabe que
 * valor o Newcon espera (lance livre precisa de um valor que ninguém definiu), e chutar num
 * lance real não tem volta: cota cadastrada assim dá erro conhecido até o usuário definir a
 * regra. Antes desta mudança elas levavam 2º Fixo em silêncio, o que era pior.
 *
 * Este módulo fica fora do newcon.js de propósito: aquele arquivo é o código validado em
 * produção e não se altera (CLAUDE.md). Ele é injetado no worker como o módulo de leitura.
 */

const { NewconCotaError } = require('./newcon');

// rotulo: como o Newcon chama na tela. lance: como o Histórico nomeia a modalidade do lance.
const MODALIDADES = {
  segundo_fixo: {
    codigo: 'segundo_fixo',
    rotulo: '2º Fixo',
    radio: '#ctl00_Conteudo_rgLance_2',
    percentual: '#ctl00_Conteudo_lblVA_Lance_Fixo_2',
  },
  fixo: {
    codigo: 'fixo',
    rotulo: 'Fixo',
    radio: '#ctl00_Conteudo_rgLance_1',
    percentual: '#ctl00_Conteudo_lblVA_Lance_Fixo',
  },
};

const NAO_SUPORTADAS = { livre: 'Livre', limitado: 'Limitado', fidelidade: 'Fidelidade' };

/**
 * daCota devolve a modalidade a marcar, a partir do que está no cadastro. Não toca na página:
 * cota em modalidade não suportada já falha aqui, antes de abrir qualquer coisa no Newcon.
 */
function daCota(cota) {
  const cadastrada = String((cota && cota.modalidade) || '').trim();
  if (MODALIDADES[cadastrada]) return MODALIDADES[cadastrada];
  if (NAO_SUPORTADAS[cadastrada]) {
    throw new NewconCotaError(
      `a cota está cadastrada na modalidade ${NAO_SUPORTADAS[cadastrada]}, que a plataforma ainda não dá: ` +
        'só Fixo e 2º Fixo. Ajuste o cadastro da cota ou registre esse lance no Newcon.'
    );
  }
  throw new NewconCotaError(`modalidade desconhecida no cadastro da cota: ${cadastrada || '(vazia)'}`);
}

/**
 * marcar marca o radio da modalidade. ASP.NET usa AutoPostBack, então esperamos a página se
 * estabilizar antes de seguir — igual ao selectSegundoFixo do newcon.js, de onde isto saiu.
 * Recebe o NewconClient (page, logger e timeout vêm dele).
 */
async function marcar(newcon, m) {
  const { page, logger } = newcon;
  logger.info(`lance: marcando "${m.rotulo}"`);
  const radio = page.locator(m.radio);
  await radio.waitFor({ state: 'attached' });
  if (await radio.isDisabled()) {
    const disponiveis = await page
      .locator('input[name="ctl00$Conteudo$rgLance"]:not([disabled])')
      .evaluateAll((els) =>
        els.map((el) => ((document.querySelector(`label[for="${el.id}"]`) || {}).textContent || el.value).trim())
      );
    throw new NewconCotaError(
      `"${m.rotulo}" está desabilitado para esta cota no Newcon` +
        (disponiveis.length ? `. Modalidades disponíveis: ${disponiveis.join(', ')}` : '')
    );
  }
  await radio.check();
  await page.waitForLoadState('networkidle', { timeout: newcon.config.playwright.timeoutMs });
  logger.ok(`lance: "${m.rotulo}" selecionado`);
  return m;
}

module.exports = { MODALIDADES, NAO_SUPORTADAS, daCota, marcar };
