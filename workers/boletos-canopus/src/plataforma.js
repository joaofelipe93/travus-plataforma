'use strict';

// Cliente das rotas internas da API (porta interna, token de serviço). Mesmo desenho do cliente
// do worker Canopus: 409 não é exceção, é parte do fluxo (a rodada saiu deste worker), então
// vira { ok: false, parar }.

/** Falha de comunicação com a API (rede ou resposta inesperada). */
class ErroPlataforma extends Error {
  constructor(mensagem, status) {
    super(mensagem);
    this.name = 'ErroPlataforma';
    this.status = status;
  }
}

class Plataforma {
  constructor({ url, token, worker, timeoutMs = 30000, fetchImpl = globalThis.fetch }) {
    this.url = url;
    this.token = token;
    this.worker = worker;
    this.timeoutMs = timeoutMs;
    this.fetch = fetchImpl;
  }

  async _post(caminho, { corpo, bruto, contentType = 'application/json' } = {}) {
    const resposta = await this.fetch(`${this.url}${caminho}`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${this.token}`, 'X-Worker': this.worker, 'Content-Type': contentType },
      body: bruto ?? JSON.stringify(corpo ?? {}),
      signal: AbortSignal.timeout(this.timeoutMs),
    });
    const texto = await resposta.text();
    let dados = null;
    try {
      dados = texto ? JSON.parse(texto) : null;
    } catch {
      // resposta sem JSON
    }
    return { status: resposta.status, dados };
  }

  _falha(caminho, r) {
    const detalhe = (r.dados && r.dados.erro) || 'sem detalhes';
    return new ErroPlataforma(`API respondeu ${r.status} em ${caminho}: ${detalhe}`, r.status);
  }

  _conflito(r) {
    const d = r.dados || {};
    return { ok: false, erro: d.erro, parar: !!d.parar };
  }

  /** Próxima rodada da fila, ou null. */
  async proximaRodada() {
    const caminho = '/internal/boletos/rodadas/proxima';
    const r = await this._post(caminho);
    if (r.status === 204) return null;
    if (r.status === 200) return r.dados.rodada;
    throw this._falha(caminho, r);
  }

  async renovar(rodadaId) {
    const caminho = `/internal/boletos/rodadas/${rodadaId}/renovar`;
    const r = await this._post(caminho);
    if (r.status === 200) return { ok: true, cancelamentoSolicitado: !!(r.dados && r.dados.cancelamento_solicitado) };
    if (r.status === 409) return this._conflito(r);
    throw this._falha(caminho, r);
  }

  /** Próximo cliente pendente (já marcado como em andamento), ou null quando acabou. */
  async proximoCliente(rodadaId) {
    const caminho = `/internal/boletos/rodadas/${rodadaId}/proximo-cliente`;
    const r = await this._post(caminho);
    if (r.status === 204) return null;
    if (r.status === 200) return r.dados.cliente;
    if (r.status === 409) return null; // a rodada saiu deste worker; o laço descobre ao renovar
    throw this._falha(caminho, r);
  }

  /** Grava as parcelas em aberto de uma cota e devolve os ids criados. */
  async registrarParcelas(clienteId, parcelas) {
    const caminho = `/internal/boletos/clientes/${clienteId}/parcelas`;
    const r = await this._post(caminho, { corpo: { parcelas } });
    if (r.status === 201) return (r.dados && r.dados.ids) || [];
    throw this._falha(caminho, r);
  }

  async enviarPdf(parcelaId, bytes) {
    const caminho = `/internal/boletos/parcelas/${parcelaId}/pdf`;
    const r = await this._post(caminho, { bruto: bytes, contentType: 'application/pdf' });
    if (r.status === 201) return r.dados.id;
    throw this._falha(caminho, r);
  }

  async concluirCliente(clienteId, resultado) {
    const caminho = `/internal/boletos/clientes/${clienteId}/concluir`;
    const r = await this._post(caminho, { corpo: resultado });
    if (r.status === 204) return { ok: true };
    if (r.status === 409) return this._conflito(r);
    throw this._falha(caminho, r);
  }

  async enviarEventos(rodadaId, eventos) {
    if (!eventos.length) return;
    const caminho = `/internal/boletos/rodadas/${rodadaId}/eventos`;
    const r = await this._post(caminho, { corpo: { eventos } });
    if (r.status === 204 || r.status === 409) return;
    throw this._falha(caminho, r);
  }

  async finalizar(rodadaId, erro) {
    const caminho = `/internal/boletos/rodadas/${rodadaId}/finalizar`;
    const r = await this._post(caminho, { corpo: erro ? { erro } : {} });
    if (r.status === 200) return { ok: true, status: r.dados && r.dados.status };
    if (r.status === 409) return this._conflito(r);
    throw this._falha(caminho, r);
  }

  async liberar(rodadaId) {
    const caminho = `/internal/boletos/rodadas/${rodadaId}/liberar`;
    const r = await this._post(caminho);
    if (r.status === 204 || r.status === 409) return { ok: true };
    throw this._falha(caminho, r);
  }
}

module.exports = { Plataforma, ErroPlataforma };
