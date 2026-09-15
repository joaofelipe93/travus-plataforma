"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { execucaoTerminou, nomeTipo, situacaoExecucao } from "@/lib/execucoes";
import { dataHora, podeEditar, useSessao } from "@/lib/sessao";
import type { ExecucaoResumo } from "@/lib/tipos";

export default function PaginaExecucoes() {
  const sessao = useSessao();
  const execucoes = useQuery({
    queryKey: ["execucoes"],
    queryFn: () => api<{ execucoes: ExecucaoResumo[] }>("/execucoes"),
    // Enquanto houver execução ativa, atualiza sozinho.
    refetchInterval: (q) => (q.state.data?.execucoes.some((e) => !execucaoTerminou(e.status)) ? 5000 : false),
  });
  const lista = execucoes.data?.execucoes ?? [];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Execuções</h1>
          <p className="text-sm text-muted-foreground">Dry-runs no Newcon: vão até a tela de credenciamento e não confirmam lance.</p>
        </div>
        {podeEditar(sessao.data?.usuario.perfil) && (
          <Link href="/execucoes/nova" className={buttonVariants()}>
            Novo dry-run
          </Link>
        )}
      </div>

      {execucoes.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{execucoes.error.message}</AlertDescription>
        </Alert>
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-16">Nº</TableHead>
                <TableHead>Situação</TableHead>
                <TableHead>Tipo</TableHead>
                <TableHead>Cotas</TableHead>
                <TableHead>Criada por</TableHead>
                <TableHead>Quando</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {execucoes.isPending &&
                Array.from({ length: 4 }, (_, i) => (
                  <TableRow key={i}>
                    <TableCell colSpan={6}>
                      <Skeleton className="h-5 w-full" />
                    </TableCell>
                  </TableRow>
                ))}
              {execucoes.isSuccess && lista.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="py-8 text-center text-muted-foreground">
                    Nenhuma execução ainda.
                  </TableCell>
                </TableRow>
              )}
              {lista.map((e) => (
                <TableRow key={e.id}>
                  <TableCell className="tabular-nums">
                    <Link className="font-medium hover:underline" href={`/execucoes/${e.id}`}>
                      {e.id}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Badge variant={situacaoExecucao[e.status].variante}>
                      {e.cancelamento_solicitado && e.status === "em_andamento" ? "Cancelando" : situacaoExecucao[e.status].rotulo}
                    </Badge>
                  </TableCell>
                  <TableCell>{nomeTipo(e.tipo)}</TableCell>
                  <TableCell className="text-muted-foreground">
                    {e.total} no total · {e.sucesso} pronta(s) · {e.com_erro} com erro
                    {e.restantes > 0 && !execucaoTerminou(e.status) ? ` · ${e.restantes} restante(s)` : ""}
                  </TableCell>
                  <TableCell>{e.criada_por_nome}</TableCell>
                  <TableCell className="whitespace-nowrap">{dataHora(e.criada_em)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}
