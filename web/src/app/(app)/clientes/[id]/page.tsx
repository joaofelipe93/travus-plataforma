"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useParams } from "next/navigation";
import { TabelaCotas } from "@/components/tabela-cotas";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { dataHora, podeEditar, useSessao } from "@/lib/sessao";
import type { Cliente, Cota } from "@/lib/tipos";

function descreverOrigem(origem: string) {
  const importacao = /^importacao:(\d+)$/.exec(origem);
  return importacao ? `Importação nº ${importacao[1]}` : origem;
}

export default function PaginaCliente() {
  const { id } = useParams<{ id: string }>();
  const sessao = useSessao();
  const dados = useQuery({
    queryKey: ["cliente", id],
    queryFn: () => api<{ cliente: Cliente; cotas: Cota[] }>(`/clientes/${encodeURIComponent(id)}`),
  });
  const cliente = dados.data?.cliente;

  return (
    <div className="flex flex-col gap-6">
      <Link href="/clientes" className="text-sm text-muted-foreground hover:text-foreground">
        ← Clientes
      </Link>

      {dados.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{dados.error.message}</AlertDescription>
        </Alert>
      ) : (
        <>
          <div>
            {cliente ? <h1 className="text-xl font-semibold tracking-tight">{cliente.nome}</h1> : <Skeleton className="h-7 w-72" />}
            <dl className="mt-3 grid gap-x-8 gap-y-2 text-sm sm:grid-cols-2 lg:grid-cols-4">
              <Dado rotulo="Telefone" valor={cliente?.telefone} carregando={dados.isPending} />
              <Dado rotulo="E-mail" valor={cliente?.email} carregando={dados.isPending} />
              <Dado rotulo="Origem" valor={cliente && descreverOrigem(cliente.origem)} carregando={dados.isPending} />
              <Dado rotulo="Cadastrado em" valor={cliente && dataHora(cliente.criado_em)} carregando={dados.isPending} />
            </dl>
          </div>

          <div className="flex flex-col gap-2">
            <h2 className="font-medium">Cotas</h2>
            <TabelaCotas
              cotas={dados.data?.cotas}
              carregando={dados.isPending}
              editavel={podeEditar(sessao.data?.usuario.perfil)}
              mostrarCliente={false}
              mensagemVazia="Este cliente não tem cotas."
            />
          </div>
        </>
      )}
    </div>
  );
}

function Dado({ rotulo, valor, carregando }: { rotulo: string; valor: string | null | undefined; carregando: boolean }) {
  return (
    <div>
      <dt className="text-muted-foreground">{rotulo}</dt>
      <dd>{carregando ? <Skeleton className="mt-1 h-4 w-32" /> : valor || "—"}</dd>
    </div>
  );
}
