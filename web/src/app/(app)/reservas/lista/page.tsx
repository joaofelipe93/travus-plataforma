"use client";

import { useQuery } from "@tanstack/react-query";
import { cn } from "cn";
import { RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { CartaoReserva } from "@/components/cartao-reserva";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api } from "@/lib/api";
import { canal as canalDe, diaLocal, haQuanto, momento, normalizarBusca, reais, somarDias, textoDeBusca } from "@/lib/reservas";
import { podeEditar, useSessao } from "@/lib/sessao";
import type { Reserva, RespostaReservas } from "@/lib/tipos";

// Reservas que chegaram pelo webhook do PMS, em cartões. A lista se atualiza sozinha a cada
// 15 s; o que chega enquanto a tela está aberta ganha destaque e um aviso. Nome e telefone de
// hóspedes: admin e operador (quem decide é a API).

const INTERVALO_MS = 15_000;
const DESTAQUE_MS = 20_000;
const TODOS = "todos";

type Visao = "proximas" | "hoje" | "canceladas" | "todas";

const VISOES: { id: Visao; rotulo: string }[] = [
  { id: "proximas", rotulo: "Próximas" },
  { id: "hoje", rotulo: "Hoje" },
  { id: "canceladas", rotulo: "Canceladas" },
  { id: "todas", rotulo: "Todas" },
];

export default function PaginaReservas() {
  const sessao = useSessao();
  const pode = podeEditar(sessao.data?.usuario.perfil);

  const { novas, conferir } = useNovas();
  const consulta = useQuery({
    queryKey: ["reservas"],
    queryFn: async () => {
      const resposta = await api<RespostaReservas>("/reservas");
      conferir(resposta.reservas);
      return resposta;
    },
    refetchInterval: INTERVALO_MS,
    enabled: pode,
  });

  const [visao, setVisao] = useState<Visao>("proximas");
  const [busca, setBusca] = useState("");
  const [imovel, setImovel] = useState(TODOS);
  const [canal, setCanal] = useState(TODOS);
  const [hoje, setHoje] = useState(() => diaLocal());
  const [agora, setAgora] = useState(() => Date.now());

  // Relógio da tela: o "há 3 min" anda e a virada do dia muda o que é "hoje".
  useEffect(() => {
    const t = setInterval(() => {
      setAgora(Date.now());
      setHoje(diaLocal());
    }, 30_000);
    return () => clearInterval(t);
  }, []);

  const todas = consulta.data?.reservas;
  const opcoesImovel = useMemo(() => opcoes(todas, (r) => r.imovel, "Todos os imóveis"), [todas]);
  const opcoesCanal = useMemo(
    () => opcoes(todas, (r) => r.canal, "Todos os canais", (c) => canalDe(c)?.nome ?? c),
    [todas],
  );

  const termo = normalizarBusca(busca);
  const filtradas = useMemo(() => {
    if (!todas) return undefined;
    return todas.filter(
      (r) =>
        (imovel === TODOS || r.imovel === imovel) &&
        (canal === TODOS || r.canal === canal) &&
        (termo === "" || textoDeBusca(r).includes(termo)),
    );
  }, [todas, imovel, canal, termo]);

  const porVisao = useMemo(() => {
    if (!filtradas) return undefined;
    return {
      proximas: ordenarPorEntrada(filtradas.filter((r) => ["chega_hoje", "hospedado", "sai_hoje", "futura"].includes(momento(r, hoje)))),
      hoje: ordenarPorEntrada(filtradas.filter((r) => ["chega_hoje", "sai_hoje", "hospedado"].includes(momento(r, hoje)))),
      canceladas: filtradas.filter((r) => r.situacao === "cancelada"),
      todas: filtradas,
    } satisfies Record<Visao, Reserva[]>;
  }, [filtradas, hoje]);

  if (!pode) {
    return (
      <Alert>
        <AlertTitle>Sem acesso</AlertTitle>
        <AlertDescription>As reservas têm dados de hóspedes: só operadores e administradores veem.</AlertDescription>
      </Alert>
    );
  }

  const lista = porVisao?.[visao];
  const filtrando = termo !== "" || imovel !== TODOS || canal !== TODOS;

  return (
    <div className="flex flex-col gap-6">
      <ResumoReservas reservas={filtradas} hoje={hoje} filtrado={filtrando} />

      <div className="flex flex-wrap items-center gap-2">
        <div role="tablist" aria-label="Quais reservas mostrar" className="flex rounded-lg bg-accent/40 p-1">
          {VISOES.map((v) => (
            <button
              key={v.id}
              role="tab"
              type="button"
              aria-selected={visao === v.id}
              onClick={() => setVisao(v.id)}
              className={cn(
                "flex items-center gap-2 rounded-md px-3 py-1.5 text-sm transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring",
                visao === v.id ? "bg-card text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {v.rotulo}
              <span className="tabular-nums text-xs text-muted-foreground">{porVisao?.[v.id].length ?? "–"}</span>
            </button>
          ))}
        </div>

        <Input
          className="sm:max-w-64"
          placeholder="Buscar hóspede, imóvel ou telefone"
          value={busca}
          onChange={(e) => setBusca(e.target.value)}
          aria-label="Buscar reservas"
        />
        <Filtro rotulo="Imóvel" opcoes={opcoesImovel} valor={imovel} aoMudar={setImovel} />
        <Filtro rotulo="Canal" opcoes={opcoesCanal} valor={canal} aoMudar={setCanal} />
        {filtrando && (
          <Button
            variant="ghost"
            onClick={() => {
              setBusca("");
              setImovel(TODOS);
              setCanal(TODOS);
            }}
          >
            Limpar filtros
          </Button>
        )}

        <p className="ml-auto flex items-center gap-2 text-xs text-muted-foreground" aria-live="polite">
          <span className="relative flex size-2" aria-hidden>
            {!consulta.isError && (
              <span className="absolute inline-flex size-full rounded-full bg-sucesso opacity-60 motion-safe:animate-ping" />
            )}
            <span className={cn("relative inline-flex size-2 rounded-full", consulta.isError ? "bg-destructive" : "bg-sucesso")} />
          </span>
          {consulta.dataUpdatedAt ? `atualizado ${haQuanto(new Date(consulta.dataUpdatedAt).toISOString(), agora)}` : "carregando"}
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Atualizar agora"
            onClick={() => consulta.refetch()}
            disabled={consulta.isFetching}
          >
            <RefreshCw className={cn("size-3.5", consulta.isFetching && "motion-safe:animate-spin")} />
          </Button>
        </p>
      </div>

      {consulta.isError && (
        <Alert variant="destructive">
          <AlertDescription>
            {consulta.error.message}
            {todas && " Mostrando o que foi lido por último."}
          </AlertDescription>
        </Alert>
      )}
      {consulta.data?.limite_atingido && (
        <p className="text-sm text-muted-foreground">Mostrando só as reservas mais recentes.</p>
      )}

      {!lista ? (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-72 animate-pulse rounded-xl bg-card/60" />
          ))}
        </div>
      ) : lista.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border py-16 text-center text-sm text-muted-foreground">
          {mensagemVazia(visao, filtrando, (todas?.length ?? 0) > 0)}
        </p>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {lista.map((r) => (
            <CartaoReserva key={r.chave} reserva={r} hoje={hoje} nova={novas.has(r.chave)} />
          ))}
        </div>
      )}
    </div>
  );
}

