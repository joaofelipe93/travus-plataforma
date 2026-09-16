"use client";

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { TabelaCotas } from "@/components/tabela-cotas";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api } from "@/lib/api";
import { podeEditar, useSessao } from "@/lib/sessao";
import type { Cota } from "@/lib/tipos";
import { useAdiado } from "@/lib/use-adiado";

const situacoes = [
  { value: "todas", label: "Todas as situações" },
  { value: "true", label: "Só ativas" },
  { value: "false", label: "Só inativas" },
];

export default function PaginaCotas() {
  const sessao = useSessao();
  const [busca, setBusca] = useState("");
  const [grupo, setGrupo] = useState("todos");
  const [situacao, setSituacao] = useState("todas");
  const buscaAdiada = useAdiado(busca.trim());

  const grupos = useQuery({
    queryKey: ["cotas", "grupos"],
    queryFn: () => api<{ grupos: string[] }>("/cotas/grupos"),
  });
  const opcoesGrupo = [{ value: "todos", label: "Todos os grupos" }, ...(grupos.data?.grupos ?? []).map((g) => ({ value: g, label: `Grupo ${g}` }))];

  const filtros = new URLSearchParams();
  if (buscaAdiada) filtros.set("busca", buscaAdiada);
  if (grupo !== "todos") filtros.set("grupo", grupo);
  if (situacao !== "todas") filtros.set("ativa", situacao);
  const consulta = filtros.toString();

  const cotas = useQuery({
    queryKey: ["cotas", "lista", consulta],
    queryFn: () => api<{ cotas: Cota[] }>(`/cotas${consulta ? `?${consulta}` : ""}`),
    placeholderData: keepPreviousData,
  });
  const lista = cotas.data?.cotas;
  const filtrando = consulta !== "";

  return (
    <div className="flex flex-col gap-4">
      <div>
        <p className="text-sm text-muted-foreground">
          {lista ? `${lista.length} cota(s)${filtrando ? " com os filtros aplicados" : ""}, ${lista.filter((c) => c.ativa).length} ativa(s)` : " "}
        </p>
      </div>

      <div className="flex flex-wrap gap-2">
        <Input className="sm:max-w-xs" placeholder="Buscar por cliente, grupo ou cota" value={busca} onChange={(e) => setBusca(e.target.value)} />
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
        <Select items={situacoes} value={situacao} onValueChange={(v) => setSituacao(v ?? "todas")}>
          <SelectTrigger className="w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {situacoes.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {cotas.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{cotas.error.message}</AlertDescription>
        </Alert>
      ) : (
        <TabelaCotas
          cotas={lista}
          carregando={cotas.isPending}
          editavel={podeEditar(sessao.data?.usuario.perfil)}
          mensagemVazia={filtrando ? "Nenhuma cota com esses filtros." : "Nenhuma cota cadastrada. Importe a planilha para começar."}
        />
      )}
    </div>
  );
}
