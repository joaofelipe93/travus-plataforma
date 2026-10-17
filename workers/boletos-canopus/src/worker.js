'use strict';

/**
 * Worker de boletos da Canopus (issue #48).
 *
 * Laço: pega uma rodada na fila da API → para cada cliente, consulta o Boleto Fácil pelo CPF,
 * casa as cotas com o cadastro, grava as parcelas em aberto e (na rodada de envio) guarda o PDF
 * → finaliza. Quem manda a mensagem no WhatsApp é a etapa seguinte; aqui nada é enviado.
 *
 * Três coisas que não são negociáveis neste arquivo:
 *   1. **Ritmo.** O Boleto Fácil responde 429. Há pausa entre clientes e entre cotas, e recuo
 *      no 429 (boleto-facil.js). Não troque por paralelismo: a rodada inteira pode ser barrada.
 *   2. **O PDF é buscado logo depois do generate-bill**, no mesmo cookie: o transactionId muda
 *      a cada chamada e o link de impressão não é durável.
 *   3. **Erro de um cliente não derruba a rodada.** Cada cliente é concluído com o que deu, e a
 *      rodada segue — como no worker do Canopus.
 */

const { carregar } = require('./config');
const { criarCliente, esperar } = require('./boleto-facil');
const { casarCotas } = require('./cotas');
const { Plataforma } = require('./plataforma');
const { Registro } = require('./registro');

const primeiraLinha = (e) => String((e && e.message) || e).split('\n')[0];

/** Processa um cliente. NUNCA lança: devolve o resultado que vai para a API. */
async function processarCliente({ boleto, plataforma, registro, cliente, tipo, pausaCotaMs = 0 }) {
  const resultado = { status: 'sem_boleto', cotas_no_site: 0, cotas_casadas: 0 };
  try {
    const doSite = await boleto.buscarCotas(cliente.documento);
    resultado.cotas_no_site = doSite.length;

    const { casadas, soNoSite, soNoCadastro } = casarCotas(doSite, cliente.cotas);
    resultado.cotas_casadas = casadas.length;

    if (doSite.length === 0) {
      // Documento sem cota na Canopus: quase sempre é CPF errado no cadastro (o site não dá
      // erro, devolve lista vazia) — por isso vira aviso, não silêncio.
      await registro.aviso(`${cliente.nome}: o Boleto Fácil não encontrou cota para o documento cadastrado.`);
      return resultado;
    }
    for (const { site, ambiguo } of soNoSite) {
      await registro.aviso(
        ambiguo
          ? `${cliente.nome}: a cota ${site.tag} do site casa com mais de uma cota cadastrada (versões diferentes): não dá para escolher.`
          : `${cliente.nome}: a cota ${site.tag} existe na Canopus e não está no cadastro: nenhum boleto dela foi buscado.`,
      );
    }
    for (const q of soNoCadastro) {
      await registro.aviso(`${cliente.nome}: a cota ${q.grupo}-${q.cota}-${q.versao} está no cadastro e não apareceu no site.`);
    }
    if (casadas.length === 0) return resultado;

    let total = 0;
    for (let i = 0; i < casadas.length; i++) {
      const { site, cadastro } = casadas[i];
      if (i > 0 && pausaCotaMs > 0) await esperar(pausaCotaMs);

      const { parcelas, descartadas, semValores } = await boleto.buscarParcelas(site);
      for (const d of descartadas) {
        // A linha "DIF" com valor zero é o caso normal: não assusta ninguém com erro.
        if (d.motivo !== 'valor zero') {
          await registro.aviso(`${cliente.nome}, cota ${site.tag}: parcela ${d.parcela} ignorada (${d.motivo}).`);
        }
      }
      if (semValores || parcelas.length === 0) {
        await registro.info(`${cliente.nome}, cota ${site.tag}: nada em aberto.`);
        continue;
      }

      const ids = await plataforma.registrarParcelas(cliente.id, parcelas.map((p) => ({
        cota_id: cadastro.id,
        parcela: p.parcela,
        vencimento: p.vencimento,
        valor_centavos: p.valorCentavos,
        tipo: p.tipo,
        charge_id: p.chargeId,
        transaction_id: p.transactionId,
      })));
      total += parcelas.length;
      await registro.ok(`${cliente.nome}, cota ${site.tag}: ${parcelas.length} parcela(s) em aberto.`);

      // Na cobrança não se baixa PDF: a mensagem do dia 14 é só o lembrete, e cada impressão é
      // um pedido a mais numa rota que responde 429.
      if (tipo !== 'envio') continue;
      for (let j = 0; j < parcelas.length; j++) {
        if (!ids[j]) continue;
        try {
          const pdf = await boleto.baixarPdf(parcelas[j]);
          await plataforma.enviarPdf(ids[j], pdf);
        } catch (e) {
          // Parcela sem PDF continua registrada: a cobrança precisa dela de qualquer jeito.
          await registro.aviso(`${cliente.nome}, cota ${site.tag}: parcela ${parcelas[j].parcela} sem PDF (${primeiraLinha(e)}).`);
        }
      }
    }
    resultado.status = total > 0 ? 'consultado' : 'sem_boleto';
    return resultado;
  } catch (e) {
    // Erro do site traz a classificação em `conhecido` (ErroBoletoFacil e ErroLimiteDeTaxa).
    // Qualquer outro — bug nosso, rede, API — é inesperado, para alguém olhar.
    const conhecido = typeof (e && e.conhecido) === 'boolean' ? e.conhecido : false;
    return {
      ...resultado,
      status: 'erro',
      erro_tipo: conhecido ? 'conhecido' : 'inesperado',
      erro: primeiraLinha(e),
    };
  }
}

