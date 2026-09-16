"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { nomeModalidade, nomePerfil, podeEditar, tagCota, useSessao } from "@/lib/sessao";
import type { Cota } from "@/lib/tipos";

export default function PaginaNovaExecucao() {
  const sessao = useSessao();
  const router = useRouter();
  const queryClient = useQueryClient();
  const [busca, setBusca] = useState("");
  const [grupo, setGrupo] = useState("todos");
  // Guardamos as desmarcadas: por padrão, todas as cotas ativas entram.
  const [desmarcadas, setDesmarcadas] = useState<Set<number>>(new Set());

  const cotas = useQuery({
    queryKey: ["cotas", "lista", "ativa=true"],
    queryFn: () => api<{ cotas: Cota[] }>("/cotas?ativa=true"),
  });
  const todas = cotas.data?.cotas ?? [];
  const grupos = [...new Set(todas.map((c) => c.grupo))].sort();
  const opcoesGrupo = [{ value: "todos", label: "Todos os grupos" }, ...grupos.map((g) => ({ value: g, label: `Grupo ${g}` }))];

  const termo = busca.trim().toLowerCase();
  const visiveis = todas.filter(
    (c) =>
      (grupo === "todos" || c.grupo === grupo) &&
      (!termo || c.cliente_nome.toLowerCase().includes(termo) || tagCota(c).includes(termo)),
  );
  const selecionadas = todas.filter((c) => !desmarcadas.has(c.id));
  const clientes = new Set(selecionadas.map((c) => c.cliente_id)).size;

  const alternar = (ids: number[], marcar: boolean) =>
    setDesmarcadas((atual) => {
      const nova = new Set(atual);
      for (const id of ids) {
        if (marcar) nova.delete(id);
        else nova.add(id);
      }
      return nova;
    });

  const iniciar = useMutation({
    mutationFn: () => api<{ id: number }>("/execucoes", { metodo: "POST", json: { tipo: "dry_run", cota_ids: selecionadas.map((c) => c.id) } }),
    onSuccess: ({ id }) => {
      queryClient.invalidateQueries({ queryKey: ["execucoes"] });
      router.push(`/canopus/execucoes/${id}`);
    },
  });

  const perfil = sessao.data?.usuario.perfil;
  if (!podeEditar(perfil)) {
    return (
      <Alert>
        <AlertDescription>
          Seu perfil ({perfil ? nomePerfil[perfil] : "—"}) permite só acompanhar execuções. Peça a um operador ou administrador para iniciar um dry-run.
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="flex flex-col gap-5">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Novo dry-run</h1>
        <p className="text-sm text-muted-foreground">Escolha as cotas. Só cotas ativas aparecem aqui.</p>
      </div>

      <Alert>
        <AlertTitle>Nenhum lance é confirmado</AlertTitle>
        <AlertDescription>
          Para cada cota, o worker entra no Newcon, abre o credenciamento, marca &quot;2º Fixo&quot;, tira um screenshot e lê o Histórico para mostrar se a
          cota já tem lance nesta assembleia. Ele não clica em Confirmar.
        </AlertDescription>
      </Alert>

      <div className="flex flex-wrap items-center gap-2">
        <Input className="sm:max-w-xs" placeholder="Filtrar por cliente ou cota" value={busca} onChange={(e) => setBusca(e.target.value)} />
        <Select items={opcoesGrupo} value={grupo} onValueChange={(v) => setGrupo(v ?? "todos")}>
          <SelectTrigger className="w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {opcoesGrupo.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button variant="outline" size="sm" onClick={() => alternar(visiveis.map((c) => c.id), true)} disabled={!visiveis.length}>
          Marcar visíveis
        </Button>
        <Button variant="outline" size="sm" onClick={() => alternar(visiveis.map((c) => c.id), false)} disabled={!visiveis.length}>
          Desmarcar visíveis
        </Button>
      </div>

      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10" />
              <TableHead>Cliente</TableHead>
              <TableHead>Cota</TableHead>
              <TableHead>Tipo</TableHead>
              <TableHead>Modalidade</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {cotas.isPending &&
              Array.from({ length: 6 }, (_, i) => (
                <TableRow key={i}>
                  <TableCell colSpan={5}>
                    <Skeleton className="h-5 w-full" />
                  </TableCell>
                </TableRow>
              ))}
            {cotas.isSuccess && visiveis.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="py-8 text-center text-muted-foreground">
                  {todas.length ? "Nenhuma cota com esse filtro." : "Nenhuma cota ativa. Importe a planilha ou ative cotas."}
                </TableCell>
              </TableRow>
            )}
            {visiveis.map((c) => {
              const marcada = !desmarcadas.has(c.id);
              return (
                <TableRow key={c.id} className={marcada ? undefined : "text-muted-foreground"}>
                  <TableCell>
                    <input
                      type="checkbox"
                      className="size-4 accent-primary"
                      checked={marcada}
                      onChange={(e) => alternar([c.id], e.target.checked)}
                      aria-label={`Incluir a cota ${tagCota(c)}`}
                    />
                  </TableCell>
                  <TableCell>{c.cliente_nome}</TableCell>
                  <TableCell className="tabular-nums">{tagCota(c)}</TableCell>
                  <TableCell>{c.tipo_consorcio ?? "—"}</TableCell>
                  <TableCell>{nomeModalidade[c.modalidade_padrao] ?? c.modalidade_padrao}</TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>

      {iniciar.isError && (
        <Alert variant="destructive">
          <AlertDescription>{iniciar.error.message}</AlertDescription>
        </Alert>
      )}

      <div className="sticky bottom-0 -mx-4 flex flex-wrap items-center justify-between gap-3 border-t bg-background px-4 py-3">
        <p className="text-sm">
          <span className="font-medium">{selecionadas.length}</span> cota(s) de <span className="font-medium">{clientes}</span> cliente(s)
        </p>
        <Button onClick={() => iniciar.mutate()} disabled={!selecionadas.length || iniciar.isPending || iniciar.isSuccess}>
          {iniciar.isPending || iniciar.isSuccess ? "Iniciando…" : "Iniciar dry-run"}
        </Button>
      </div>
    </div>
  );
}
