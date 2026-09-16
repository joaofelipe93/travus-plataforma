"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useState } from "react";
import { Comprovante } from "@/components/comprovante";
import { TabelaCotas } from "@/components/tabela-cotas";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { dataHora, podeEditar, tagCota, useSessao } from "@/lib/sessao";
import type { Cliente, Cota, LanceCliente } from "@/lib/tipos";

function descreverOrigem(origem: string) {
  const importacao = /^importacao:(\d+)$/.exec(origem);
  return importacao ? `Importação nº ${importacao[1]}` : origem;
}

export default function PaginaCliente() {
  const { id } = useParams<{ id: string }>();
  const sessao = useSessao();
  const editavel = podeEditar(sessao.data?.usuario.perfil);
  const dados = useQuery({
    queryKey: ["cliente", id],
    queryFn: () => api<{ cliente: Cliente; cotas: Cota[]; lances: LanceCliente[] }>(`/clientes/${encodeURIComponent(id)}`),
  });
  const cliente = dados.data?.cliente;

  return (
    <div className="flex flex-col gap-6">
      <Link href="/canopus/clientes" className="text-sm text-muted-foreground hover:text-foreground">
        ← Clientes
      </Link>

      {dados.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{dados.error.message}</AlertDescription>
        </Alert>
      ) : (
        <>
          <div>
            {cliente ? <h1 className="text-xl font-semibold tracking-tight">{cliente.nome}</h1> : <Skeleton className="h-7 w-72" />}
            <dl className="mt-3 grid gap-x-8 gap-y-2 text-sm sm:grid-cols-2 lg:grid-cols-4">
              <Dado rotulo="Telefone" valor={cliente?.telefone} carregando={dados.isPending} />
              <Dado rotulo="E-mail" valor={cliente?.email} carregando={dados.isPending} />
              <Dado rotulo="Origem" valor={cliente && descreverOrigem(cliente.origem)} carregando={dados.isPending} />
              <Dado rotulo="Cadastrado em" valor={cliente && dataHora(cliente.criado_em)} carregando={dados.isPending} />
            </dl>
          </div>

          <div className="flex flex-col gap-2">
            <h2 className="font-medium">Cotas</h2>
            <TabelaCotas cotas={dados.data?.cotas} carregando={dados.isPending} editavel={editavel} mostrarCliente={false} mensagemVazia="Este cliente não tem cotas." />
          </div>

          <div className="flex flex-col gap-2">
            <h2 className="font-medium">Lances</h2>
            <HistoricoLances lances={dados.data?.lances} carregando={dados.isPending} editavel={editavel} />
            {editavel && dados.data && dados.data.cotas.length > 0 && <BuscarComprovante cotas={dados.data.cotas} />}
          </div>
        </>
      )}
    </div>
  );
}

