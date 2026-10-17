import type { StatusClienteRodada, StatusMensagemBoleto, StatusRodada, TipoRodada } from "./tipos";

// Rótulos e formatações do serviço de boletos. Ficam aqui porque a lista, o detalhe da rodada e
// a tela do WhatsApp mostram as mesmas situações.

export const rotuloTipoRodada: Record<TipoRodada, string> = {
  envio: "envio",
  cobranca: "cobrança",
};

export const rotuloStatusRodada: Record<StatusRodada, string> = {
  na_fila: "na fila",
  em_andamento: "em andamento",
  concluida: "concluída",
  concluida_com_erros: "concluída com erros",
  cancelada: "cancelada",
  falhou: "falhou",
};

export function corDoStatusRodada(status: StatusRodada): "default" | "secondary" | "destructive" | "outline" {
  switch (status) {
    case "concluida":
      return "default";
    case "em_andamento":
    case "na_fila":
      return "secondary";
    case "falhou":
      return "destructive";
    default:
      return "outline";
  }
}

export const rotuloStatusCliente: Record<StatusClienteRodada, string> = {
  pendente: "na fila",
  em_andamento: "consultando",
  consultado: "com boleto",
  sem_boleto: "nada em aberto",
  erro: "erro",
  cancelado: "cancelado",
};

export const rotuloStatusMensagem: Record<StatusMensagemBoleto, string> = {
  pendente: "esperando envio",
  enviada: "enviada",
  falhou: "não entregue",
  cancelada: "não enviada",
};

/** "2026-11" → "novembro de 2026". */
export function mesLegivel(referencia: string): string {
  const [ano, mes] = referencia.split("-");
  if (!ano || !mes) return referencia;
  const data = new Date(Number(ano), Number(mes) - 1, 1);
  return data.toLocaleDateString("pt-BR", { month: "long", year: "numeric" });
}

/** Centavos → "R$ 644,62". */
export function valor(centavos: number): string {
  return (centavos / 100).toLocaleString("pt-BR", { style: "currency", currency: "BRL" });
}

/** "2026-11-16" → "16/11/2026" (sem passar por Date: a data não tem fuso). */
export function vencimento(iso: string): string {
  const [ano, mes, dia] = iso.split("-");
  return ano && mes && dia ? `${dia}/${mes}/${ano}` : iso;
}
