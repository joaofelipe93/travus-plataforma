'use strict';

// Faz SÓ o login no Newcon a partir desta máquina e sai. Não abre nenhuma tela de lance.
// Chamado pelo teste-ip.sh dentro do container do Playwright (lê NEWCON_URL/USER/PASS do ambiente).

const path = require('path');
const { chromium } = require('playwright');

const { NEWCON_URL, NEWCON_USER, NEWCON_PASS } = process.env;
const SHOT = path.join(__dirname, 'login-resultado.png');
const hideKey = (u) => u.replace(/applicationKey=[^&]+/, 'applicationKey=…');

async function main() {
  if (!NEWCON_URL || !NEWCON_USER || !NEWCON_PASS) throw new Error('Defina NEWCON_URL, NEWCON_USER e NEWCON_PASS');

  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
  page.setDefaultTimeout(30000);
  page.on('dialog', async (d) => {
    console.log(`  · aviso do Newcon (${d.type()}): ${d.message()}`);
    await d.dismiss();
  });

  try {
    const resp = await page.goto(NEWCON_URL, { waitUntil: 'domcontentloaded' });
    console.log(`  · página de login: HTTP ${resp ? resp.status() : '?'}`);
    await page.locator('#edtUsuario').fill(NEWCON_USER);
    await page.locator('#edtSenha').fill(NEWCON_PASS);
    await page.locator('#btnLogin').click();
    await page.waitForURL(/frmMain\.aspx/i);
    await page.waitForLoadState('networkidle').catch(() => {});
    await page.screenshot({ path: SHOT, fullPage: true });
    console.log('  \x1b[32m✓\x1b[0m LOGIN OK: chegou na tela inicial do Newcon');

    const sair = page.locator('#ctl00_Conteudo_lkbSair');
    if (await sair.count()) {
      await sair.click().catch(() => {});
      await page.waitForLoadState('domcontentloaded').catch(() => {});
      console.log('  · logout feito');
    }
    return 0;
  } catch (e) {
    // Mensagens de validação do Newcon aparecem em vermelho (ex.: usuário ou senha inválidos).
    const msg = await page
      .evaluate(() =>
        Array.from(document.querySelectorAll('body *'))
          .filter((el) => !el.children.length && el.getBoundingClientRect().width)
          .filter((el) => {
            const [r, g, b] = (getComputedStyle(el).color.match(/\d+/g) || []).map(Number);
            return r > 150 && g < 90 && b < 90;
          })
          .map((el) => el.textContent.trim())
          .filter(Boolean)
          .join(' ')
      )
      .catch(() => '');
    console.log(`  \x1b[31mx\x1b[0m LOGIN FALHOU: ${e.message.split('\n')[0]}`);
    if (msg) console.log(`    mensagem na tela: ${msg}`);
    console.log(`    URL: ${hideKey(page.url())}`);
    await page.screenshot({ path: SHOT, fullPage: true }).catch(() => {});
    return 1;
  } finally {
    console.log(`  · screenshot: ${path.basename(SHOT)}`);
    await browser.close();
  }
}

main()
  .then((code) => process.exit(code))
  .catch((e) => {
    console.log(`  x ERRO: ${e.message}`);
    process.exit(1);
  });
