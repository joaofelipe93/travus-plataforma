"use client";

// Espaço de Clientes: o cadastro que todos os serviços usam, fora de qualquer serviço.
// Resumo da carteira em cima, lista à esquerda, cliente aberto à direita. A lista e o painel
// vivem na mesma tela — trocar de cliente é navegar para /clientes/<id>, então o endereço
// continua servindo para abrir um cliente direto.

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { SheetNovoCliente } from "@/components/cadastro-sheets";
import { Pagina } from "@/components/pagina";
import { PainelCliente } from "@/components/painel-cliente";
import { ResumoCarteira } from "@/components/resumo-carteira";
import { TracoCotas } from "@/components/traco-cotas";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";

import { podeEditar, useSessao } from "@/lib/sessao";
import type { ClienteResumo, Cota, ServicoCatalogo } from "@/lib/tipos";
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
  // O catálogo de serviços vem da API (mesma lista do banco): a lista só precisa dele para
  // dizer de qual serviço o cliente está fora.
  const catalogo = useQuery({
    queryKey: ["servicos"],
    queryFn: () => api<{ servicos: ServicoCatalogo[] }>("/servicos"),
    staleTime: Infinity,
  });

  const lista = clientes.data?.clientes ?? [];

  return (
    <Pagina
      titulo="Clientes"
      descricao="O cadastro é um só para a plataforma: cada cliente entra nos serviços que você ligar para ele."
      acoes={
        editavel && (
          <Button onClick={() => setNovoAberto(true)} aria-label="Cadastrar cliente">
            Novo cliente
          </Button>
        )
      }
    >
      <div className="flex flex-col gap-8">
      <ResumoCarteira cotas={cotas.data?.cotas} clientes={clientes.data?.clientes.length} />

      {/* A lista acompanha a rolagem (o topo da página é fixo, por isso o deslocamento). */}
      <div className="grid gap-8 lg:grid-cols-[20rem_minmax(0,1fr)] lg:items-start xl:grid-cols-[23rem_minmax(0,1fr)] xl:gap-12">
        <aside className="flex flex-col gap-3 lg:sticky lg:top-[9rem]">
          <Input
            placeholder="Buscar cliente"
            value={busca}
            onChange={(e) => setBusca(e.target.value)}
            aria-label="Buscar cliente pelo nome"
          />

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
              <ul className="flex max-h-[32rem] flex-col gap-1 overflow-y-auto rounded-lg border p-1 lg:max-h-[calc(100svh-19rem)]">
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
                    <ItemCliente
                      cliente={c}
                      selecionado={c.id === selecionadoId}
                      catalogo={catalogo.data?.servicos ?? []}
                    />
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
    </Pagina>
  );
}

function ItemCliente({
  cliente,
  selecionado,
  catalogo,
}: {
  cliente: ClienteResumo;
  selecionado: boolean;
  catalogo: ServicoCatalogo[];
}) {
  // Ficar em todos os serviços é o normal e não merece tinta; o que interessa é quem está fora.
  const fora = catalogo.filter((s) => !cliente.servicos.includes(s.id));
  return (
    <Link
      href={`/clientes/${cliente.id}`}
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
        {fora.length > 0 && ` · fora de ${fora.map((s) => s.nome).join(" e ")}`}
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
    <div className="flex max-w-xl flex-col items-start gap-3 rounded-xl border border-dashed p-8">
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
