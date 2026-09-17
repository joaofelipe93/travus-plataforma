"use client";

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { ResumoCarteira } from "@/components/resumo-carteira";
import { type Ordem, TabelaCotas } from "@/components/tabela-cotas";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api } from "@/lib/api";
import { podeEditar, useSessao } from "@/lib/sessao";
import type { Cota } from "@/lib/tipos";
import { useAdiado } from "@/lib/use-adiado";

const TODOS = "todos";

const situacoes = [
  { value: TODOS, label: "Ativas e inativas" },
  { value: "true", label: "Só ativas" },
  { value: "false", label: "Só inativas" },
];

export default function PaginaCotas() {
  const sessao = useSessao();
  const [busca, setBusca] = useState("");
  const [grupo, setGrupo] = useState(TODOS);
  const [situacao, setSituacao] = useState(TODOS);
  const [tipo, setTipo] = useState(TODOS);
  const [vendedor, setVendedor] = useState(TODOS);
  const [ordem, setOrdem] = useState<Ordem>({ campo: "cliente_nome", crescente: true });
  const buscaAdiada = useAdiado(busca.trim());

  // Busca, grupo e situação são filtros da API; tipo e vendedor saem do próprio resultado.
  const filtros = new URLSearchParams();
  if (buscaAdiada) filtros.set("busca", buscaAdiada);
  if (grupo !== TODOS) filtros.set("grupo", grupo);
  if (situacao !== TODOS) filtros.set("ativa", situacao);
  const consulta = filtros.toString();

  const cotas = useQuery({
    queryKey: ["cotas", "lista", consulta],
    queryFn: () => api<{ cotas: Cota[] }>(`/cotas${consulta ? `?${consulta}` : ""}`),
    placeholderData: keepPreviousData,
  });
  const grupos = useQuery({
    queryKey: ["cotas", "grupos"],
    queryFn: () => api<{ grupos: string[] }>("/cotas/grupos"),
  });

  const recebidas = cotas.data?.cotas;
  const opcoesTipo = useMemo(() => opcoesDe(recebidas, (c) => c.tipo_consorcio, "Todos os tipos"), [recebidas]);
  const opcoesVendedor = useMemo(() => opcoesDe(recebidas, (c) => c.vendedor, "Todos os vendedores"), [recebidas]);
  const opcoesGrupo = [
    { value: TODOS, label: "Todos os grupos" },
    ...(grupos.data?.grupos ?? []).map((g) => ({ value: g, label: `Grupo ${g}` })),
  ];

  const lista = useMemo(() => {
    if (!recebidas) return undefined;
    const filtradas = recebidas.filter(
      (c) => (tipo === TODOS || (c.tipo_consorcio ?? "") === tipo) && (vendedor === TODOS || (c.vendedor ?? "") === vendedor),
    );
    return ordenar(filtradas, ordem);
  }, [recebidas, tipo, vendedor, ordem]);

  const filtrando = consulta !== "" || tipo !== TODOS || vendedor !== TODOS;

  return (
    <div className="flex flex-col gap-6">
      <ResumoCarteira cotas={lista} filtrado={filtrando} />

      <div className="flex flex-wrap items-center gap-2">
        <Input
          className="sm:max-w-xs"
          placeholder="Buscar por cliente, grupo ou cota"
          value={busca}
          onChange={(e) => setBusca(e.target.value)}
          aria-label="Buscar cotas"
        />
        <Filtro rotulo="Grupo" opcoes={opcoesGrupo} valor={grupo} aoMudar={setGrupo} />
        <Filtro rotulo="Tipo de consórcio" opcoes={opcoesTipo} valor={tipo} aoMudar={setTipo} />
        <Filtro rotulo="Vendedor" opcoes={opcoesVendedor} valor={vendedor} aoMudar={setVendedor} />
        <Filtro rotulo="Situação" opcoes={situacoes} valor={situacao} aoMudar={setSituacao} />
        {filtrando && (
          <Button
            variant="ghost"
            onClick={() => {
              setBusca("");
              setGrupo(TODOS);
              setSituacao(TODOS);
              setTipo(TODOS);
              setVendedor(TODOS);
            }}
          >
            Limpar filtros
          </Button>
        )}
        <p className="ml-auto text-sm text-muted-foreground" aria-live="polite">
          {lista ? `${lista.length} de ${recebidas?.length ?? 0} cota(s)` : " "}
        </p>
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
          ordem={ordem}
          aoOrdenar={(campo) => setOrdem((o) => ({ campo, crescente: o.campo === campo ? !o.crescente : true }))}
          mensagemVazia={
            filtrando
              ? "Nenhuma cota com esses filtros."
              : "Nenhuma cota cadastrada. Cadastre um cliente com as cotas dele para começar."
          }
        />
      )}
    </div>
  );
}

function Filtro({
  rotulo,
  opcoes,
  valor,
  aoMudar,
}: {
  rotulo: string;
  opcoes: { value: string; label: string }[];
  valor: string;
  aoMudar: (v: string) => void;
}) {
  if (opcoes.length <= 1) return null;
  return (
    <Select items={opcoes} value={valor} onValueChange={(v) => aoMudar(v ?? TODOS)}>
      <SelectTrigger className="w-48" aria-label={rotulo}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {opcoes.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/** Opções de um filtro tiradas do que o resultado tem: filtro sem opção não aparece. */
function opcoesDe(cotas: Cota[] | undefined, campo: (c: Cota) => string | null, rotuloTodos: string) {
  const valores = [...new Set((cotas ?? []).map((c) => campo(c)?.trim()).filter((v): v is string => !!v))].sort((a, b) =>
    a.localeCompare(b, "pt-BR"),
  );
  return [{ value: TODOS, label: rotuloTodos }, ...valores.map((v) => ({ value: v, label: v }))];
}

function ordenar(cotas: Cota[], { campo, crescente }: Ordem) {
  const sinal = crescente ? 1 : -1;
  return [...cotas].sort((a, b) => {
    if (campo === "ativa") return (Number(b.ativa) - Number(a.ativa)) * sinal;
    const va = (a[campo] ?? "").toString();
    const vb = (b[campo] ?? "").toString();
    // Empate pelo par grupo+cota para a lista nunca "dançar" entre atualizações.
    return (va.localeCompare(vb, "pt-BR") || (a.grupo + a.cota).localeCompare(b.grupo + b.cota)) * sinal;
  });
}
