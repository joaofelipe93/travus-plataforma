'use strict';

// O Newcon derruba a conexão de quem abre uma página sem cabeçalho Referer (ERR_CONNECTION_RESET,
// sem resposta nenhuma). Estes testes garantem que o worker sempre manda o cabeçalho.
// Nada aqui abre navegador nem fala com o Newcon: a página é um dublê.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');

const { NewconClient, REFERER_PADRAO } = require('../src/newcon');

const silencio = { info() {}, ok() {}, warn() {}, error() {} };

/** Página falsa: anota cada goto e responde ao mínimo que o login usa. */
function paginaFalsa(gotos, urlAtual = 'https://cnp3.consorciocanopus.com.br/WWW/frmMain.aspx') {
  return {
    gotos,
    async goto(url, opcoes = {}) {
      gotos.push({ url, referer: opcoes.referer });
    },
    url: () => urlAtual,
    locator: () => ({
      async fill() {},
      async click() {},
      first: () => ({ async getAttribute() { return 'frmMainMenu.aspx?ID_Modulo=CP&ID_SubGrupo=PC'; } }),
    }),
    async waitForURL() {},
    on() {},
  };
}

function clienteFalso({ referer } = {}) {
  const gotos = [];
  const config = {
    newcon: { url: 'https://cnp3.consorciocanopus.com.br/WWW/frmCorCcCnsLogin.aspx', user: 'u', pass: 'p', referer },
    playwright: { timeoutMs: 1000, headless: true },
  };
  const cliente = new NewconClient({ config, logger: silencio });
  cliente.page = paginaFalsa(gotos);
  return { cliente, gotos };
}

test('login abre a tela do Newcon com Referer do Portal do Parceiro', async () => {
  const { cliente, gotos } = clienteFalso();
  await cliente.login();
  assert.equal(gotos.length, 1);
  assert.equal(gotos[0].url, 'https://cnp3.consorciocanopus.com.br/WWW/frmCorCcCnsLogin.aspx');
  assert.equal(gotos[0].referer, REFERER_PADRAO);
  assert.match(REFERER_PADRAO, /^https:\/\//);
});

test('NEWCON_REFERER configurado troca o cabeçalho', async () => {
  const { cliente, gotos } = clienteFalso({ referer: 'https://outro.exemplo/entrada' });
  await cliente.login();
  assert.equal(gotos[0].referer, 'https://outro.exemplo/entrada');
});

test('Pré-Contemplação sai com a página de origem no Referer', async () => {
  const { cliente, gotos } = clienteFalso();
  cliente.preContemplacaoUrl = 'https://cnp3.consorciocanopus.com.br/WWW/frmMainMenu.aspx?ID_Modulo=CP';
  await cliente._openPreContemplacao();
  assert.equal(gotos.length, 1);
  assert.equal(gotos[0].referer, 'https://cnp3.consorciocanopus.com.br/WWW/frmMain.aspx');
});

test('nenhum goto do worker sai sem Referer', () => {
  // Sem isso, um goto novo escrito no futuro leva ERR_CONNECTION_RESET em produção, e o erro
  // não parece ser disso: a conexão simplesmente cai, sem mensagem do servidor.
  const dir = path.join(__dirname, '../src');
  for (const arquivo of fs.readdirSync(dir).filter((f) => f.endsWith('.js'))) {
    const fonte = fs.readFileSync(path.join(dir, arquivo), 'latin1');
    const chamadas = [...fonte.matchAll(/\.goto\(/g)];
    for (const achada of chamadas) {
      const trecho = fonte.slice(achada.index, achada.index + 400);
      assert.ok(
        /referer\s*:/.test(trecho),
        `${arquivo}: um page.goto sem "referer" — o Newcon derruba a conexão sem esse cabeçalho`,
      );
    }
  }
});
