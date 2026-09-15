import type { VariantProps } from "class-variance-authority";
import type { badgeVariants } from "@/components/ui/badge";
import type { StatusCotaExecucao, StatusDrive, StatusExecucao, TipoExecucao } from "@/lib/tipos";

type Variante = NonNullable<VariantProps<typeof badgeVariants>["variant"]>;

export const situacaoExecucao: Record<StatusExecucao, { rotulo: string; variante: Variante }> = {
  na_fila: { rotulo: "Na fila", variante: "outline" },
  em_andamento: { rotulo: "Em andamento", variante: "default" },
  concluida: { rotulo: "Concluída", variante: "secondary" },
  concluida_com_erros: { rotulo: "Concluída com erros", variante: "outline" },
  cancelada: { rotulo: "Cancelada", variante: "outline" },
  falhou: { rotulo: "Falhou", variante: "destructive" },
};

export const situacaoCota: Record<StatusCotaExecucao, { rotulo: string; variante: Variante }> = {
  pendente: { rotulo: "Pendente", variante: "outline" },
  em_andamento: { rotulo: "Em andamento", variante: "default" },
  verificada: { rotulo: "Pronta para confirmar", variante: "secondary" },
  erro_antes_confirmar: { rotulo: "Erro (lance não registrado)", variante: "destructive" },
  confirmacao_iniciada: { rotulo: "Confirmando", variante: "default" },
  confirmada: { rotulo: "Lance registrado", variante: "secondary" },
  erro_apos_confirmar: { rotulo: "Erro após confirmar", variante: "destructive" },
  reimpressa: { rotulo: "Comprovante obtido", variante: "secondary" },
  cancelada: { rotulo: "Cancelada", variante: "outline" },
};

export const situacaoDrive: Record<StatusDrive, { rotulo: string; variante: Variante }> = {
  pendente: { rotulo: "Drive: aguardando", variante: "outline" },
  enviando: { rotulo: "Drive: enviando", variante: "outline" },
  enviado: { rotulo: "No Drive", variante: "secondary" },
  erro: { rotulo: "Drive: erro", variante: "destructive" },
  sem_pdf: { rotulo: "Sem PDF", variante: "outline" },
  nao_enviar: { rotulo: "Fora do Drive", variante: "outline" },
};

export function execucaoTerminou(status: StatusExecucao) {
  return status === "concluida" || status === "concluida_com_erros" || status === "cancelada" || status === "falhou";
}

export function nomeTipo(tipo: TipoExecucao) {
  return { dry_run: "Dry-run", real: "Lance real", reimpressao: "Reimpressão" }[tipo];
}

export function hora(iso: string) {
  return new Date(iso).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}
