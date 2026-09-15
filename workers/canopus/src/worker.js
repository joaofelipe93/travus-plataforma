#!/usr/bin/env node
'use strict';

/**
 * Worker Canopus da plataforma: pega execuções da fila (API interna), roda no Newcon e
 * informa o andamento de cada cota.
 *
 * ETAPA 2: SÓ DRY-RUN. Este arquivo não tem nenhum caminho que clique em Confirmar
 * (confirmAndWaitReport/downloadReportPdf não são chamados; um teste garante isso).
 * Por cota: filtro → dados da assembleia → "2º Fixo" → screenshot → Histórico (só leitura).
 */

const fs = require('fs');
const { NewconClient } = require('./newcon');
const { lerDadosCredenciamento, lerHistorico, resumirHistorico } = require('./leitura-credenciamento');
const { Plataforma } = require('./plataforma');
const { Registro } = require('./registro');
const { carregarConfig } = require('./config-worker');

const TIPOS_SUPORTADOS = ['dry_run'];

const primeiraLinha = (e) => String((e && e.message) || e).split('\n')[0];
const tagCota = (c) => `${c.grupo}-${c.cota}-${c.versao}`;

class Worker {
  constructor({ config, plataforma, criarNewcon, leitura = { lerDadosCredenciamento, lerHistorico }, saida = console }) {
    this.config = config;
    this.plataforma = plataforma;
    this.criarNewcon = criarNewcon;
    this.leitura = leitura;
    this.saida = saida;
    this.parando = false;
    this.acordar = null;
  }

  /** SIGTERM/SIGINT: termina a cota atual e não começa a próxima. */
  pedirParada(motivo) {
    if (!this.parando) this.saida.log(`· ${motivo}: termino a cota atual (se houver) e paro`);
    this.parando = true;
    if (this.acordar) this.acordar();
  }

  _esperar(ms) {
    return new Promise((resolve) => {
      const t = setTimeout(resolve, ms);
      this.acordar = () => {
        clearTimeout(t);
        resolve();
      };
    });
  }

  async rodar() {
    const { nome, intervaloFilaMs } = this.config.worker;
    this.saida.log(`· worker ${nome} esperando execuções (${TIPOS_SUPORTADOS.join(', ')})`);
    let falhasSeguidas = 0;
    while (!this.parando) {
      let tarefa;
      try {
        tarefa = await this.plataforma.proximaTarefa(TIPOS_SUPORTADOS);
        falhasSeguidas = 0;
      } catch (e) {
        falhasSeguidas++;
        this.saida.log(`! API indisponível (${falhasSeguidas}ª vez): ${primeiraLinha(e)}`);
        await this._esperar(Math.min(60000, intervaloFilaMs * 2 ** Math.min(falhasSeguidas, 4)));
        continue;
      }
      if (!tarefa) {
        await this._esperar(intervaloFilaMs);
        continue;
      }
      await this.processar(tarefa);
    }
    this.saida.log('· worker parado');
  }

  async processar({ execucao, cotas }) {
    const { id, tipo } = execucao;
    if (!TIPOS_SUPORTADOS.includes(tipo)) {
      // Defesa extra: a API não entrega outros tipos a este worker.
      await this.plataforma.finalizar(id, `este worker não executa o tipo "${tipo}"`).catch(() => {});
      return;
    }

    const registro = new Registro({ plataforma: this.plataforma, execucaoId: id, saida: this.saida });
    const estado = { cancelar: false, perdeuTrava: false };
    const renovador = setInterval(async () => {
      try {
        const r = await this.plataforma.renovar(id);
        if (!r.ok) estado.perdeuTrava = true;
        else if (r.cancelamentoSolicitado) estado.cancelar = true;
      } catch (e) {
        registro.warn(`falha ao renovar a trava da execução: ${primeiraLinha(e)}`);
      }
    }, this.config.worker.renovarTravaMs);
    const envio = setInterval(() => registro.enviar(), 1000);

    let desfecho = 'finalizar';
    let erroFatal = null;
    const newcon = this.criarNewcon({ config: this.config, logger: registro });
    try {
      fs.mkdirSync(this.config.paths.screenshotDir, { recursive: true });
      try {
        await newcon.start();
        await newcon.login();
        await newcon.goToCredenciamento();
      } catch (e) {
        erroFatal = `não foi possível entrar no Newcon: ${primeiraLinha(e)}`;
        registro.error(erroFatal);
        return;
      }

      let processadas = 0;
      for (const cota of cotas) {
        if (cota.status !== 'pendente') continue;
        if (estado.perdeuTrava || registro.parar) {
          desfecho = 'abandonar';
          break;
        }
        if (this.parando) {
          desfecho = 'liberar';
          break;
        }
        if (estado.cancelar) break;

        const inicio = await this.plataforma.iniciarCota(cota.id);
        if (!inicio.ok) {
          if (inicio.parar) {
            desfecho = 'abandonar';
            break;
          }
          if (inicio.cancelada) break;
          registro.warn(`cota ${tagCota(cota)} pulada: ${inicio.erro}`);
          continue;
        }

        registro.definirCota(cota.id);
        await this.processarCota(newcon, registro, cota, processadas > 0);
        processadas++;
        await registro.enviar();
        registro.definirCota(null);
      }
    } finally {
      clearInterval(renovador);
      clearInterval(envio);
      await Promise.resolve(newcon.close()).catch(() => {});
      await registro.enviar();
      try {
        if (desfecho === 'liberar') await this.plataforma.liberar(id);
        else if (desfecho === 'finalizar') await this.plataforma.finalizar(id, erroFatal);
        // 'abandonar': a execução não é mais deste worker (trava perdida).
      } catch (e) {
        this.saida.log(`! falha ao encerrar a execução ${id}: ${primeiraLinha(e)}`);
      }
    }
  }

