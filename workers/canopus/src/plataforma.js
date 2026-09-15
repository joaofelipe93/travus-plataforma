'use strict';

const fs = require('fs');

/** Falha de comunicação com a API (rede ou resposta inesperada). */
class ErroPlataforma extends Error {
  constructor(mensagem, status) {
    super(mensagem);
    this.name = 'ErroPlataforma';
    this.status = status;
  }
}

/**
 * Cliente das rotas internas da API (porta interna, token de serviço). Respostas 409
 * viram objetos ({ ok: false, parar, cancelada }) porque fazem parte do fluxo normal.
 */
class Plataforma {
  constructor({ url, token, worker, fetchImpl = globalThis.fetch }) {
    this.url = url;
    this.token = token;
    this.worker = worker;
    this.fetch = fetchImpl;
  }

  async _post(caminho, { corpo, bruto, contentType = 'application/json' } = {}) {
    const resposta = await this.fetch(`${this.url}${caminho}`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${this.token}`, 'X-Worker': this.worker, 'Content-Type': contentType },
      body: bruto ?? JSON.stringify(corpo ?? {}),
      signal: AbortSignal.timeout(30000),
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
    return new ErroPlataforma(`API respondeu ${r.status} em ${caminho}: ${(r.dados && r.dados.erro) || 'sem detalhes'}`, r.status);
  }

  _conflito(r) {
    const d = r.dados || {};
    return { ok: false, erro: d.erro, parar: !!d.parar, cancelada: !!d.cancelada };
  }

  /** Próxima execução da fila, ou null. */
  async proximaTarefa(tipos) {
    const caminho = '/internal/tarefas/proxima';
    const r = await this._post(caminho, { corpo: { tipos } });
    if (r.status === 204) return null;
    if (r.status === 200) return r.dados;
    throw this._falha(caminho, r);
  }

  async renovar(execucaoId) {
    const caminho = `/internal/execucoes/${execucaoId}/renovar`;
    const r = await this._post(caminho);
    if (r.status === 200) return { ok: true, cancelamentoSolicitado: !!(r.dados && r.dados.cancelamento_solicitado) };
    if (r.status === 409) return this._conflito(r);
    throw this._falha(caminho, r);
  }

  async enviarEventos(execucaoId, eventos) {
    const caminho = `/internal/execucoes/${execucaoId}/eventos`;
    const r = await this._post(caminho, { corpo: { eventos } });
    if (r.status === 204) return { ok: true };
    if (r.status === 409) return this._conflito(r);
    throw this._falha(caminho, r);
  }

  async iniciarCota(execucaoCotaId) {
    const caminho = `/internal/execucao-cotas/${execucaoCotaId}/iniciar`;
    const r = await this._post(caminho);
    if (r.status === 204) return { ok: true };
    if (r.status === 409) return this._conflito(r);
    throw this._falha(caminho, r);
  }

  async concluirCota(execucaoCotaId, conclusao) {
    const caminho = `/internal/execucao-cotas/${execucaoCotaId}/concluir`;
    const r = await this._post(caminho, { corpo: conclusao });
    if (r.status === 204) return { ok: true };
    if (r.status === 409) return this._conflito(r);
    throw this._falha(caminho, r);
  }

  async enviarScreenshot(execucaoCotaId, arquivo) {
    const caminho = `/internal/execucao-cotas/${execucaoCotaId}/screenshot`;
    const r = await this._post(caminho, { bruto: fs.readFileSync(arquivo), contentType: 'image/png' });
    if (r.status === 201) return r.dados.id;
    throw this._falha(caminho, r);
  }

  /** Lance real: grava confirmacao_iniciada. Só clique em Confirmar se voltar { ok: true }. */
  async marcarConfirmacaoIniciada(execucaoCotaId, { assembleia_data }) {
    const caminho = `/internal/execucao-cotas/${execucaoCotaId}/confirmacao-iniciada`;
    const r = await this._post(caminho, { corpo: { assembleia_data } });
    if (r.status === 204) return { ok: true };
    if (r.status === 409 || r.status === 422) return this._conflito(r);
    throw this._falha(caminho, r);
  }

  async concluirConfirmacao(execucaoCotaId, conclusao) {
    const caminho = `/internal/execucao-cotas/${execucaoCotaId}/concluir-confirmacao`;
    const r = await this._post(caminho, { corpo: conclusao });
    if (r.status === 204) return { ok: true };
    if (r.status === 409 || r.status === 422) return this._conflito(r);
    throw this._falha(caminho, r);
  }

  async concluirReimpressao(execucaoCotaId, conclusao) {
    const caminho = `/internal/execucao-cotas/${execucaoCotaId}/concluir-reimpressao`;
    const r = await this._post(caminho, { corpo: conclusao });
    if (r.status === 204) return { ok: true };
    if (r.status === 409 || r.status === 422) return this._conflito(r);
    throw this._falha(caminho, r);
  }

  async enviarPdf(execucaoCotaId, arquivo) {
    const caminho = `/internal/execucao-cotas/${execucaoCotaId}/pdf`;
    const r = await this._post(caminho, { bruto: fs.readFileSync(arquivo), contentType: 'application/pdf' });
    if (r.status === 201) return r.dados.id;
    throw this._falha(caminho, r);
  }

  async finalizar(execucaoId, erro) {
    const caminho = `/internal/execucoes/${execucaoId}/finalizar`;
    const r = await this._post(caminho, { corpo: erro ? { erro } : {} });
    if (r.status === 200) return r.dados.status;
    if (r.status === 409) return null;
    throw this._falha(caminho, r);
  }

  async liberar(execucaoId) {
    const caminho = `/internal/execucoes/${execucaoId}/liberar`;
    const r = await this._post(caminho);
    if (r.status === 204 || r.status === 409) return;
    throw this._falha(caminho, r);
  }
}

module.exports = { Plataforma, ErroPlataforma };
