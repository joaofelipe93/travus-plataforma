"use client";

import { cn } from "cn";

// Esqueleto das telas. A regra de largura mora aqui: a página usa a tela toda, com margens
// generosas, e só para de crescer em 112rem — num monitor largo o conteúdo não vira uma linha
// ilegível, e em nenhuma tela ele fica amontoado num canto.
//
//   Faixa    largura útil de tudo: o cabeçalho e o conteúdo alinham na mesma margem
//   Topo     faixa fixa no alto com título, números e ações (e as abas, nos serviços)
//   Pagina   uma tela solta: Topo + conteúdo (Início, Status, Meu perfil)
//   Colunas  conteúdo largo + coluna de contexto, para a tela não ser uma tira à esquerda
//   Secao    bloco com título e um fio embaixo dele
//   Painel   superfície cercada: só para o que se opera (formulário, lista de escolha)

export function Faixa({ className, children }: { className?: string; children: React.ReactNode }) {
  return <div className={cn("mx-auto w-full max-w-[112rem] px-6 lg:px-10 xl:px-14", className)}>{children}</div>;
}

export type Numero = { valor: React.ReactNode; rotulo: string };

/** Números do topo: o valor na frente, o que ele é logo atrás. */
export function Numeros({ itens, className }: { itens: readonly Numero[]; className?: string }) {
  return (
    <ul className={cn("flex flex-wrap items-baseline gap-x-8 gap-y-2", className)}>
      {itens.map((n) => (
        <li key={n.rotulo} className="flex items-baseline gap-1.5">
          <span className="font-heading text-base leading-none tabular-nums">{n.valor}</span>
          <span className="text-sm text-muted-foreground">{n.rotulo}</span>
        </li>
      ))}
    </ul>
  );
}

// A faixa do topo é da mesma cor do trilho: o alto da tela é uma peça só, e o conteúdo
// começa visivelmente abaixo dela.
export function Topo({
  titulo,
  descricao,
  meta,
  acoes,
  abas,
}: {
  titulo: React.ReactNode;
  descricao?: React.ReactNode;
  meta?: readonly Numero[];
  acoes?: React.ReactNode;
  /** Navegação de abas do serviço: encosta no fio de baixo. */
  abas?: React.ReactNode;
}) {
  return (
    <div className="sticky top-0 z-20 border-b bg-sidebar/95 backdrop-blur-sm">
      <Faixa className={cn("pt-6", abas ? "pb-0" : "pb-6")}>
        <div className="flex flex-wrap items-start justify-between gap-x-10 gap-y-3">
          <div className="min-w-0">
            <h1 className="font-heading text-[1.6rem] leading-tight font-semibold">{titulo}</h1>
            {descricao && <p className="mt-1 text-sm text-muted-foreground">{descricao}</p>}
          </div>
          {acoes && <div className="flex flex-wrap items-center gap-2">{acoes}</div>}
        </div>
        {meta && <Numeros itens={meta} className="mt-4" />}
        {abas && <div className="-mb-px mt-5">{abas}</div>}
      </Faixa>
    </div>
  );
}

export function Pagina({
  titulo,
  descricao,
  meta,
  acoes,
  children,
}: {
  titulo: React.ReactNode;
  descricao?: React.ReactNode;
  meta?: readonly Numero[];
  acoes?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <main className="min-w-0 flex-1 pb-20">
      <Topo titulo={titulo} descricao={descricao} meta={meta} acoes={acoes} />
      <Faixa className="pt-8">{children}</Faixa>
    </main>
  );
}

/** Conteúdo largo à esquerda e contexto à direita. Sem contexto, só o conteúdo. */
export function Colunas({
  lateral,
  lateralFixa = false,
  children,
  className,
}: {
  lateral?: React.ReactNode;
  /** A coluna de contexto acompanha a rolagem: para o que se decide olhando uma lista longa. */
  lateralFixa?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  if (!lateral) return <div className={cn("min-w-0", className)}>{children}</div>;
  return (
    <div className={cn("grid items-start gap-8 xl:grid-cols-[minmax(0,1fr)_21rem] xl:gap-12", className)}>
      <div className="flex min-w-0 flex-col gap-10">{children}</div>
      <aside
        className={cn(
          "flex min-w-0 flex-col gap-6",
          // A faixa do topo é fixa: a coluna para logo abaixo dela.
          lateralFixa && "xl:sticky xl:top-[10rem]",
        )}
      >
        {lateral}
      </aside>
    </div>
  );
}

export function Secao({
  titulo,
  descricao,
  acoes,
  children,
  className,
  rotulo,
}: {
  titulo?: React.ReactNode;
  descricao?: React.ReactNode;
  acoes?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  /** Nome acessível quando a seção não tem título visível. */
  rotulo?: string;
}) {
  return (
    <section className={cn("flex min-w-0 flex-col gap-4", className)} aria-label={rotulo}>
      {(titulo || acoes) && (
        <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-2 border-b pb-3">
          <div className="min-w-0">
            {titulo && <h2 className="font-heading text-[1.05rem] font-semibold">{titulo}</h2>}
            {descricao && <p className="mt-1 max-w-prose text-sm text-muted-foreground">{descricao}</p>}
          </div>
          {acoes && <div className="flex flex-wrap items-center gap-2">{acoes}</div>}
        </div>
      )}
      {children}
    </section>
  );
}

export function Painel({ className, children }: { className?: string; children: React.ReactNode }) {
  return <div className={cn("rounded-xl border bg-card/50 p-5", className)}>{children}</div>;
}
