"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useState } from "react";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { dataHora, tagCota, useSessao } from "@/lib/sessao";
import type { CotaRevisao, Revisao } from "@/lib/tipos";
import { cn } from "@/lib/utils";

type Escolha = { incluir?: boolean; registrar?: boolean };

export default function PaginaRevisao() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const queryClient = useQueryClient();
  const sessao = useSessao();
  const [escolhas, setEscolhas] = useState<Record<number, Escolha>>({});
  const [confirmando, setConfirmando] = useState(false);
  const [digitado, setDigitado] = useState("");

  const revisao = useQuery({
    queryKey: ["revisao", id],
    queryFn: () => api<Revisao>(`/execucoes/${encodeURIComponent(id)}/revisao`),
  });

  const registrar = (c: CotaRevisao) => escolhas[c.execucao_cota_id]?.registrar ?? false;
  // Sem autorização explícita, cota com lance na assembleia não entra.
  const incluida = (c: CotaRevisao) =>
    !c.bloqueio && (!c.exige_autorizacao || registrar(c)) && (escolhas[c.execucao_cota_id]?.incluir ?? !c.exige_autorizacao);
  const alterar = (c: CotaRevisao, mudanca: Escolha) =>
    setEscolhas((atual) => ({ ...atual, [c.execucao_cota_id]: { ...atual[c.execucao_cota_id], ...mudanca } }));

  const aprovar = useMutation({
    mutationFn: (dados: Revisao) => {
      const cotas = dados.cotas.filter(incluida);
      return api<{ id: number }>("/execucoes/reais", {
        metodo: "POST",
        json: {
          dry_run_id: dados.dry_run.id,
          cotas: cotas.map((c) => ({ execucao_cota_id: c.execucao_cota_id, registrar_mesmo_com_lance: registrar(c) })),
          quantidade_confirmada: Number(digitado),
        },
      });
    },
    onSuccess: ({ id: real }) => {
      queryClient.invalidateQueries({ queryKey: ["execucoes"] });
      router.push(`/canopus/execucoes/${real}`);
    },
    onError: () => setConfirmando(false),
  });

  if (revisao.isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{revisao.error.message}</AlertDescription>
      </Alert>
    );
  }
  if (!revisao.data) return <Skeleton className="h-40 w-full" />;

  const dados = revisao.data;
  const selecionadas = dados.cotas.filter(incluida);
  const comAutorizacao = selecionadas.filter((c) => c.exige_autorizacao).length;
  const assembleias = [...new Set(selecionadas.map((c) => `${c.assembleia_data}${c.assembleia_numero ? ` (nº ${c.assembleia_numero})` : ""}`))];
  const ehAdmin = sessao.data?.usuario.perfil === "admin";
  const podeEnviar = dados.pode_aprovar && ehAdmin && selecionadas.length > 0;

  return (
    <div className="flex flex-col gap-5">
      <Link href={`/canopus/execucoes/${id}`} className="text-sm text-muted-foreground hover:text-foreground">
        ← Dry-run nº {id}
      </Link>
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Revisão para lance real</h1>
        <p className="text-sm text-muted-foreground">
          Dry-run nº {dados.dry_run.id}, concluído em {dataHora(dados.dry_run.finalizada_em)}
          {dados.dry_run.valido_ate && ` · pode ser aprovado até ${dataHora(dados.dry_run.valido_ate)}`}
        </p>
      </div>

      {!ehAdmin && (
        <Alert>
          <AlertDescription>Só administradores aprovam lance real. Você pode conferir a revisão.</AlertDescription>
        </Alert>
      )}
      {dados.bloqueio && (
        <Alert variant="destructive">
          <AlertTitle>A aprovação está bloqueada</AlertTitle>
          <AlertDescription>
            {dados.bloqueio}
            {dados.dry_run.execucao_real_id && (
              <>
                {" "}
                <Link className="underline" href={`/canopus/execucoes/${dados.dry_run.execucao_real_id}`}>
                  Ver execução nº {dados.dry_run.execucao_real_id}
                </Link>
              </>
            )}
          </AlertDescription>
        </Alert>
      )}

      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-10" />
              <TableHead>Cliente</TableHead>
              <TableHead>Cota</TableHead>
              <TableHead>Assembleia</TableHead>
              <TableHead>2º Fixo</TableHead>
              <TableHead>Lances nesta assembleia</TableHead>
              <TableHead className="text-right">Screenshot</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {dados.cotas.length === 0 && (
              <TableRow>
                <TableCell colSpan={7} className="py-8 text-center text-muted-foreground">
                  Nenhuma cota ficou pronta para confirmar neste dry-run.
                </TableCell>
              </TableRow>
            )}
            {dados.cotas.map((c) => (
              <TableRow key={c.execucao_cota_id} className={cn(!incluida(c) && "text-muted-foreground")}>
                <TableCell>
                  <input
                    type="checkbox"
                    className="size-4 accent-primary"
                    checked={incluida(c)}
                    disabled={!!c.bloqueio || (c.exige_autorizacao && !registrar(c))}
                    onChange={(e) => alterar(c, { incluir: e.target.checked })}
                    aria-label={`Incluir a cota ${tagCota(c)}`}
                  />
                </TableCell>
                <TableCell>{c.cliente_nome}</TableCell>
                <TableCell className="whitespace-nowrap tabular-nums">{tagCota(c)}</TableCell>
                <TableCell className="whitespace-nowrap">
                  {c.assembleia_data ?? "—"}
                  {c.assembleia_numero && ` (nº ${c.assembleia_numero})`}
                </TableCell>
                <TableCell className="tabular-nums">{c.percentual_segundo_fixo ? `${c.percentual_segundo_fixo}%` : "—"}</TableCell>
                <TableCell className="max-w-sm whitespace-normal">
                  {c.bloqueio && <p className="text-destructive">{c.bloqueio}</p>}
                  {!c.historico_lido && <p className="text-muted-foreground">Histórico não lido no dry-run (o worker confere de novo antes de confirmar).</p>}
                  {c.exige_autorizacao ? (
                    <div className="flex flex-col gap-1">
                      <p className="text-amber-700 dark:text-amber-400">
                        Já tem lance: {[...c.lances.map((l) => `${l.protocolo} (${l.modalidade ?? "?"})`), ...c.lances_plataforma.map((p) => `${p} (plataforma)`)].join(", ")}
                      </p>
                      <label className="flex items-center gap-2 text-sm">
                        <input
                          type="checkbox"
                          className="size-4 accent-primary"
                          checked={registrar(c)}
                          disabled={!!c.bloqueio}
                          onChange={(e) => alterar(c, { registrar: e.target.checked, incluir: e.target.checked })}
                        />
                        Registrar mesmo assim
                      </label>
                    </div>
                  ) : (
                    c.historico_lido && <span className="text-muted-foreground">Nenhum</span>
                  )}
                </TableCell>
                <TableCell className="text-right">
                  {c.screenshot_id ? (
                    <a href={`/api/arquivos/${c.screenshot_id}`} target="_blank" rel="noopener noreferrer" className="inline-block">
                      {/* eslint-disable-next-line @next/next/no-img-element */}
                      <img src={`/api/arquivos/${c.screenshot_id}`} alt={`Screenshot da cota ${tagCota(c)}`} className="h-12 w-20 rounded border object-cover object-top" loading="lazy" />
                    </a>
                  ) : (
                    "—"
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      {aprovar.isError && (
        <Alert variant="destructive">
          <AlertTitle>A execução real não foi criada</AlertTitle>
          <AlertDescription>{aprovar.error.message}</AlertDescription>
        </Alert>
      )}

      <div className="sticky bottom-0 -mx-4 flex flex-wrap items-center justify-between gap-3 border-t bg-background px-4 py-3">
        <p className="text-sm">
          Serão registrados <span className="font-medium">{selecionadas.length}</span> lance(s) de 2º Lance Fixo
          {assembleias.length > 0 && ` na(s) assembleia(s) de ${assembleias.join(", ")}`}
          {comAutorizacao > 0 && `, ${comAutorizacao} em cota que já tem lance`}.
        </p>
        <Button variant="destructive" disabled={!podeEnviar} onClick={() => { setDigitado(""); setConfirmando(true); }}>
          Aprovar lance real
        </Button>
      </div>

      <AlertDialog open={confirmando} onOpenChange={(aberto) => !aprovar.isPending && setConfirmando(aberto)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Registrar {selecionadas.length} lance(s) real(is) no Newcon?</AlertDialogTitle>
            <AlertDialogDescription>
              O worker vai clicar em Confirmar para: {selecionadas.map((c) => tagCota(c)).join(", ")}. Isso não pode ser desfeito pela plataforma.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="quantidade">Digite {selecionadas.length} para confirmar</Label>
            <Input id="quantidade" inputMode="numeric" autoComplete="off" value={digitado} onChange={(e) => setDigitado(e.target.value)} />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={aprovar.isPending}>Voltar</AlertDialogCancel>
            <Button variant="destructive" disabled={aprovar.isPending || digitado !== String(selecionadas.length)} onClick={() => aprovar.mutate(dados)}>
              {aprovar.isPending ? "Criando…" : "Aprovar e registrar"}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
