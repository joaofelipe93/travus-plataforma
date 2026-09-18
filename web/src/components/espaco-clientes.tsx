"use client";

// Espaço de Clientes: resumo da carteira em cima, lista à esquerda, cliente aberto à direita.
// A lista e o painel vivem na mesma tela — trocar de cliente é navegar para
// /canopus/clientes/<id>, então o endereço continua servindo para abrir um cliente direto.

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { SheetNovoCliente } from "@/components/cadastro-sheets";
import { PainelCliente } from "@/components/painel-cliente";
import { ResumoCarteira } from "@/components/resumo-carteira";
import { TracoCotas } from "@/components/traco-cotas";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { podeEditar, useSessao } from "@/lib/sessao";
import type { ClienteResumo, Cota } from "@/lib/tipos";
import { useAdiado } from "@/lib/use-adiado";
import { cn } from "@/lib/utils";

export function EspacoClientes({ selecionadoId }: { selecionadoId?: number }) {
  const editavel = podeEditar(useSessao().data?.usuario.perfil);
  const [busca, setBusca] = useState("");
  const buscaAdiada = useAdiado(busca.trim());
  const [novoAberto, setNovoAberto] = useState(false);

  const clientes = useQuery({
    queryKey: ["clientes", buscaAdiada],
    queryFn: () => api<{ clientes: ClienteResumo[] }>(`/clientes${buscaAdiada ? `?busca=${encodeURIComponent(buscaAdiada)}` : ""}`),
    placeholderData: keepPreviousData,
  });
  // O resumo é da carteira inteira, não do que está filtrado na lista.
  const cotas = useQuery({
    queryKey: ["cotas", ""],
    queryFn: () => api<{ cotas: Cota[] }>("/cotas"),
  });

  const lista = clientes.data?.clientes ?? [];

  return (
    <div className="flex flex-col gap-6">
      <ResumoCarteira cotas={cotas.data?.cotas} clientes={clientes.data?.clientes.length} />

      <div className="grid gap-6 lg:grid-cols-[19rem_1fr] lg:items-start">
        <aside className="flex flex-col gap-3 lg:sticky lg:top-6">
          <div className="flex gap-2">
            <Input
              className="flex-1"
              placeholder="Buscar cliente"
              value={busca}
              onChange={(e) => setBusca(e.target.value)}
              aria-label="Buscar cliente pelo nome"
            />
            {editavel && (
              <Button onClick={() => setNovoAberto(true)} aria-label="Cadastrar cliente">
                Novo
              </Button>
            )}
          </div>

          {clientes.isError ? (
            <Alert variant="destructive">
              <AlertDescription>{clientes.error.message}</AlertDescription>
            </Alert>
          ) : (
            <>
              <p className="px-1 text-sm text-muted-foreground" aria-live="polite">
                {clientes.isPending ? " " : `${lista.length} ${lista.length === 1 ? "cliente" : "clientes"}`}
                {buscaAdiada && lista.length > 0 && " encontrados"}
              </p>
              <ul className="flex max-h-[32rem] flex-col gap-1 overflow-y-auto rounded-lg border p-1 lg:max-h-[calc(100svh-14rem)]">
                {clientes.isPending &&
                  Array.from({ length: 6 }, (_, i) => (
                    <li key={i} className="px-3 py-2.5">
                      <Skeleton className="h-4 w-40" />
                      <Skeleton className="mt-2 h-1.5 w-full" />
                    </li>
                  ))}
                {clientes.isSuccess && lista.length === 0 && (
                  <li className="px-3 py-10 text-center text-sm text-muted-foreground">
                    {buscaAdiada ? (
                      <>Nada com “{buscaAdiada}”.</>
                    ) : editavel ? (
                      <>Nenhum cliente ainda. Comece pelo botão Novo.</>
                    ) : (
                      <>Nenhum cliente cadastrado.</>
                    )}
                  </li>
                )}
                {lista.map((c) => (
                  <li key={c.id}>
                    <ItemCliente cliente={c} selecionado={c.id === selecionadoId} />
                  </li>
                ))}
              </ul>
            </>
          )}
        </aside>

        <section className="min-w-0">
          {selecionadoId === undefined ? (
            <NenhumSelecionado editavel={editavel} temClientes={lista.length > 0} aoCadastrar={() => setNovoAberto(true)} />
          ) : (
            <PainelCliente id={selecionadoId} editavel={editavel} />
          )}
        </section>
      </div>

      {editavel && <SheetNovoCliente aberto={novoAberto} aoFechar={() => setNovoAberto(false)} />}
    </div>
  );
}

function ItemCliente({ cliente, selecionado }: { cliente: ClienteResumo; selecionado: boolean }) {
  return (
    <Link
      href={`/canopus/clientes/${cliente.id}`}
      aria-current={selecionado ? "true" : undefined}
      className={cn(
        "flex flex-col gap-2 rounded-md border-l-2 border-transparent px-3 py-2.5 transition-colors outline-none",
        "hover:bg-accent focus-visible:bg-accent focus-visible:ring-2 focus-visible:ring-ring",
        selecionado && "border-l-primary bg-accent",
      )}
    >
      <span className={cn("truncate text-sm", selecionado && "font-medium")}>{cliente.nome}</span>
      <TracoCotas total={cliente.total_cotas} ativas={cliente.cotas_ativas} />
      <span className="text-xs text-muted-foreground">
        {cliente.total_cotas === 0
          ? "sem cotas"
          : cliente.cotas_ativas === cliente.total_cotas
            ? `${cliente.total_cotas} cota(s)`
            : `${cliente.cotas_ativas} de ${cliente.total_cotas} ativa(s)`}
      </span>
    </Link>
  );
}

function NenhumSelecionado({
  editavel,
  temClientes,
  aoCadastrar,
}: {
  editavel: boolean;
  temClientes: boolean;
  aoCadastrar: () => void;
}) {
  return (
    <div className="flex min-h-64 flex-col items-start justify-center gap-3 rounded-xl border border-dashed p-8">
      <h2 className="font-heading text-lg">{temClientes ? "Escolha um cliente" : "A carteira começa aqui"}</h2>
      <p className="max-w-prose text-sm text-muted-foreground">
        {temClientes
          ? "Na lista ao lado: o painel mostra o contato, as cotas e os lances já registrados."
          : "Cadastre o primeiro cliente com as cotas dele; depois é daqui que saem os dry-runs e os lances."}
      </p>
      {editavel && !temClientes && <Button onClick={aoCadastrar}>Cadastrar cliente</Button>}
    </div>
  );
}
