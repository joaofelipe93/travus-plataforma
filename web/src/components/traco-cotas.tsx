"use client";

// Uma carteira de consórcio é feita de cotas contáveis, não de porcentagem: o traço mostra
// uma marca por cota, ativa ou não. Acima de doze cotas a contagem uma a uma deixa de ajudar
// e o traço vira uma barra proporcional.

import { cn } from "cn";

const LIMITE_SEGMENTOS = 12;

export function TracoCotas({
  total,
  ativas,
  className,
  tamanho = "normal",
}: {
  total: number;
  ativas: number;
  className?: string;
  tamanho?: "normal" | "grande";
}) {
  const altura = tamanho === "grande" ? "h-2" : "h-1.5";
  const rotulo = `${ativas} de ${total} cota(s) ativa(s)`;

  if (total === 0) {
    return <div className={cn("w-full rounded-full bg-border/60", altura, className)} role="img" aria-label="sem cotas" />;
  }

  if (total <= LIMITE_SEGMENTOS) {
    return (
      <div className={cn("flex w-full gap-0.5", className)} role="img" aria-label={rotulo}>
        {Array.from({ length: total }, (_, i) => (
          <span
            key={i}
            className={cn("flex-1 rounded-full transition-colors", altura, i < ativas ? "bg-chart-1" : "bg-border")}
          />
        ))}
      </div>
    );
  }

  const proporcao = Math.round((ativas / total) * 100);
  return (
    <div className={cn("w-full overflow-hidden rounded-full bg-border", altura, className)} role="img" aria-label={rotulo}>
      <div className={cn("rounded-full bg-chart-1 transition-[width]", altura)} style={{ width: `${proporcao}%` }} />
    </div>
  );
}
