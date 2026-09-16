"use client";

import { useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { dataHora } from "@/lib/sessao";
import type { ImportacaoResumo, StatusImportacao } from "@/lib/tipos";
import { resumirTotais } from "./previa";

const situacao: Record<StatusImportacao, { rotulo: string; variante: "secondary" | "outline" }> = {
  aplicada: { rotulo: "Aplicada", variante: "secondary" },
  previa: { rotulo: "Prévia não aplicada", variante: "outline" },
  descartada: { rotulo: "Descartada", variante: "outline" },
};

export function Historico() {
  const importacoes = useQuery({
    queryKey: ["importacoes"],
    queryFn: () => api<{ importacoes: ImportacaoResumo[] }>("/importacoes"),
  });
  const lista = importacoes.data?.importacoes ?? [];
  if (lista.length === 0) return null;

  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-sm font-medium">Últimas importações</h2>
      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Quando</TableHead>
              <TableHead>Arquivo</TableHead>
              <TableHead>Por</TableHead>
              <TableHead>Situação</TableHead>
              <TableHead>Resultado</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {lista.map((i) => (
              <TableRow key={i.id}>
                <TableCell className="whitespace-nowrap">{dataHora(i.finalizada_em ?? i.criada_em)}</TableCell>
                <TableCell>{i.arquivo_nome}</TableCell>
                <TableCell>{i.criada_por_nome}</TableCell>
                <TableCell>
                  <Badge variant={situacao[i.status].variante}>{situacao[i.status].rotulo}</Badge>
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {i.totais ? (i.totais.erros ? `${i.totais.erros} erro(s)` : resumirTotais(i.totais)) : "—"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}
