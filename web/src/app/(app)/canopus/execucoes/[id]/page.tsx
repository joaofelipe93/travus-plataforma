"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";
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
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Comprovante } from "@/components/comprovante";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { execucaoTerminou, hora, nomeTipo, situacaoCota, situacaoExecucao } from "@/lib/execucoes";
import { dataHora, podeEditar, tagCota, useSessao } from "@/lib/sessao";
import type { CotaExecucao, DetalheExecucao, EventoExecucao, LanceExecucao } from "@/lib/tipos";
import { cn } from "@/lib/utils";

const MAX_EVENTOS = 1000;

export default function PaginaExecucao() {
  const { id } = useParams<{ id: string }>();
  const sessao = useSessao();
  const queryClient = useQueryClient();
  const [eventos, setEventos] = useState<EventoExecucao[]>([]);
  const [confirmarCancelamento, setConfirmarCancelamento] = useState(false);

  const detalhe = useQuery({
    queryKey: ["execucao", id],
    queryFn: () => api<DetalheExecucao>(`/execucoes/${encodeURIComponent(id)}`),
    // Rede de segurança: o SSE é quem atualiza de verdade. Depois do fim, acompanha o envio ao Drive.
    refetchInterval: (q) => {
      const d = q.state.data;
      if (!d) return false;
      if (!execucaoTerminou(d.execucao.status)) return 15000;
      return d.lances?.some((l) => l.drive_status === "pendente" || l.drive_status === "enviando") ? 20000 : false;
    },
  });
  const terminou = detalhe.data ? execucaoTerminou(detalhe.data.execucao.status) : false;

  // Progresso ao vivo: cada evento chega pelo SSE e dispara uma nova leitura do detalhe.
  useEffect(() => {
    const fonte = new EventSource(`/api/execucoes/${encodeURIComponent(id)}/eventos`);
    let atualizacao: ReturnType<typeof setTimeout> | undefined;
    const atualizarDetalhe = () => {
      clearTimeout(atualizacao);
      atualizacao = setTimeout(() => queryClient.invalidateQueries({ queryKey: ["execucao", id] }), 300);
    };
    fonte.addEventListener("evento", (m) => {
      const evento = JSON.parse((m as MessageEvent).data) as EventoExecucao;
      setEventos((atual) => (atual.some((e) => e.id === evento.id) ? atual : [...atual, evento].slice(-MAX_EVENTOS)));
      atualizarDetalhe();
    });
    fonte.addEventListener("fim", () => {
      fonte.close();
      queryClient.invalidateQueries({ queryKey: ["execucao", id] });
      queryClient.invalidateQueries({ queryKey: ["execucoes"] });
    });
    return () => {
      clearTimeout(atualizacao);
      fonte.close();
    };
  }, [id, queryClient]);

  const cancelar = useMutation({
    mutationFn: () => api<{ status: string }>(`/execucoes/${encodeURIComponent(id)}/cancelar`, { metodo: "POST" }),
    onSuccess: (r) => {
      setConfirmarCancelamento(false);
      toast(r.status === "cancelada" ? "Execução cancelada" : "Cancelamento pedido: o worker termina a cota atual e para");
      queryClient.invalidateQueries({ queryKey: ["execucao", id] });
    },
    onError: (e) => {
      setConfirmarCancelamento(false);
      toast.error(e.message);
    },
  });

  if (detalhe.isError) {
    return (
      <div className="flex flex-col gap-4">
        <Link href="/canopus/execucoes" className="text-sm text-muted-foreground hover:text-foreground">
          ← Execuções
        </Link>
        <Alert variant="destructive">
          <AlertDescription>{detalhe.error.message}</AlertDescription>
        </Alert>
      </div>
    );
  }
  if (!detalhe.data) {
    return <Skeleton className="h-40 w-full" />;
  }

  const { execucao, totais, cotas } = detalhe.data;
  const lancesPorCota = new Map((detalhe.data.lances ?? []).map((l) => [l.execucao_cota_id, l]));
  const processadas = cotas.filter((c) => c.status !== "pendente" && c.status !== "em_andamento" && c.status !== "confirmacao_iniciada").length;
  const percentual = cotas.length ? Math.round((processadas / cotas.length) * 100) : 0;
  const comLanceNaAssembleia = cotas.filter((c) => (c.detalhes.lances_nesta_assembleia ?? 0) > 0).length;
  const perfil = sessao.data?.usuario.perfil;
  const podeCancelar = podeEditar(perfil) && !terminou && !execucao.cancelamento_solicitado;
  const podeRevisar =
    perfil === "admin" &&
    execucao.tipo === "dry_run" &&
    (execucao.status === "concluida" || execucao.status === "concluida_com_erros") &&
    (totais.verificada ?? 0) > 0;
  const mostrarComprovante = execucao.tipo !== "dry_run";

  return (
    <div className="flex flex-col gap-5">
      <Link href="/canopus/execucoes" className="text-sm text-muted-foreground hover:text-foreground">
        ← Execuções
      </Link>

      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-xl font-semibold tracking-tight">
              {nomeTipo(execucao.tipo)} nº {execucao.id}
            </h1>
            <Badge variant={situacaoExecucao[execucao.status].variante}>
              {execucao.cancelamento_solicitado && !terminou ? "Cancelando" : situacaoExecucao[execucao.status].rotulo}
            </Badge>
          </div>
          <p className="text-sm text-muted-foreground">
            {execucao.tipo === "real" && execucao.aprovada_por_nome ? `Aprovada por ${execucao.aprovada_por_nome}` : `Criada por ${execucao.criada_por_nome}`} em{" "}
            {dataHora(execucao.criada_em)}
            {execucao.dry_run_origem_id && (
              <>
                {" · a partir do "}
                <Link className="underline" href={`/canopus/execucoes/${execucao.dry_run_origem_id}`}>
                  dry-run nº {execucao.dry_run_origem_id}
                </Link>
              </>
            )}
            {execucao.finalizada_em && ` · terminou em ${dataHora(execucao.finalizada_em)}`}
            {execucao.status === "na_fila" && execucao.posicao_fila !== null && ` · posição na fila: ${execucao.posicao_fila}`}
          </p>
        </div>
        <div className="flex gap-2">
          {podeRevisar && (
            <Link href={`/canopus/execucoes/${execucao.id}/revisao`} className={buttonVariants({ variant: "destructive" })}>
              Revisar para lance real
            </Link>
          )}
          {podeCancelar && (
            <Button variant="outline" onClick={() => setConfirmarCancelamento(true)} disabled={cancelar.isPending}>
              Cancelar execução
            </Button>
          )}
        </div>
      </div>

      {execucao.tipo === "dry_run" && (
        <p className="rounded-md border border-dashed px-3 py-2 text-sm text-muted-foreground">Dry-run: nenhum lance foi ou será confirmado nesta execução.</p>
      )}
      {execucao.tipo === "reimpressao" && (
        <p className="rounded-md border border-dashed px-3 py-2 text-sm text-muted-foreground">Reimpressão de comprovante pelo Histórico: não registra lance.</p>
      )}
      {execucao.tipo === "real" && (
        <Alert variant="destructive">
          <AlertTitle>Execução real</AlertTitle>
          <AlertDescription>O worker clica em Confirmar no Newcon para as cotas abaixo. Cota que passou pela confirmação nunca é repetida automaticamente.</AlertDescription>
        </Alert>
      )}
      {execucao.erro && (
        <Alert variant="destructive">
          <AlertTitle>A execução foi interrompida</AlertTitle>
          <AlertDescription>{execucao.erro}</AlertDescription>
        </Alert>
      )}
      {execucao.cancelamento_solicitado && !terminou && (
        <Alert>
          <AlertDescription>
            Cancelamento pedido{execucao.cancelada_por_nome ? ` por ${execucao.cancelada_por_nome}` : ""}: o worker termina a cota atual e para.
          </AlertDescription>
        </Alert>
      )}

      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap justify-between gap-2 text-sm">
          <span>
            {processadas} de {cotas.length} cota(s) processada(s)
          </span>
          <span className="text-muted-foreground">
            {(totais.verificada ?? 0) + (totais.confirmada ?? 0) + (totais.reimpressa ?? 0)} ok ·{" "}
            {(totais.erro_antes_confirmar ?? 0) + (totais.erro_apos_confirmar ?? 0)} com erro
            {comLanceNaAssembleia > 0 && execucao.tipo === "dry_run" && ` · ${comLanceNaAssembleia} já com lance nesta assembleia`}
          </span>
        </div>
        <div className="h-2 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={percentual} aria-valuemin={0} aria-valuemax={100}>
          <div className="h-full bg-primary transition-all" style={{ width: `${percentual}%` }} />
        </div>
      </div>

      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10">#</TableHead>
              <TableHead>Cliente</TableHead>
              <TableHead>Cota</TableHead>
              <TableHead>Situação</TableHead>
              <TableHead>Assembleia</TableHead>
              <TableHead>Resultado</TableHead>
              {mostrarComprovante && <TableHead>Comprovante</TableHead>}
              <TableHead className="text-right">Screenshot</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {cotas.map((c) => (
              <LinhaCota key={c.id} cota={c} lance={lancesPorCota.get(c.id)} mostrarComprovante={mostrarComprovante} editavel={podeEditar(perfil)} />
            ))}
          </TableBody>
        </Table>
      </div>

      <section className="flex flex-col gap-2">
        <h2 className="text-sm font-medium">Log da execução</h2>
        <Log eventos={eventos} />
      </section>

      <AlertDialog open={confirmarCancelamento} onOpenChange={(aberto) => !cancelar.isPending && setConfirmarCancelamento(aberto)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Cancelar a execução nº {execucao.id}?</AlertDialogTitle>
            <AlertDialogDescription>
              {execucao.status === "na_fila" ? "Ela ainda não começou e sai da fila." : "O worker termina a cota que está processando e não começa as próximas."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={cancelar.isPending}>Voltar</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={cancelar.isPending} onClick={() => cancelar.mutate()}>
              {cancelar.isPending ? "Cancelando…" : "Cancelar execução"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function LinhaCota({
  cota: c,
  lance,
  mostrarComprovante,
  editavel,
}: {
  cota: CotaExecucao;
  lance?: LanceExecucao;
  mostrarComprovante: boolean;
  editavel: boolean;
}) {
  const d = c.detalhes;
  const lances = d.lances_nesta_assembleia ?? 0;
  const erroAposConfirmar = c.status === "erro_apos_confirmar";
  return (
    <TableRow className={cn(erroAposConfirmar && "bg-destructive/10", (c.status === "em_andamento" || c.status === "confirmacao_iniciada") && "bg-muted/60")}>
      <TableCell className="tabular-nums text-muted-foreground">{c.ordem}</TableCell>
      <TableCell>{c.cliente_nome}</TableCell>
      <TableCell className="whitespace-nowrap tabular-nums">{tagCota(c)}</TableCell>
      <TableCell>
        <Badge variant={situacaoCota[c.status].variante}>{situacaoCota[c.status].rotulo}</Badge>
      </TableCell>
      <TableCell className="whitespace-nowrap text-muted-foreground">
        {d.assembleia_data ? `${d.assembleia_data}${d.assembleia_numero ? ` (nº ${d.assembleia_numero})` : ""}` : (c.assembleia_aprovada ?? "—")}
      </TableCell>
      <TableCell className="max-w-md whitespace-normal">
        {erroAposConfirmar && <p className="font-medium text-destructive">O lance pode ter sido registrado: confira no Histórico do Newcon.</p>}
        {c.protocolo && (c.status === "confirmada" || c.status === "reimpressa" || erroAposConfirmar) && (
          <p>
            Protocolo <span className="font-medium tabular-nums">{c.protocolo}</span>
            {lance?.parcelas_em_atraso && <Badge variant="outline" className="ml-2">Parcelas em atraso</Badge>}
          </p>
        )}
        {c.erro && (
          <p className={cn(c.erro_tipo === "inesperado" && "text-destructive")}>
            {c.erro_tipo === "inesperado" ? "Erro inesperado: " : ""}
            {c.erro}
          </p>
        )}
        {lances > 0 && c.status !== "confirmada" && (
          <p className="text-amber-700 dark:text-amber-400">
            Já tem {lances} lance(s) nesta assembleia: {d.lances?.map((l) => `${l.protocolo} (${l.modalidade ?? "?"})`).join(", ")}
          </p>
        )}
        {c.permitir_lance_existente && <p className="text-xs text-muted-foreground">Autorizado a registrar mesmo com lance existente.</p>}
        {c.status === "verificada" && lances === 0 && d.historico_lido && <p className="text-muted-foreground">Sem lance nesta assembleia.</p>}
        {c.status === "verificada" && d.historico_lido === false && <p className="text-muted-foreground">Histórico não pôde ser lido.</p>}
        {d.percentual_segundo_fixo && c.status === "verificada" && <p className="text-xs text-muted-foreground">2º Lance Fixo: {d.percentual_segundo_fixo}%</p>}
      </TableCell>
      {mostrarComprovante && (
        <TableCell>
          {lance ? <Comprovante lance={lance} editavel={editavel} /> : <span className="text-muted-foreground">—</span>}
        </TableCell>
      )}
      <TableCell className="text-right">
        {c.screenshot_id ? (
          <a href={`/api/arquivos/${c.screenshot_id}`} target="_blank" rel="noopener noreferrer" className="inline-block" title="Abrir screenshot">
            {/* <img> e não next/image: o arquivo exige o cookie de sessão e não passa pelo otimizador. */}
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src={`/api/arquivos/${c.screenshot_id}`} alt={`Screenshot da cota ${tagCota(c)}`} className="h-12 w-20 rounded border object-cover object-top" loading="lazy" />
          </a>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </TableCell>
    </TableRow>
  );
}

const corNivel: Record<EventoExecucao["nivel"], string> = {
  info: "text-muted-foreground",
  ok: "text-emerald-700 dark:text-emerald-400",
  aviso: "text-amber-700 dark:text-amber-400",
  erro: "text-destructive",
};

function Log({ eventos }: { eventos: EventoExecucao[] }) {
  const fim = useRef<HTMLDivElement>(null);
  useEffect(() => {
    fim.current?.scrollIntoView({ block: "nearest" });
  }, [eventos.length]);

  return (
    <div className="max-h-80 overflow-y-auto rounded-lg border bg-muted/30 p-3 font-mono text-xs">
      {eventos.length === 0 && <p className="text-muted-foreground">Aguardando eventos…</p>}
      {eventos.map((e) => (
        <p key={e.id} className={cn("whitespace-pre-wrap break-words", corNivel[e.nivel])}>
          <span className="text-muted-foreground">{hora(e.criado_em)}</span> {e.mensagem}
        </p>
      ))}
      <div ref={fim} />
    </div>
  );
}