// Reservas que apareceram (ou mudaram, como um cancelamento) depois da primeira leitura:
// destaque por alguns segundos e um aviso no canto. A conferência roda a cada leitura da API.
function useNovas() {
  const vistas = useRef<Map<string, string> | null>(null);
  const [novas, setNovas] = useState<ReadonlySet<string>>(new Set());

  const conferir = useCallback((reservas: Reserva[]) => {
    if (vistas.current === null) {
      vistas.current = new Map(reservas.map((r) => [r.chave, r.atualizada_em]));
      return;
    }
    const chegaram: Reserva[] = [];
    for (const r of reservas) {
      if (vistas.current.get(r.chave) !== r.atualizada_em) chegaram.push(r);
      vistas.current.set(r.chave, r.atualizada_em);
    }
    if (chegaram.length === 0) return;

    for (const r of chegaram.slice(0, 3)) {
      const quem = [r.hospede, r.imovel].filter(Boolean).join(" · ");
      if (r.situacao === "cancelada") toast.warning(`Reserva cancelada${quem ? `: ${quem}` : ""}`);
      else toast.success(`Nova reserva${quem ? `: ${quem}` : ""}`);
    }
    const chaves = chegaram.map((r) => r.chave);
    setNovas((atual) => new Set([...atual, ...chaves]));
    setTimeout(() => {
      setNovas((atual) => new Set([...atual].filter((c) => !chaves.includes(c))));
    }, DESTAQUE_MS);
  }, []);

  return { novas, conferir };
}

