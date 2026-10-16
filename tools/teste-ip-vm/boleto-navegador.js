'use strict';

// Abre o Boleto Fácil num Chromium de verdade e consulta /find-cota com um CPF FICTÍCIO.
// Chamado pelo teste-boleto.sh --navegador dentro do container do Playwright.
//
// SÓ LEITURA: não chama generate-bill, não imprime boleto, não toca cota de cliente.
// Para que serve: o `curl` na VM levou 403 da Cloudflare. Se o mesmo pedido, feito de
// dentro de uma página aberta por um navegador real (User-Agent, TLS e cookies de
// navegador, e o desafio em JavaScript resolvido), passar, então o bloqueio é de
// detecção de bot e o worker pode rodar na VM usando o Chromium que a imagem do worker
// do Canopus já tem. Se levar 403 também, o bloqueio é do IP de saída e navegador na
// VM não resolve nada.

const { chromium } = require('playwright');

const SITE = process.env.BOLETO_SITE || 'https://www.consorciocanopus.com.br';
const BASE = process.env.BOLETO_BASE || `${SITE}/extensions/boleto-facil-rolledback/public/index.php`;
const CPF = process.env.CPF_FICTICIO || '11122233396';
// O Playwright se anuncia como "HeadlessChrome" e marca navigator.webdriver: a Cloudflare
// barra isso. BOLETO_UA troca o User-Agent para ver se é só o cabeçalho que incomoda.
const UA = process.env.BOLETO_UA || '';

async function main() {
  const browser = await chromium.launch({
    headless: true,
    args: ['--disable-blink-features=AutomationControlled'],
  });
  const page = await browser.newPage({
    viewport: { width: 1280, height: 900 },
    ...(UA ? { userAgent: UA } : {}),
    locale: 'pt-BR',
  });
  if (UA) console.log(`  · User-Agent trocado para: ${UA.slice(0, 48)}…`);
  page.setDefaultTimeout(45000);

  try {
    const resp = await page.goto(`${SITE}/boletofacil/`, { waitUntil: 'domcontentloaded' });
    const status = resp ? resp.status() : 0;
    console.log(`  · página do Boleto Fácil no Chromium: HTTP ${status}`);
    if (status !== 200) {
      console.log('  \x1b[31mx\x1b[0m A própria página foi barrada: bloqueio não é só da rota de dados.');
      process.exitCode = 1;
      return;
    }

    // O mesmo pedido que o front faz, de dentro da página (mesma origem).
    const r = await page.evaluate(async (url) => {
      const res = await fetch(url, { headers: { 'X-Requested-With': 'XMLHttpRequest' } });
      return { status: res.status, corpo: (await res.text()).slice(0, 200) };
    }, `${BASE}/find-cota/${CPF}`);

    console.log(`  · find-cota pelo Chromium: HTTP ${r.status}`);
    if (r.status === 200 && r.corpo.includes('"cotas"')) {
      console.log(`  \x1b[32m✓\x1b[0m O NAVEGADOR PASSA deste IP: ${r.corpo.trim()}`);
      console.log('    → o bloqueio é de detecção de bot. O worker pode rodar na VM com Chromium.');
    } else {
      console.log(`  \x1b[31mx\x1b[0m O navegador também foi barrado: ${r.corpo.trim()}`);
      console.log('    → bloqueio pelo IP de saída. Chromium na VM não resolve; a chamada precisa sair por outro IP.');
      process.exitCode = 1;
    }
  } finally {
    await browser.close();
  }
}

main().catch((e) => {
  console.error(`  x erro no teste pelo navegador: ${e.message}`);
  process.exit(1);
});
