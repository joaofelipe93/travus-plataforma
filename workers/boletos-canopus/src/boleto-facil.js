'use strict';

/**
 * Cliente do Boleto Fácil da Canopus. Faz as três chamadas da vez, na ordem, com o mesmo
 * cookie (ver docs/canopus-boleto-facil.md):
 *   find-cota/{documento}  → cotas daquele CPF/CNPJ
 *   generate-bill/{g}/{c}/{idCota} → parcelas em aberto (e um transactionId NOVO a cada chamada)
 *   print-bill | print-overdue-bill → o PDF
 *
 * Dois modos, porque a Cloudflare do site não recusa o que se imagina (medido em 16/10/2026):
 *
 *   'navegador' (padrão) — Chromium de verdade com User-Agent de Chrome. A página é aberta uma
 *     vez e os três pedidos saem por `fetch` DENTRO dela: pilha de rede e cookies do navegador,
 *     mas resposta em JSON e PDF em bytes, sem depender de seletor de tela nem de download.
 *     Essa combinação foi testada de ponta a ponta, inclusive o PDF (56 KB, `%PDF-`…`%%EOF`).
 *   'http' — fetch do Node com cabeçalho de navegador. Mais leve; é o que fica se a VM aceitar.
 *     Da máquina do escritório funciona até com User-Agent de curl; de uma VM, o curl levou 403.
 *
 * O que NÃO fazer aqui: usar o Playwright com o User-Agent de fábrica ("HeadlessChrome"), que
 * leva 403 até de um IP liberado; e clicar na tela — o site é três GETs, seletor só acrescenta
 * motivo para quebrar.
 */

const { ErroBoletoFacil, lerCotas, lerParcelas, pdfValido } = require('./parcelas');

class ErroLimiteDeTaxa extends ErroBoletoFacil {
  constructor(mensagem) {
    super(mensagem);
    this.name = 'ErroLimiteDeTaxa';
  }
}

const esperar = (ms) => new Promise((r) => setTimeout(r, ms));

/** Cabeçalhos que fazem o pedido parecer o que o próprio site faz. */
function cabecalhos(config, { json }) {
  return {
    'User-Agent': config.userAgent,
    'Accept-Language': 'pt-BR,pt;q=0.9',
    Referer: `${config.site}/boletofacil/`,
    Accept: json ? 'application/json, text/javascript, */*; q=0.01' : 'application/pdf,*/*',
    ...(json ? { 'X-Requested-With': 'XMLHttpRequest' } : {}),
  };
}

/** Driver HTTP: fetch do Node, com um porta-cookies simples (o site guarda estado na sessão). */
function driverHttp(config) {
  const cookies = new Map();

  const guardarCookies = (resposta) => {
    const brutos = typeof resposta.headers.getSetCookie === 'function' ? resposta.headers.getSetCookie() : [];
    for (const linha of brutos) {
      const [par] = String(linha).split(';');
      const i = par.indexOf('=');
      if (i > 0) cookies.set(par.slice(0, i).trim(), par.slice(i + 1).trim());
    }
  };

  const pedir = async (url, { binario = false } = {}) => {
    const controle = new AbortController();
    const prazo = setTimeout(() => controle.abort(), config.timeoutMs);
    try {
      const cabecalho = cabecalhos(config, { json: !binario });
      if (cookies.size > 0) {
        cabecalho.Cookie = [...cookies].map(([k, v]) => `${k}=${v}`).join('; ');
      }
      const r = await fetch(url, { headers: cabecalho, signal: controle.signal, redirect: 'follow' });
      guardarCookies(r);
      if (binario) {
        return { status: r.status, bytes: Buffer.from(await r.arrayBuffer()) };
      }
      return { status: r.status, texto: await r.text() };
    } finally {
      clearTimeout(prazo);
    }
  };

  return { modo: 'http', abrir: async () => {}, fechar: async () => {}, pedir };
}

