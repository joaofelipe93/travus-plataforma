"use client";

// Resumo da carteira, no topo das telas de Clientes e Cotas. Os números acompanham o que
// está filtrado: filtrar por grupo muda o resumo junto. Três leituras, cada uma com a forma
// que o dado pede — contagem em número grande, composição em barra única, comparação entre
// grupos em barras ordenadas.

import { cn } from "cn";
import type { Cota } from "@/lib/tipos";

// Ordem fixa das cores (nunca reciclada): a quinta categoria e as seguintes viram "Outros".
const CORES = ["bg-chart-1", "bg-chart-2", "bg-chart-3", "bg-chart-4"] as const;
const COR_OUTROS = "bg-chart-5";
const MAX_CATEGORIAS = CORES.length;

type Fatia = { rotulo: string; valor: number; cor: string };

function porTipo(cotas: Cota[]): Fatia[] {
  const contagem = new Map<string, number>();
  for (const c of cotas) {
    const tipo = c.tipo_consorcio?.trim() || "Sem tipo";
    contagem.set(tipo, (contagem.get(tipo) ?? 0) + 1);
  }
  const ordenado = [...contagem.entries()].sort((a, b) => b[1] - a[1]);
  const principais = ordenado.slice(0, MAX_CATEGORIAS).map(([rotulo, valor], i) => ({ rotulo, valor, cor: CORES[i] }));
  const resto = ordenado.slice(MAX_CATEGORIAS).reduce((soma, [, v]) => soma + v, 0);
  return resto > 0 ? [...principais, { rotulo: "Outros", valor: resto, cor: COR_OUTROS }] : principais;
}

function porGrupo(cotas: Cota[], quantos = 5) {
  const contagem = new Map<string, number>();
  for (const c of cotas) {
    contagem.set(c.grupo, (contagem.get(c.grupo) ?? 0) + 1);
  }
  return [...contagem.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])).slice(0, quantos);
}

export function ResumoCarteira({
  cotas,
  clientes,
  filtrado = false,
  className,
}: {
  cotas: Cota[] | undefined;
  clientes?: number;
  /** Muda o texto para deixar claro que os números seguem o filtro. */
  filtrado?: boolean;
  className?: string;
}) {
  if (!cotas) {
    return <div className={cn("h-28 animate-pulse rounded-xl border bg-card/50", className)} />;
  }

  const ativas = cotas.filter((c) => c.ativa).length;
  const tipos = porTipo(cotas);
  const grupos = porGrupo(cotas);
  const maiorGrupo = grupos[0]?.[1] ?? 1;
  const total = cotas.length;

  return (
    <section
      className={cn("grid gap-6 rounded-xl border bg-card p-5 md:grid-cols-[auto_1fr_auto] md:gap-10", className)}
      aria-label="Resumo da carteira"
    >
      <div className="flex gap-8">
        {clientes !== undefined && <Numero valor={clientes} rotulo={clientes === 1 ? "cliente" : "clientes"} />}
        <Numero valor={total} rotulo={total === 1 ? "cota" : "cotas"} />
        <Numero valor={ativas} rotulo="ativas" atencao={total > 0 && ativas === 0} />
      </div>

      <div className="flex flex-col justify-center gap-2">
        <div className="flex h-2 w-full gap-0.5 overflow-hidden rounded-full" role="img" aria-label={legenda(tipos)}>
          {total === 0 ? (
            <span className="flex-1 rounded-full bg-border" />
          ) : (
            tipos.map((f) => (
              <span
                key={f.rotulo}
                className={cn("rounded-full", f.cor)}
                style={{ width: `${(f.valor / total) * 100}%` }}
                title={`${f.rotulo}: ${f.valor}`}
              />
            ))
          )}
        </div>
        <ul className="flex flex-wrap gap-x-5 gap-y-1 text-sm">
          {tipos.length === 0 && <li className="text-muted-foreground">Nada para mostrar com este filtro.</li>}
          {tipos.map((f) => (
            <li key={f.rotulo} className="flex items-center gap-2">
              <span className={cn("size-2 rounded-full", f.cor)} aria-hidden />
              <span className="text-muted-foreground">{f.rotulo}</span>
              <span className="tabular-nums">{f.valor}</span>
            </li>
          ))}
        </ul>
      </div>

      {grupos.length > 0 && (
        <div className="flex min-w-48 flex-col gap-1.5">
          <p className="text-sm text-muted-foreground">{filtrado ? "Grupos no filtro" : "Grupos com mais cotas"}</p>
          <ul className="flex flex-col gap-1">
            {grupos.map(([grupo, quantas]) => (
              <li key={grupo} className="grid grid-cols-[4.5rem_1fr_1.5rem] items-center gap-2 text-sm">
                <span className="tabular-nums text-muted-foreground">{grupo}</span>
                <span className="h-1.5 rounded-full bg-border/60">
                  <span
                    className="block h-1.5 rounded-full bg-chart-1"
                    style={{ width: `${Math.max((quantas / maiorGrupo) * 100, 6)}%` }}
                  />
                </span>
                <span className="text-right tabular-nums">{quantas}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}

function legenda(fatias: Fatia[]) {
  return fatias.map((f) => `${f.rotulo}: ${f.valor}`).join(", ");
}

function Numero({ valor, rotulo, atencao = false }: { valor: number; rotulo: string; atencao?: boolean }) {
  return (
    <div>
      <p className={cn("font-heading text-3xl leading-none tabular-nums", atencao && "text-aviso")}>{valor}</p>
      <p className="mt-1.5 text-sm text-muted-foreground">{rotulo}</p>
    </div>
  );
}
