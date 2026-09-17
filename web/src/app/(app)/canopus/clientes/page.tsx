"use client";

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { podeEditar, useSessao } from "@/lib/sessao";
import type { ClienteResumo } from "@/lib/tipos";
import { useAdiado } from "@/lib/use-adiado";

export default function PaginaClientes() {
  const sessao = useSessao();
  const editavel = podeEditar(sessao.data?.usuario.perfil);
  const [busca, setBusca] = useState("");
  const buscaAdiada = useAdiado(busca.trim());

  const clientes = useQuery({
    queryKey: ["clientes", buscaAdiada],
    queryFn: () => api<{ clientes: ClienteResumo[] }>(`/clientes${buscaAdiada ? `?busca=${encodeURIComponent(buscaAdiada)}` : ""}`),
    placeholderData: keepPreviousData,
  });
  const lista = clientes.data?.clientes ?? [];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">{clientes.isSuccess ? `${lista.length} cliente(s)` : " "}</p>
        {editavel && (
          <Button render={<Link href="/canopus/clientes/novo" />}>Novo cliente</Button>
        )}
      </div>

      <Input className="sm:max-w-sm" placeholder="Buscar por nome" value={busca} onChange={(e) => setBusca(e.target.value)} />

      {clientes.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{clientes.error.message}</AlertDescription>
        </Alert>
      ) : (
        <div className="rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Nome</TableHead>
                <TableHead>Telefone</TableHead>
                <TableHead>E-mail</TableHead>
                <TableHead className="text-right">Cotas ativas</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {clientes.isPending &&
                Array.from({ length: 5 }, (_, i) => (
                  <TableRow key={i}>
                    <TableCell colSpan={4}>
                      <Skeleton className="h-5 w-full" />
                    </TableCell>
                  </TableRow>
                ))}
              {clientes.isSuccess && lista.length === 0 && (
                <TableRow>
                  <TableCell colSpan={4} className="py-8 text-center text-muted-foreground">
                    {buscaAdiada ? "Nenhum cliente encontrado." : "Nenhum cliente cadastrado ainda."}
                  </TableCell>
                </TableRow>
              )}
              {lista.map((c) => (
                <TableRow key={c.id}>
                  <TableCell className="font-medium">
                    <Link className="hover:underline" href={`/canopus/clientes/${c.id}`}>
                      {c.nome}
                    </Link>
                  </TableCell>
                  <TableCell className="text-muted-foreground">{c.telefone ?? "—"}</TableCell>
                  <TableCell className="text-muted-foreground">{c.email ?? "—"}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {c.cotas_ativas} de {c.total_cotas}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}