/** Driver navegador: Chromium com User-Agent de Chrome; os pedidos saem de dentro da página. */
function driverNavegador(config, { playwright } = {}) {
  const { chromium } = playwright || require('playwright');
  let browser;
  let contexto;
  let pagina;

  const abrir = async () => {
    browser = await chromium.launch({
      headless: config.headless,
      args: ['--disable-blink-features=AutomationControlled'],
    });
    contexto = await browser.newContext({
      userAgent: config.userAgent,
      locale: 'pt-BR',
      viewport: { width: 1280, height: 900 },
    });
    contexto.setDefaultTimeout(config.timeoutMs);
    pagina = await contexto.newPage();
    const r = await pagina.goto(`${config.site}/boletofacil/`, { waitUntil: 'domcontentloaded' });
    const status = r ? r.status() : 0;
    if (status !== 200) {
      throw new ErroBoletoFacil(
        `a página do Boleto Fácil respondeu HTTP ${status} para o navegador: pode ser bloqueio por IP de saída (ver docs/canopus-boleto-facil.md)`,
        { conhecido: false },
      );
    }
  };

  const fechar = async () => {
    if (contexto) await contexto.close().catch(() => {});
    if (browser) await browser.close().catch(() => {});
    browser = contexto = pagina = undefined;
  };

  // O fetch roda na página (mesma origem), então leva os cookies e o User-Agent dela.
  const pedir = async (url, { binario = false } = {}) => {
    if (!pagina) throw new ErroBoletoFacil('o navegador não está aberto', { conhecido: false });
    const extra = cabecalhos(config, { json: !binario });
    // User-Agent e Referer já vêm da página; mandar nos dois lugares só atrapalha.
    delete extra['User-Agent'];
    delete extra.Referer;
    const r = await pagina.evaluate(
      async ({ url, binario, extra }) => {
        const res = await fetch(url, { headers: extra, credentials: 'include' });
        if (!binario) return { status: res.status, texto: await res.text() };
        const buf = new Uint8Array(await res.arrayBuffer());
        // base64 em pedaços: o PDF tem dezenas de KB e String.fromCharCode(...) estoura a pilha.
        let bruto = '';
        for (let i = 0; i < buf.length; i += 8192) {
          bruto += String.fromCharCode.apply(null, buf.subarray(i, i + 8192));
        }
        return { status: res.status, base64: btoa(bruto) };
      },
      { url, binario, extra },
    );
    if (binario) return { status: r.status, bytes: Buffer.from(r.base64 || '', 'base64') };
    return { status: r.status, texto: r.texto };
  };

  return { modo: 'navegador', abrir, fechar, pedir };
}

/**
 * criarCliente devolve o cliente pronto, com as retentativas de 429 embutidas.
 * `registro` (opcional) recebe avisos: registro.aviso(mensagem).
 */
function criarCliente(config, { playwright, registro } = {}) {
  const driver = config.modo === 'http' ? driverHttp(config) : driverNavegador(config, { playwright });
  const avisar = (mensagem) => registro && registro.aviso && registro.aviso(mensagem);

  // O 429 do site não é "tente de novo agora": é recuo. Espera dobrando a cada tentativa.
  const pedirComRecuo = async (url, opcoes = {}) => {
    let espera = config.espera429Ms;
    for (let tentativa = 1; ; tentativa++) {
      const r = await driver.pedir(url, opcoes);
      if (r.status !== 429) return r;
      if (tentativa >= config.tentativas429) {
        throw new ErroLimiteDeTaxa(
          `o site respondeu 429 (limite de taxa) ${tentativa} vezes seguidas: a rodada continua depois`,
        );
      }
      avisar(`429 do Boleto Fácil: esperando ${Math.round(espera / 1000)}s antes de tentar de novo`);
      await esperar(espera);
      espera *= 2;
    }
  };

  const exigir200 = (r, oque) => {
    if (r.status === 403) {
      throw new ErroBoletoFacil(
        `o Boleto Fácil recusou o acesso (403) ${oque}: a Cloudflare barrou este cliente ou este IP (ver docs/canopus-boleto-facil.md)`,
        { conhecido: false },
      );
    }
    if (r.status !== 200) throw new ErroBoletoFacil(`o Boleto Fácil respondeu HTTP ${r.status} ${oque}`);
    return r;
  };

  return {
    modo: driver.modo,
    abrir: () => driver.abrir(),
    fechar: () => driver.fechar(),

    /** Cotas de um CPF/CNPJ (só dígitos). Lista vazia = documento sem cota na Canopus. */
    async buscarCotas(documento) {
      const doc = String(documento || '').replace(/\D/g, '');
      if (doc.length !== 11 && doc.length !== 14) {
        throw new ErroBoletoFacil(`documento precisa ter 11 ou 14 dígitos (tem ${doc.length})`);
      }
      const r = exigir200(await pedirComRecuo(`${config.base}/find-cota/${doc}`), 'na consulta de cotas');
      return lerCotas(r.texto);
    },

    /** Parcelas em aberto de uma cota. Erro do site aqui costuma ser "nada em aberto". */
    async buscarParcelas({ grupo, cota, idCota }) {
      const r = await pedirComRecuo(`${config.base}/generate-bill/${grupo}/${cota}/${idCota}`);
      if (r.status === 404 || r.status === 500) {
        // O front mostra "Não foram identificados valores válidos para geração de boletos".
        return { parcelas: [], descartadas: [], semValores: true };
      }
      exigir200(r, 'ao gerar o boleto');
      return { ...lerParcelas(r.texto, { base: config.base }), semValores: false };
    },

    /** O PDF da parcela, buscado logo depois do generate-bill (o link não é durável). */
    async baixarPdf(parcela) {
      if (!parcela.urlPdf) throw new ErroBoletoFacil('a parcela não trouxe os dados da impressão');
      const r = exigir200(await pedirComRecuo(parcela.urlPdf, { binario: true }), 'ao imprimir o boleto');
      if (!pdfValido(r.bytes)) {
        throw new ErroBoletoFacil(`a impressão não devolveu um PDF (${r.bytes.length} bytes)`);
      }
      return r.bytes;
    },
  };
}

module.exports = { criarCliente, ErroLimiteDeTaxa, ErroBoletoFacil, esperar };