  async processarCota(newcon, registro, cota, voltarAoFiltro) {
    const tag = tagCota(cota);
    const detalhes = {};
    try {
      // Como o script legado: a partir da 2ª cota volta ao filtro, mesmo depois de erro.
      if (voltarAoFiltro) await newcon.backToFilter();
      await newcon.searchCota(cota);
      Object.assign(detalhes, await this.leitura.lerDadosCredenciamento(newcon.page));
      await newcon.selectSegundoFixo();
      const arquivo = await newcon.captureBeforeConfirm(cota);
      await this._enviarScreenshot(cota, arquivo, registro);

      // Depois do screenshot, só leitura: lances já registrados nesta assembleia.
      try {
        const historico = await this.leitura.lerHistorico(newcon.page, this.config.playwright.timeoutMs);
        Object.assign(detalhes, resumirHistorico(historico, detalhes.assembleia_data));
        if (detalhes.lances_nesta_assembleia > 0) {
          registro.warn(
            `cota ${tag} já tem ${detalhes.lances_nesta_assembleia} lance(s) na assembleia de ${detalhes.assembleia_data}: ` +
              detalhes.lances.map((l) => `${l.protocolo} (${l.modalidade})`).join(', ')
          );
        }
      } catch (e) {
        detalhes.historico_lido = false;
        registro.warn(`cota ${tag}: não foi possível ler o Histórico (${primeiraLinha(e)})`);
      }

      await this._concluir(cota, { status: 'verificada', detalhes }, registro);
    } catch (e) {
      const conhecido = e && e.name === 'NewconCotaError';
      const arquivo = await newcon.snapshotError(tag);
      if (arquivo) await this._enviarScreenshot(cota, arquivo, registro);
      await this._concluir(
        cota,
        { status: 'erro_antes_confirmar', erro_tipo: conhecido ? 'conhecido' : 'inesperado', erro: conhecido ? e.message : primeiraLinha(e), detalhes },
        registro
      );
    }
  }

  async _enviarScreenshot(cota, arquivo, registro) {
    try {
      await this.plataforma.enviarScreenshot(cota.id, arquivo);
    } catch (e) {
      registro.warn(`cota ${tagCota(cota)}: screenshot não enviado (${primeiraLinha(e)})`);
    } finally {
      fs.rm(arquivo, { force: true }, () => {});
    }
  }

  async _concluir(cota, conclusao, registro) {
    try {
      const r = await this.plataforma.concluirCota(cota.id, conclusao);
      if (!r.ok) registro.warn(`cota ${tagCota(cota)}: conclusão recusada pela API (${r.erro})`);
    } catch (e) {
      registro.error(`cota ${tagCota(cota)}: não foi possível registrar o resultado (${primeiraLinha(e)})`);
    }
  }
}

if (require.main === module) {
  let config;
  try {
    config = carregarConfig();
  } catch (e) {
    console.error(`erro de configuração: ${e.message}`);
    process.exit(2);
  }
  const plataforma = new Plataforma({ url: config.api.url, token: config.api.token, worker: config.worker.nome });
  const worker = new Worker({ config, plataforma, criarNewcon: (opcoes) => new NewconClient(opcoes) });
  for (const sinal of ['SIGTERM', 'SIGINT']) process.on(sinal, () => worker.pedirParada(sinal));
  worker.rodar().then(
    () => process.exit(0),
    (e) => {
      console.error('erro fatal no worker:', e);
      process.exit(1);
    }
  );
}

module.exports = { Worker, TIPOS_SUPORTADOS };
