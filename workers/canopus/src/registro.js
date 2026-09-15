'use strict';

/**
 * Logger com a mesma interface do logger.js (info/ok/warn/error), entregue ao NewconClient.
 * Escreve uma linha no stdout e junta os eventos para enviar à API em lotes
 * (aparecem ao vivo na tela da execução).
 */

const NIVEIS = { info: 'info', ok: 'ok', warn: 'aviso', error: 'erro' };
const SIMBOLOS = { info: '·', ok: '✓', warn: '!', error: 'x' };
// Não vão para a tela: caminho local do arquivo e pilha de erro.
const OCULTOS = new Set(['file', 'stack']);

class Registro {
  constructor({ plataforma, execucaoId, saida = console }) {
    this.plataforma = plataforma;
    this.execucaoId = execucaoId;
    this.saida = saida;
    this.fila = [];
    this.cotaAtual = null;
    this.parar = false;
  }

  definirCota(execucaoCotaId) {
    this.cotaAtual = execucaoCotaId;
  }

  _registrar(nivel, mensagem, extra) {
    const visiveis = extra ? Object.fromEntries(Object.entries(extra).filter(([k]) => !OCULTOS.has(k))) : {};
    let texto = mensagem;
    let nivelApi = NIVEIS[nivel];
    if (mensagem === 'dialog' && extra) {
      // Diálogo JS do Newcon (aceito automaticamente pelo NewconClient).
      texto = `Aviso do Newcon (${extra.type}): ${extra.message}`;
      nivelApi = 'aviso';
    } else if (Object.keys(visiveis).length) {
      texto += ' ' + Object.entries(visiveis).map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`).join(' ');
    }

    this.saida.log(`${SIMBOLOS[nivel]} [execução ${this.execucaoId}] ${texto}`);
    if (extra && extra.stack) this.saida.log(extra.stack);
    this.fila.push({ nivel: nivelApi, mensagem: texto, execucao_cota_id: this.cotaAtual, dados: visiveis });
    if (this.fila.length >= 50) this.enviar();
  }

  info(mensagem, extra) { this._registrar('info', mensagem, extra); }
  ok(mensagem, extra) { this._registrar('ok', mensagem, extra); }
  warn(mensagem, extra) { this._registrar('warn', mensagem, extra); }
  error(mensagem, extra) { this._registrar('error', mensagem, extra); }

  /** Envia o que está na fila. Nunca lança: falha de envio só vai para o stdout. */
  async enviar() {
    if (!this.fila.length || this.parar) return;
    const lote = this.fila.splice(0);
    try {
      const r = await this.plataforma.enviarEventos(this.execucaoId, lote);
      if (r && r.parar) this.parar = true;
    } catch (e) {
      this.saida.log(`! [execução ${this.execucaoId}] falha ao enviar ${lote.length} evento(s): ${e.message}`);
    }
  }
}

module.exports = { Registro };
