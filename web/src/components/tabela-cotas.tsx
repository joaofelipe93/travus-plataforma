"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { nomeModalidade, tagCota } from "@/lib/sessao";
import type { Cota } from "@/lib/tipos";

type Props = {
  cotas: Cota[] | undefined;
  carregando: boolean;
  editavel: boolean;
  mostrarCliente?: boolean;
  mensagemVazia: string;
};

export function TabelaCotas({ cotas, carregando, editavel, mostrarCliente = true, mensagemVazia }: Props) {
  const queryClient = useQueryClient();
  const [alvo, setAlvo] = useState<Cota | null>(null);

  const alterar = useMutation({
    mutationFn: (c: Cota) => api<{ id: number; ativa: boolean }>(`/cotas/${c.id}`, { metodo: "PATCH", json: { ativa: !c.ativa } }),
    onSuccess: (r, c) => {
      toast.success(`Cota ${tagCota(c)} ${r.ativa ? "ativada" : "desativada"}`);
      setAlvo(null);
      for (const chave of ["cotas", "clientes", "cliente"]) {
        queryClient.invalidateQueries({ queryKey: [chave] });
      }
    },
    onError: (e) => toast.error(e.message),
  });

  const colunas = 7 + (mostrarCliente ? 1 : 0);

  return (
    <>
      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              {mostrarCliente && <TableHead>Cliente</TableHead>}
              <TableHead>Grupo</TableHead>
              <TableHead>Cota</TableHead>
              <TableHead>Versão</TableHead>
              <TableHead>Tipo</TableHead>
              <TableHead>Modalidade</TableHead>
              <TableHead>Situação</TableHead>
              <TableHead className="text-right">{editavel ? "Ativa" : ""}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {carregando &&
              Array.from({ length: 6 }, (_, i) => (
                <TableRow key={i}>
                  <TableCell colSpan={colunas}>
                    <Skeleton className="h-5 w-full" />
                  </TableCell>
                </TableRow>
              ))}
            {!carregando && cotas?.length === 0 && (
              <TableRow>
                <TableCell colSpan={colunas} className="py-8 text-center text-muted-foreground">
                  {mensagemVazia}
                </TableCell>
              </TableRow>
            )}
            {cotas?.map((c) => (
              <TableRow key={c.id} className={c.ativa ? undefined : "text-muted-foreground"}>
                {mostrarCliente && (
                  <TableCell className="font-medium">
                    <Link className="hover:underline" href={`/canopus/clientes/${c.cliente_id}`}>
                      {c.cliente_nome}
                    </Link>
                  </TableCell>
                )}
                <TableCell className="tabular-nums">{c.grupo}</TableCell>
                <TableCell className="tabular-nums">{c.cota}</TableCell>
                <TableCell className="tabular-nums">{c.versao}</TableCell>
                <TableCell>{c.tipo_consorcio ?? "—"}</TableCell>
                <TableCell>{nomeModalidade[c.modalidade_padrao] ?? c.modalidade_padrao}</TableCell>
                <TableCell>
                  {c.ativa ? <Badge variant="secondary">Ativa</Badge> : <Badge variant="outline">Inativa</Badge>}
                </TableCell>
                <TableCell className="text-right">
                  {editavel && (
                    <Switch
                      checked={c.ativa}
                      onCheckedChange={() => setAlvo(c)}
                      aria-label={`${c.ativa ? "Desativar" : "Ativar"} a cota ${tagCota(c)}`}
                    />
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <AlertDialog
        open={alvo !== null}
        onOpenChange={(aberto) => {
          if (!aberto && !alterar.isPending) setAlvo(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {alvo?.ativa ? "Desativar" : "Ativar"} a cota {alvo && tagCota(alvo)}?
            </AlertDialogTitle>
            <AlertDialogDescription>
              {alvo?.cliente_nome}.{" "}
              {alvo?.ativa
                ? "Cotas inativas ficam fora das execuções de lance até serem ativadas de novo."
                : "A cota volta a entrar nas execuções de lance."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={alterar.isPending}>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              variant={alvo?.ativa ? "destructive" : "default"}
              disabled={alterar.isPending}
              onClick={() => alvo && alterar.mutate(alvo)}
            >
              {alterar.isPending ? "Salvando…" : alvo?.ativa ? "Desativar" : "Ativar"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