function HistoricoLances({ lances, carregando, editavel }: { lances?: LanceCliente[]; carregando: boolean; editavel: boolean }) {
  const reimprimir = useReimpressao();
  return (
    <div className="rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Registrado em</TableHead>
            <TableHead>Cota</TableHead>
            <TableHead>Protocolo</TableHead>
            <TableHead>Assembleia</TableHead>
            <TableHead>Modalidade</TableHead>
            <TableHead>Origem</TableHead>
            <TableHead>Comprovante</TableHead>
            {editavel && <TableHead />}
          </TableRow>
        </TableHeader>
        <TableBody>
          {carregando && (
            <TableRow>
              <TableCell colSpan={8}>
                <Skeleton className="h-5 w-full" />
              </TableCell>
            </TableRow>
          )}
          {!carregando && lances?.length === 0 && (
            <TableRow>
              <TableCell colSpan={8} className="py-6 text-center text-muted-foreground">
                Nenhum lance registrado pela plataforma ainda.
              </TableCell>
            </TableRow>
          )}
          {lances?.map((l) => (
            <TableRow key={l.id}>
              <TableCell className="whitespace-nowrap">{dataHora(l.registrado_em)}</TableCell>
              <TableCell className="tabular-nums">{tagCota(l)}</TableCell>
              <TableCell className="tabular-nums">
                {l.execucao_id ? (
                  <Link className="hover:underline" href={`/canopus/execucoes/${l.execucao_id}`}>
                    {l.protocolo}
                  </Link>
                ) : (
                  l.protocolo
                )}
                {l.parcelas_em_atraso && <Badge variant="outline" className="ml-2">Parcelas em atraso</Badge>}
              </TableCell>
              <TableCell className="whitespace-nowrap">
                {l.assembleia_data ?? "—"}
                {l.assembleia_numero && ` (nº ${l.assembleia_numero})`}
              </TableCell>
              <TableCell>
                {l.modalidade}
                {l.percentual && ` · ${l.percentual}`}
              </TableCell>
              <TableCell>{l.origem === "plataforma" ? "Plataforma" : "Histórico do Newcon"}</TableCell>
              <TableCell>
                <Comprovante lance={l} editavel={editavel} />
              </TableCell>
              {editavel && (
                <TableCell>
                  <Button variant="outline" size="sm" disabled={reimprimir.isPending} onClick={() => reimprimir.mutate({ cota_id: l.cota_id, protocolo: l.protocolo })}>
                    Reimprimir
                  </Button>
                </TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function useReimpressao() {
  const router = useRouter();
  return useMutation({
    // Sem enviar_drive, o PDF fica só na plataforma (dá para enviar ao Drive depois, no comprovante).
    mutationFn: (corpo: { cota_id: number; protocolo: string; enviar_drive?: boolean }) =>
      api<{ id: number }>("/execucoes/reimpressoes", { metodo: "POST", json: corpo }),
    onSuccess: ({ id }) => router.push(`/canopus/execucoes/${id}`),
  });
}

/** Busca no Histórico do Newcon o comprovante de um protocolo (ex.: lance feito fora da plataforma). */
function BuscarComprovante({ cotas }: { cotas: Cota[] }) {
  const reimprimir = useReimpressao();
  const opcoes = cotas.map((c) => ({ value: String(c.id), label: tagCota(c) }));
  const [cota, setCota] = useState(opcoes[0].value);
  const [protocolo, setProtocolo] = useState("");
  const [enviarDrive, setEnviarDrive] = useState(false);
  const valido = /^\d+$/.test(protocolo.trim());

  return (
    <form
      className="mt-2 flex flex-wrap items-end gap-2 rounded-lg border border-dashed p-3"
      onSubmit={(e) => {
        e.preventDefault();
        if (valido) reimprimir.mutate({ cota_id: Number(cota), protocolo: protocolo.trim(), enviar_drive: enviarDrive });
      }}
    >
      <div className="grid gap-1">
        <Label>Cota</Label>
        <Select items={opcoes} value={cota} onValueChange={(v) => setCota(v ?? opcoes[0].value)}>
          <SelectTrigger className="w-48">
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
      </div>
      <div className="grid gap-1">
        <Label htmlFor="protocolo">Protocolo</Label>
        <Input id="protocolo" className="w-40" inputMode="numeric" value={protocolo} onChange={(e) => setProtocolo(e.target.value)} />
      </div>
      <Label className="flex h-9 items-center gap-2 font-normal">
        <Switch checked={enviarDrive} onCheckedChange={(v) => setEnviarDrive(v)} />
        Enviar o PDF ao Google Drive
      </Label>
      <Button type="submit" variant="outline" disabled={!valido || reimprimir.isPending}>
        Buscar comprovante no Histórico
      </Button>
      <p className="w-full text-xs text-muted-foreground">
        Abre o Histórico da cota no Newcon e reimprime o comprovante. Não registra lance. Sem o envio ao Drive, o PDF fica só na plataforma.
      </p>
      {reimprimir.isError && <p className="w-full text-sm text-destructive">{reimprimir.error.message}</p>}
    </form>
  );
}

function Dado({ rotulo, valor, carregando }: { rotulo: string; valor: string | null | undefined; carregando: boolean }) {
  return (
    <div>
      <dt className="text-muted-foreground">{rotulo}</dt>
      <dd>{carregando ? <Skeleton className="mt-1 h-4 w-32" /> : valor || "—"}</dd>
    </div>
  );
}
