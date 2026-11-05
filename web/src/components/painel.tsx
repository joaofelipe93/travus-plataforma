"use client";

import Link from "next/link";
import { cn } from "@/lib/utils";

// Peças do painel de cada serviço (/lances/painel, /boletos/painel, /reservas/painel).
//
// Os três painéis respondem às mesmas perguntas, na mesma ordem: o que está acontecendo agora,
// o que a plataforma vai fazer sozinha, e o que está pendente ou precisa de alguém. As peças
// moram aqui para que serviço novo ganhe um painel igual sem reinventar o desenho — e para que
// "ligado", "desligado" e "erro" tenham sempre a mesma cor em toda a plataforma.

export type Tom = "neutro" | "ok" | "aviso" | "erro" | "destaque";

const tomDoTexto: Record<Tom, string> = {
  neutro: "text-foreground",
  ok: "text-sucesso",
  aviso: "text-aviso",
  erro: "text-destructive",
  destaque: "text-ouro",
};

const tomDaBorda: Record<Tom, string> = {
  neutro: "border-border",
  ok: "border-sucesso/40",
  aviso: "border-aviso/50",
  erro: "border-destructive/50",
  destaque: "border-ouro/40",
};

/** Fileira de números do painel: dois por linha no celular, quatro no monitor. */
export function Indicadores({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn("grid grid-cols-2 gap-4 lg:grid-cols-4", className)}>{children}</div>;
}

/** Um número do painel. Com href, o cartão inteiro leva à tela que detalha o número. */
export function Indicador({
  rotulo,
  valor,
  detalhe,
  tom = "neutro",
  href,
}: {
  rotulo: string;
  valor: React.ReactNode;
  detalhe?: React.ReactNode;
  tom?: Tom;
  href?: string;
}) {
  const corpo = (
    <>
      <p className="text-sm text-muted-foreground">{rotulo}</p>
      <p className={cn("mt-2 font-heading text-[1.75rem] leading-none font-semibold tabular-nums", tomDoTexto[tom])}>
        {valor}
      </p>
      {detalhe && <p className="mt-2 text-xs leading-snug text-muted-foreground">{detalhe}</p>}
    </>
  );
  const classe = cn("block rounded-xl border bg-card/50 p-5", tomDaBorda[tom]);
  return href ? (
    <Link
      href={href}
      className={cn(
        classe,
        "outline-none transition-colors hover:border-ouro/60 focus-visible:ring-2 focus-visible:ring-ring",
      )}
    >
      {corpo}
    </Link>
  ) : (
    <div className={classe}>{corpo}</div>
  );
}

/** Bloco cercado do painel: um assunto por bloco (o automático, a conexão, a fila). */
export function Bloco({
  titulo,
  descricao,
  acoes,
  tom = "neutro",
  className,
  children,
}: {
  titulo: React.ReactNode;
  descricao?: React.ReactNode;
  acoes?: React.ReactNode;
  tom?: Tom;
  className?: string;
  children?: React.ReactNode;
}) {
  return (
    <section className={cn("flex min-w-0 flex-col gap-4 rounded-xl border bg-card/50 p-5", tomDaBorda[tom], className)}>
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-2">
        <div className="min-w-0">
          <h2 className="font-heading text-[1.05rem] font-semibold">{titulo}</h2>
          {descricao && <p className="mt-1 max-w-prose text-sm text-muted-foreground">{descricao}</p>}
        </div>
        {acoes && <div className="flex flex-wrap items-center gap-2">{acoes}</div>}
      </div>
      {children}
    </section>
  );
}

export type ItemEstado = {
  rotulo: React.ReactNode;
  valor: React.ReactNode;
  tom?: Tom;
  detalhe?: React.ReactNode;
};

/** Lista "o que é → como está": o jeito de mostrar chave ligada, prazo e próximo horário. */
export function Estados({ itens, className }: { itens: readonly ItemEstado[]; className?: string }) {
  return (
    <dl className={cn("flex flex-col divide-y divide-border/70", className)}>
      {itens.map((item, indice) => (
        <div
          key={indice}
          className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1 py-2 first:pt-0 last:pb-0"
        >
          <dt className="min-w-0 text-sm text-muted-foreground">{item.rotulo}</dt>
          <dd className={cn("text-sm font-medium", tomDoTexto[item.tom ?? "neutro"])}>
            {item.valor}
            {item.detalhe && <span className="ml-2 font-normal text-muted-foreground">{item.detalhe}</span>}
          </dd>
        </div>
      ))}
    </dl>
  );
}

/** "Nada aqui ainda", no mesmo lugar e no mesmo tom em todos os painéis. */
export function Vazio({ children }: { children: React.ReactNode }) {
  return <p className="py-2 text-sm text-muted-foreground">{children}</p>;
}
