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
import { desfazer, pausarConsultasDeCota, trocarCotaNoCache } from "@/lib/cache-cadastro";
import { nomeModalidade, tagCota } from "@/lib/sessao";
import type { Cota } from "@/lib/tipos";

type Props = {
  cotas: Cota[] | undefined;
  carregando: boolean;
  editavel: boolean;
  mostrarCliente?: boolean;
  mensagemVazia: string;
  /** Botões do CRM (editar, excluir) numa coluna à direita, quando a tela oferece. */
  acoes?: (cota: Cota) => React.ReactNode;
  /** Com aoOrdenar, os cabeçalhos viram botões de ordenação. */
  ordem?: Ordem;
  aoOrdenar?: (campo: CampoOrdem) => void;
};

export type CampoOrdem = "cliente_nome" | "grupo" | "cota" | "tipo_consorcio" | "modalidade_padrao" | "ativa";
export type Ordem = { campo: CampoOrdem; crescente: boolean };

export function TabelaCotas({ cotas, carregando, editavel, mostrarCliente = true, mensagemVazia, acoes, ordem, aoOrdenar }: Props) {
  const queryClient = useQueryClient();
  const [alvo, setAlvo] = useState<Cota | null>(null);

  const alterar = useMutation({
    mutationFn: (c: Cota) => api<{ id: number; ativa: boolean }>(`/cotas/${c.id}`, { metodo: "PATCH", json: { ativa: !c.ativa } }),
    // A linha muda na hora; se a API recusar, volta ao que era.
    onMutate: async (c) => {
      setAlvo(null);
      const anterior = await pausarConsultasDeCota(queryClient);
      trocarCotaNoCache(queryClient, c.id, (atual) => ({ ...atual, ativa: !c.ativa }));
      return { anterior };
    },
    onSuccess: (r, c) => toast.success(`Cota ${tagCota(c)} ${r.ativa ? "ativada" : "desativada"}`),
    onError: (e, _c, contexto) => {
      desfazer(queryClient, contexto?.anterior);
      toast.error(e.message);
    },
    onSettled: () => {
      for (const chave of ["cotas", "clientes", "cliente"]) {
        queryClient.invalidateQueries({ queryKey: [chave] });
      }
    },
  });

  const colunas = 7 + (mostrarCliente ? 1 : 0) + (acoes ? 1 : 0);

  return (
    <>
      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              {mostrarCliente && (
                <Coluna campo="cliente_nome" ordem={ordem} aoOrdenar={aoOrdenar}>
                  Cliente
                </Coluna>
              )}
              <Coluna campo="grupo" ordem={ordem} aoOrdenar={aoOrdenar}>
                Grupo
              </Coluna>
              <Coluna campo="cota" ordem={ordem} aoOrdenar={aoOrdenar}>
                Cota
              </Coluna>
              <TableHead>Versão</TableHead>
              <Coluna campo="tipo_consorcio" ordem={ordem} aoOrdenar={aoOrdenar}>
                Tipo
              </Coluna>
              <Coluna campo="modalidade_padrao" ordem={ordem} aoOrdenar={aoOrdenar}>
                Modalidade
              </Coluna>
              <Coluna campo="ativa" ordem={ordem} aoOrdenar={aoOrdenar}>
                Situação
              </Coluna>
              <TableHead className="text-right">{editavel ? "Ativa" : ""}</TableHead>
              {acoes && <TableHead />}
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
                {acoes && <TableCell className="text-right whitespace-nowrap">{acoes(c)}</TableCell>}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      <AlertDialog
        open={alvo !== null}
        onOpenChange={(aberto) => {
          if (!aberto) setAlvo(null);
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
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction variant={alvo?.ativa ? "destructive" : "default"} onClick={() => alvo && alterar.mutate(alvo)}>
              {alvo?.ativa ? "Desativar" : "Ativar"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

/** Coluna que ordena ao ser clicada. Sem aoOrdenar, é um cabeçalho comum. */
function Coluna({
  campo,
  ordem,
  aoOrdenar,
  className,
  children,
}: {
  campo: CampoOrdem;
  ordem?: Ordem;
  aoOrdenar?: (campo: CampoOrdem) => void;
  className?: string;
  children: React.ReactNode;
}) {
  if (!aoOrdenar) {
    return <TableHead className={className}>{children}</TableHead>;
  }
  const ativa = ordem?.campo === campo;
  return (
    <TableHead className={className} aria-sort={ativa ? (ordem.crescente ? "ascending" : "descending") : "none"}>
      <button
        type="button"
        onClick={() => aoOrdenar(campo)}
        className="-mx-1 inline-flex items-center gap-1 rounded px-1 transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
      >
        {children}
        <span aria-hidden className={ativa ? "text-foreground" : "text-muted-foreground/40"}>
          {ativa && !ordem.crescente ? "\u2193" : "\u2191"}
        </span>
      </button>
    </TableHead>
  );
}
