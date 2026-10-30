import type { StatusConversa, StatusMensagemBot } from "./tipos";

// Rótulos e cores do serviço de Atendimento, num lugar só: o mesmo estado tem o mesmo nome e a
// mesma cor no painel, na lista de conversas e dentro da conversa (como lib/boletos.ts faz).

export const rotuloStatusConversa: Record<StatusConversa, string> = {
  aberta: "aberta",
  encerrada: "encerrada",
  abandonada: "abandonada",
  erro: "com erro",
};

export function corDoStatusConversa(status: StatusConversa): "default" | "secondary" | "destructive" | "outline" {
  if (status === "aberta") return "default";
  if (status === "erro") return "destructive";
  return "secondary";
}

// Por que a conversa terminou. Os quatro primeiros são o que o bot pode dizer ao encerrar; os
// outros são decisão da plataforma (as travas da etapa B).
export const rotuloMotivo: Record<string, string> = {
  concluido: "fluxo concluído",
  fora_do_escopo: "assunto fora do escopo",
  desistiu: "a pessoa desistiu",
  pediu_humano: "pediu atendente",
  teto_tokens: "teto de tokens da conversa",
  bot_desligado: "o bot foi desligado",
};

export function motivoLegivel(motivo: string | null): string | null {
  if (!motivo) return null;
  return rotuloMotivo[motivo] ?? motivo;
}

export const rotuloStatusMensagemBot: Record<StatusMensagemBot, string> = {
  recebida: "recebida",
  pendente: "na fila",
  enviada: "enviada",
  falhou: "não entregue",
  cancelada: "não enviada",
};

export function corDoStatusMensagem(status: StatusMensagemBot): "default" | "secondary" | "destructive" | "outline" {
  if (status === "enviada") return "default";
  if (status === "falhou") return "destructive";
  return "secondary";
}

/** Gasto de uma conversa em tokens, curto o suficiente para caber numa linha de lista. */
export function tokensLegivel(tokens: number): string {
  if (tokens < 1000) return `${tokens} tokens`;
  return `${(tokens / 1000).toFixed(tokens < 10_000 ? 1 : 0)} mil tokens`;
}

/**
 * Slug sugerido a partir do nome do bot: é o que o worker usa como pasta da sessão do WhatsApp,
 * então só minúsculas, dígitos e hífen (a API recusa o resto). A pessoa pode editar.
 */
export function slugSugerido(nome: string): string {
  return nome
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
}

/**
 * Nome do dado a partir do texto da pergunta: "Quantas vidas vocês querem?" →
 * "quantas_vidas_voces_querem". É o que vai para o lead e para o cadastro, e precisa começar com
 * letra (a API recusa o resto). A tela mostra isso escondido: é detalhe técnico, não decisão de
 * quem escreve o atendimento.
 */
export function campoDaPergunta(pergunta: string): string {
  return pergunta
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "")
    .replace(/^[0-9_]+/, "")
    .slice(0, 31)
    .replace(/_+$/, "");
}