function ResumoReservas({ reservas, hoje, filtrado }: { reservas: Reserva[] | undefined; hoje: string; filtrado: boolean }) {
  if (!reservas) {
    return <div className="h-24 animate-pulse rounded-xl bg-card/60" />;
  }
  const semana = somarDias(hoje, 7);
  const mes = hoje.slice(0, 7);
  const confirmadas = reservas.filter((r) => r.situacao === "confirmada");

  const chegamHoje = confirmadas.filter((r) => r.check_in === hoje).length;
  const hospedados = confirmadas.filter((r) => ["hospedado", "sai_hoje"].includes(momento(r, hoje))).length;
  const proximos7 = confirmadas.filter((r) => r.check_in && r.check_in > hoje && r.check_in <= semana).length;
  const doMes = confirmadas.filter((r) => r.check_in?.startsWith(mes));
  const valorMes = doMes.reduce((soma, r) => soma + (r.valor_centavos ?? 0), 0);
  const noitesMes = doMes.reduce((soma, r) => soma + (r.noites ?? 0), 0);
  const canceladasMes = reservas.filter((r) => r.situacao === "cancelada" && r.cancelada_em?.startsWith(mes)).length;
  const nomeMes = new Date(`${mes}-01T12:00:00`).toLocaleDateString("pt-BR", { month: "long" });

  const numeros = [
    { valor: String(chegamHoje), rotulo: chegamHoje === 1 ? "chega hoje" : "chegam hoje", destaque: chegamHoje > 0 },
    { valor: String(hospedados), rotulo: hospedados === 1 ? "hospedado agora" : "hospedados agora" },
    { valor: String(proximos7), rotulo: "chegadas em 7 dias" },
    {
      valor: reais(valorMes),
      rotulo: `em ${nomeMes}`,
      detalhe: `${doMes.length} ${doMes.length === 1 ? "reserva" : "reservas"} · ${noitesMes} ${noitesMes === 1 ? "noite" : "noites"}${
        canceladasMes ? ` · ${canceladasMes} ${canceladasMes === 1 ? "cancelada" : "canceladas"}` : ""
      }`,
    },
  ];

  return (
    <section
      className="grid grid-cols-2 gap-3 border-b border-border/60 pb-6 lg:grid-cols-4"
      aria-label={filtrado ? "Resumo do que está filtrado" : "Resumo das reservas"}
    >
      {numeros.map((n) => (
        <div key={n.rotulo} className="rounded-xl border border-border/60 bg-card/60 px-4 py-3">
          <p className={cn("font-heading text-3xl leading-none tracking-tight tabular-nums", n.destaque && "text-sucesso")}>{n.valor}</p>
          <p className="mt-2 text-sm text-muted-foreground">{n.rotulo}</p>
          {n.detalhe && <p className="mt-0.5 text-xs text-muted-foreground">{n.detalhe}</p>}
        </div>
      ))}
    </section>
  );
}

function ordenarPorEntrada(reservas: Reserva[]) {
  return [...reservas].sort((a, b) => (a.check_in ?? "9999").localeCompare(b.check_in ?? "9999"));
}

function opcoes(
  reservas: Reserva[] | undefined,
  campo: (r: Reserva) => string | null,
  todos: string,
  rotulo: (v: string) => string = (v) => v,
) {
  const valores = [...new Set((reservas ?? []).map(campo).filter((v): v is string => !!v))].sort((a, b) => a.localeCompare(b, "pt-BR"));
  return [{ value: TODOS, label: todos }, ...valores.map((v) => ({ value: v, label: rotulo(v) }))];
}

function mensagemVazia(visao: Visao, filtrando: boolean, temAlguma: boolean) {
  if (filtrando) return "Nenhuma reserva com esses filtros.";
  if (!temAlguma) return "Nenhuma reserva recebida ainda. Elas aparecem aqui assim que o PMS avisar.";
  switch (visao) {
    case "proximas":
      return "Nenhuma reserva por vir.";
    case "hoje":
      return "Ninguém chegando, saindo ou hospedado hoje.";
    case "canceladas":
      return "Nenhuma reserva cancelada.";
    default:
      return "Nenhuma reserva.";
  }
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
  if (opcoes.length <= 2) return null;
  return (
    <Select items={opcoes} value={valor} onValueChange={(v) => aoMudar(v ?? TODOS)}>
      <SelectTrigger className="w-44" aria-label={rotulo}>
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