/**
 * Percorre uma rodada. `criarBoleto` existe para os testes injetarem um dublê do site — é
 * injeção de dependência de propósito: trocar o módulo por fora não funcionaria, porque este
 * arquivo desestrutura `criarCliente` no require.
 */
async function processarRodada({ config, plataforma, rodada, parar, criarBoleto = criarCliente }) {
  const registro = new Registro({ plataforma, rodadaId: rodada.id });
  const boleto = criarBoleto(config.site, { registro });

  let cancelada = false;
  const renovar = setInterval(async () => {
    try {
      const r = await plataforma.renovar(rodada.id);
      if (!r.ok) {
        cancelada = true;
        return;
      }
      if (r.cancelamentoSolicitado) cancelada = true;
    } catch {
      // Falha de rede na renovação: a trava vence e a API devolve a rodada à fila.
    }
  }, config.worker.renovarTravaMs);

  let erroFatal = null;
  try {
    await registro.info(`Modo ${boleto.modo}: abrindo o Boleto Fácil.`);
    await boleto.abrir();

    for (let n = 1; ; n++) {
      if (cancelada) {
        await registro.aviso('Cancelamento pedido: a rodada para depois deste cliente.');
        break;
      }
      if (parar.pedido) {
        await registro.aviso('Worker encerrando: a rodada volta para a fila.');
        break;
      }
      const cliente = await plataforma.proximoCliente(rodada.id);
      if (!cliente) break;
      if (n > 1) await esperar(config.site.pausaClienteMs);

      registro.comCliente(cliente.id);
      const resultado = await processarCliente({
        boleto, plataforma, registro, cliente, tipo: rodada.tipo, pausaCotaMs: config.site.pausaCotaMs,
      });
      const fim = await plataforma.concluirCliente(cliente.id, resultado);
      registro.comCliente(null);
      await registro.descarregar();
      if (!fim.ok && fim.parar) {
        cancelada = true;
        break;
      }
    }
  } catch (e) {
    erroFatal = primeiraLinha(e);
    await registro.erro(`A rodada parou: ${erroFatal}`);
  } finally {
    clearInterval(renovar);
    await boleto.fechar().catch(() => {});
    await registro.descarregar();
  }

  if (parar.pedido && !erroFatal) {
    await plataforma.liberar(rodada.id).catch(() => {});
    return;
  }
  await plataforma.finalizar(rodada.id, erroFatal).catch((e) => {
    process.stdout.write(JSON.stringify({ nivel: 'erro', mensagem: `falha ao finalizar a rodada: ${primeiraLinha(e)}` }) + '\n');
  });
}

async function main() {
  const config = carregar();
  const plataforma = new Plataforma({
    url: config.api.url,
    token: config.api.token,
    worker: config.worker.nome,
    timeoutMs: config.api.timeoutMs,
  });
  const parar = { pedido: false };
  for (const sinal of ['SIGTERM', 'SIGINT']) {
    process.on(sinal, () => {
      parar.pedido = true;
      process.stdout.write(JSON.stringify({ nivel: 'aviso', mensagem: `${sinal} recebido: terminando o cliente atual` }) + '\n');
    });
  }

  process.stdout.write(
    JSON.stringify({
      nivel: 'info',
      mensagem: 'worker de boletos no ar',
      worker: config.worker.nome,
      modo: config.site.modo,
      versao: process.env.VERSAO || 'dev',
      commit: process.env.COMMIT || 'desconhecido',
    }) + '\n',
  );

  while (!parar.pedido) {
    let rodada = null;
    try {
      rodada = await plataforma.proximaRodada();
    } catch (e) {
      process.stdout.write(JSON.stringify({ nivel: 'aviso', mensagem: `API indisponível: ${primeiraLinha(e)}` }) + '\n');
    }
    if (!rodada) {
      await esperar(config.worker.intervaloFilaMs);
      continue;
    }
    await processarRodada({ config, plataforma, rodada, parar });
  }
  process.stdout.write(JSON.stringify({ nivel: 'info', mensagem: 'worker de boletos encerrado' }) + '\n');
}

if (require.main === module) {
  main().catch((e) => {
    process.stderr.write(`${primeiraLinha(e)}\n`);
    process.exit(1);
  });
}

module.exports = { processarCliente, processarRodada };
