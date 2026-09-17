"use client";

// Resumo da carteira, no topo das telas de Clientes e Cotas. Três leituras e nada mais: quantas
// cotas estão valendo, de que tipo são e em que grupos estão. Os números acompanham o filtro.

import { cn } from "cn";
import type { Cota } from "@/lib/tipos";

// Ordem fixa das cores (nunca reciclada): a quinta categoria e as seguintes viram "Outros".
const CORES = ["bg-chart-1", "bg-chart-2", "bg-chart-3", "bg-chart-4"] as const;
const COR_OUTROS = "bg-chart-5";
const MAX_CATEGORIAS = CORES.length;
const MAX_GRUPOS = 4;

// O tipo de consórcio é a coisa mais concreta da carteira: casa, carro, moto. O emoji lê antes
// da palavra; a palavra e o número continuam ali para quem não vê o emoji.
const EMOJI: Record<string, string> = {
  IMOVEL: "🏠",
  AUTOMOVEL: "🚗",
  MOTO: "🏍️",
  MOTOCICLETA: "🏍️",
  CAMINHAO: "🚚",
  PESADOS: "🚚",
  SERVICO: "🧾",
  SERVICOS: "🧾",
};

function emojiDoTipo(tipo: string) {
  const chave = tipo
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toUpperCase()
    .trim();
  return EMOJI[chave] ?? "▫️";
}

type Fatia = { rotulo: string; valor: number; cor: string; emoji: string };

function porTipo(cotas: Cota[]): Fatia[] {
  const contagem = new Map<string, number>();
  for (const c of cotas) {
    const tipo = c.tipo_consorcio?.trim() || "Sem tipo";
    contagem.set(tipo, (contagem.get(tipo) ?? 0) + 1);
  }
  const ordenado = [...contagem.entries()].sort((a, b) => b[1] - a[1]);
  const principais = ordenado.slice(0, MAX_CATEGORIAS).map(([rotulo, valor], i) => ({
    rotulo: capitalizar(rotulo),
    valor,
    cor: CORES[i],
    emoji: emojiDoTipo(rotulo),
  }));
  const resto = ordenado.slice(MAX_CATEGORIAS).reduce((soma, [, v]) => soma + v, 0);
  return resto > 0 ? [...principais, { rotulo: "Outros", valor: resto, cor: COR_OUTROS, emoji: "▫️" }] : principais;
}

function porGrupo(cotas: Cota[]) {
  const contagem = new Map<string, number>();
  for (const c of cotas) {
    contagem.set(c.grupo, (contagem.get(c.grupo) ?? 0) + 1);
  }
  return [...contagem.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
}

// "IMÓVEL" grita; "Imóvel" conversa.
function capitalizar(texto: string) {
  return texto.charAt(0).toUpperCase() + texto.slice(1).toLowerCase();
}

export function ResumoCarteira({
  cotas,
  clientes,
  filtrado = false,
  className,
}: {
  cotas: Cota[] | undefined;
  clientes?: number;
  /** Deixa claro que os números seguem o filtro. */
  filtrado?: boolean;
  className?: string;
}) {
  if (!cotas) {
    return <div className={cn("h-20 animate-pulse rounded-xl bg-card/60", className)} />;
  }

  const total = cotas.length;
  const ativas = cotas.filter((c) => c.ativa).length;
  const tipos = porTipo(cotas);
  const grupos = porGrupo(cotas);

  if (total === 0) {
    return (
      <p className={cn("text-sm text-muted-foreground", className)}>
        {filtrado ? "Nenhuma cota com este filtro." : "A carteira ainda está vazia."}
      </p>
    );
  }

  return (
    <section
      className={cn("flex flex-wrap items-center gap-x-12 gap-y-6 border-b border-border/60 pb-6", className)}
      aria-label={filtrado ? "Resumo do que está filtrado" : "Resumo da carteira"}
    >
      <div>
        <p className="flex items-baseline gap-2">
          <span className="font-heading text-4xl leading-none tracking-tight tabular-nums">{ativas}</span>
          <span className="text-muted-foreground">{ativas === 1 ? "cota ativa" : "cotas ativas"}</span>
        </p>
        <p className="mt-2 text-sm text-muted-foreground">
          {ativas === total ? `todas as ${total}` : `de ${total}`}
          {clientes !== undefined && ` · ${clientes} ${clientes === 1 ? "cliente" : "clientes"}`}
        </p>
      </div>

      <div className="flex min-w-56 flex-1 flex-col gap-2.5">
        <div className="flex h-1 w-full gap-0.5" role="img" aria-label={tipos.map((f) => `${f.rotulo}: ${f.valor}`).join(", ")}>
          {tipos.map((f) => (
            <span key={f.rotulo} className={cn("rounded-full", f.cor)} style={{ width: `${(f.valor / total) * 100}%` }} />
          ))}
        </div>
        <ul className="flex flex-wrap gap-x-6 gap-y-2">
          {tipos.map((f) => (
            <li key={f.rotulo} className="flex items-center gap-2">
              <span aria-hidden className="text-base leading-none">
                {f.emoji}
              </span>
              <span className="text-sm text-muted-foreground">{f.rotulo}</span>
              <span className="text-sm tabular-nums">{f.valor}</span>
            </li>
          ))}
        </ul>
      </div>

      <ul className="flex flex-wrap items-center gap-2" aria-label="Cotas por grupo">
        <li className="text-sm text-muted-foreground">Grupos</li>
        {grupos.slice(0, MAX_GRUPOS).map(([grupo, quantas]) => (
          <li
            key={grupo}
            className="flex items-center gap-2 rounded-full bg-accent/60 px-3 py-1 text-sm"
            title={`Grupo ${grupo}: ${quantas} cota(s)`}
          >
            <span className="tabular-nums text-muted-foreground">{grupo}</span>
            <span className="tabular-nums">{quantas}</span>
          </li>
        ))}
        {grupos.length > MAX_GRUPOS && (
          <li className="text-sm text-muted-foreground">
            +{grupos.length - MAX_GRUPOS} {grupos.length - MAX_GRUPOS === 1 ? "grupo" : "grupos"}
          </li>
        )}
      </ul>
    </section>
  );
}
