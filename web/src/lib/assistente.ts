// Assistente da tela inicial (api/internal/httpapi/assistente.go). A conversa fica só no
// navegador: cada pergunta manda o histórico e a resposta chega em SSE pelo corpo do POST
// (EventSource não faz POST).

import { cabecalhoCsrf, ErroApi, irParaLogin, mensagemPadrao } from "@/lib/api";

export type EstadoAssistente = {
  disponivel: boolean;
  modelo?: string;
  consulta_livre: boolean;
  reservas: boolean;
};

export type MensagemEnviada = { papel: "usuario" | "assistente"; texto: string };

export type EventoAssistente =
  | { tipo: "texto"; texto: string }
  | { tipo: "ferramenta"; ferramenta: string; rotulo: string }
  | { tipo: "fim"; parada?: string }
  | { tipo: "erro"; mensagem: string };

// Limites da API (internal/assistente): conversa longa perde o começo.
const maximoMensagens = 30;
const maximoResposta = 16_000;

export function historicoParaEnviar(msgs: MensagemEnviada[]): MensagemEnviada[] {
  let recorte = msgs;
  while (recorte.length > maximoMensagens - 1 || (recorte.length > 0 && recorte[0].papel !== "usuario")) {
    recorte = recorte.slice(recorte[0].papel === "usuario" ? 2 : 1);
  }
  return recorte.map((m) => (m.papel === "assistente" ? { ...m, texto: m.texto.slice(0, maximoResposta) } : m));
}

export async function conversar(
  mensagens: MensagemEnviada[],
  aoEvento: (e: EventoAssistente) => void,
  sinal: AbortSignal,
): Promise<void> {
  let resposta: Response;
  try {
    resposta = await fetch("/api/assistente/conversa", {
      method: "POST",
      headers: { "Content-Type": "application/json", ...cabecalhoCsrf() },
      body: JSON.stringify({ mensagens }),
      cache: "no-store",
      signal: sinal,
    });
  } catch (e) {
    if (sinal.aborted) throw e;
    throw new ErroApi(0, "Sem conexão com o servidor. Verifique a internet e tente de novo.");
  }
  if (!resposta.ok || !resposta.body) {
    let mensagem = mensagemPadrao(resposta.status);
    try {
      mensagem = ((await resposta.json()) as { erro?: string }).erro ?? mensagem;
    } catch {
      // Resposta do gateway, sem JSON.
    }
    if (resposta.status === 401) irParaLogin();
    throw new ErroApi(resposta.status, mensagem);
  }

  const leitor = resposta.body.pipeThrough(new TextDecoderStream()).getReader();
  let resto = "";
  for (;;) {
    const { value, done } = await leitor.read();
    if (done) break;
    resto += value;
    let fim: number;
    while ((fim = resto.indexOf("\n\n")) >= 0) {
      const bloco = resto.slice(0, fim);
      resto = resto.slice(fim + 2);
      const evento = lerBloco(bloco);
      if (evento) aoEvento(evento);
    }
  }
}

function lerBloco(bloco: string): EventoAssistente | null {
  let tipo = "";
  let dados = "";
  for (const linha of bloco.split("\n")) {
    if (linha.startsWith("event: ")) tipo = linha.slice(7);
    else if (linha.startsWith("data: ")) dados += linha.slice(6);
  }
  if (!tipo || !dados) return null; // ": ping"
  try {
    const corpo = JSON.parse(dados) as Record<string, unknown>;
    switch (tipo) {
      case "texto":
        return { tipo, texto: String(corpo.texto ?? "") };
      case "ferramenta":
        return { tipo, ferramenta: String(corpo.ferramenta ?? ""), rotulo: String(corpo.rotulo ?? "Consultando") };
      case "fim":
        return { tipo, parada: corpo.parada as string | undefined };
      case "erro":
        return { tipo, mensagem: String(corpo.mensagem ?? "O assistente falhou ao responder.") };
    }
  } catch {
    // Bloco malformado: ignora.
  }
  return null;
}
