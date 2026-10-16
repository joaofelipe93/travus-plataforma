'use strict';

// Log do worker: uma linha JSON por evento no stdout (vai para o log do container) e, para o
// que interessa a quem olha a tela, um evento na rodada pela API. Os eventos vão em lote: a
// rodada do dia 8 passa por muitos clientes e um POST por linha seria barulho.
//
// Regra do que entra na mensagem: nunca CPF e nunca valor. Nome de cliente pode (a tela mostra
// de qualquer jeito), documento não — é o dado mais sensível deste serviço.

const LOTE = 20;

class Registro {
  constructor({ plataforma, rodadaId }) {
    this.plataforma = plataforma;
    this.rodadaId = rodadaId;
    this.fila = [];
    this.clienteId = null;
  }

  /** Amarra os próximos eventos a um cliente da rodada (null volta ao nível da rodada). */
  comCliente(clienteId) {
    this.clienteId = clienteId;
    return this;
  }

  _registrar(nivel, mensagem, dados) {
    const linha = { hora: new Date().toISOString(), nivel, mensagem, ...(dados || {}) };
    process.stdout.write(JSON.stringify(linha) + '\n');
    this.fila.push({ cliente_id: this.clienteId, nivel, mensagem, dados: dados || {} });
    if (this.fila.length >= LOTE) return this.descarregar();
    return Promise.resolve();
  }

  info(mensagem, dados) {
    return this._registrar('info', mensagem, dados);
  }
  ok(mensagem, dados) {
    return this._registrar('ok', mensagem, dados);
  }
  aviso(mensagem, dados) {
    return this._registrar('aviso', mensagem, dados);
  }
  erro(mensagem, dados) {
    return this._registrar('erro', mensagem, dados);
  }

  /** Manda o que está na fila. Falha de rede aqui nunca derruba a rodada: o log é secundário. */
  async descarregar() {
    if (!this.fila.length) return;
    const lote = this.fila.splice(0, this.fila.length);
    try {
      await this.plataforma.enviarEventos(this.rodadaId, lote);
    } catch (e) {
      process.stdout.write(
        JSON.stringify({ hora: new Date().toISOString(), nivel: 'aviso', mensagem: `log não foi para a API: ${e.message}` }) + '\n',
      );
    }
  }
}

module.exports = { Registro };
