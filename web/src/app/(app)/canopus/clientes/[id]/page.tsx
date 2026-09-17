"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { CamposCliente, CamposCota, cotaParaFormulario, cotaVazia } from "@/components/campos-cadastro";
import { Comprovante } from "@/components/comprovante";
import { TabelaCotas } from "@/components/tabela-cotas";
import { Alert, AlertDescription } from "@/components/ui/alert";
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
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { dataHora, podeEditar, tagCota, useSessao } from "@/lib/sessao";
import type { Cliente, ClienteFormulario, Cota, CotaFormulario, LanceCliente } from "@/lib/tipos";

function descreverOrigem(origem: string) {
  const importacao = /^importacao:(\d+)$/.exec(origem);
  if (importacao) return `Importação nº ${importacao[1]} (antes do cadastro)`;
  return origem === "cadastro" ? "Cadastro na plataforma" : origem;
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
  const [editandoCliente, setEditandoCliente] = useState(false);
  // null: nenhum formulário de cota aberto. "nova" ou a cota que está sendo editada.
  const [cotaEmEdicao, setCotaEmEdicao] = useState<Cota | "nova" | null>(null);

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
            <div className="flex flex-wrap items-start justify-between gap-3">
              {cliente ? <h1 className="text-xl font-semibold tracking-tight">{cliente.nome}</h1> : <Skeleton className="h-7 w-72" />}
              {editavel && cliente && !editandoCliente && (
                <div className="flex gap-2">
                  <Button variant="outline" size="sm" onClick={() => setEditandoCliente(true)}>
                    Editar cliente
                  </Button>
                  <ExcluirCliente cliente={cliente} temCotas={(dados.data?.cotas.length ?? 0) > 0} />
                </div>
              )}
            </div>
            {editavel && cliente && editandoCliente ? (
              <FormularioCliente cliente={cliente} aoFechar={() => setEditandoCliente(false)} />
            ) : (
              <dl className="mt-3 grid gap-x-8 gap-y-2 text-sm sm:grid-cols-2 lg:grid-cols-4">
                <Dado rotulo="Telefone" valor={cliente?.telefone} carregando={dados.isPending} />
                <Dado rotulo="E-mail" valor={cliente?.email} carregando={dados.isPending} />
                <Dado rotulo="Origem" valor={cliente && descreverOrigem(cliente.origem)} carregando={dados.isPending} />
                <Dado rotulo="Cadastrado em" valor={cliente && dataHora(cliente.criado_em)} carregando={dados.isPending} />
              </dl>
            )}
          </div>

          <div className="flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <h2 className="font-medium">Cotas</h2>
              {editavel && cotaEmEdicao === null && (
                <Button variant="outline" size="sm" onClick={() => setCotaEmEdicao("nova")}>
                  Nova cota
                </Button>
              )}
            </div>
            <TabelaCotas
              cotas={dados.data?.cotas}
              carregando={dados.isPending}
              editavel={editavel}
              mostrarCliente={false}
              mensagemVazia="Este cliente não tem cotas."
              acoes={
                editavel
                  ? (c) => (
                      <div className="flex justify-end gap-2">
                        <Button variant="ghost" size="sm" onClick={() => setCotaEmEdicao(c)}>
                          Editar
                        </Button>
                        <ExcluirCota cota={c} />
                      </div>
                    )
                  : undefined
              }
            />
            {editavel && cotaEmEdicao !== null && cliente && (
              <FormularioCota
                clienteId={cliente.id}
                cota={cotaEmEdicao === "nova" ? null : cotaEmEdicao}
                aoFechar={() => setCotaEmEdicao(null)}
              />
            )}
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

// Depois de mexer no cadastro, tudo que mostra cliente ou cota fica velho.
function useInvalidarCadastro() {
  const queryClient = useQueryClient();
  return () => {
    for (const chave of ["cliente", "clientes", "cotas"]) {
      queryClient.invalidateQueries({ queryKey: [chave] });
    }
  };
}

/** Edita o cliente. O formulário manda tudo: telefone ou e-mail em branco apagam o contato. */
function FormularioCliente({ cliente, aoFechar }: { cliente: Cliente; aoFechar: () => void }) {
  const invalidar = useInvalidarCadastro();
  const [valor, setValor] = useState<ClienteFormulario>({
    nome: cliente.nome,
    telefone: cliente.telefone ?? "",
    email: cliente.email ?? "",
  });
  const salvar = useMutation({
    mutationFn: (v: ClienteFormulario) => api<{ cliente: Cliente }>(`/clientes/${cliente.id}`, { metodo: "PATCH", json: v }),
    onSuccess: () => {
      invalidar();
      toast.success("Cliente atualizado");
      aoFechar();
    },
  });

  return (
    <form
      className="mt-4 flex flex-col gap-4 rounded-lg border p-4"
      onSubmit={(e) => {
        e.preventDefault();
        salvar.mutate(valor);
      }}
    >
      <CamposCliente valor={valor} aoMudar={setValor} prefixo="editar-cliente" />
      {salvar.isError && <p className="text-sm text-destructive">{salvar.error.message}</p>}
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={salvar.isPending || valor.nome.trim() === ""}>
          {salvar.isPending ? "Salvando…" : "Salvar"}
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={aoFechar} disabled={salvar.isPending}>
          Cancelar
        </Button>
      </div>
    </form>
  );
}

function ExcluirCliente({ cliente, temCotas }: { cliente: Cliente; temCotas: boolean }) {
  const router = useRouter();
  const invalidar = useInvalidarCadastro();
  const [aberto, setAberto] = useState(false);
  const excluir = useMutation({
    mutationFn: () => api<void>(`/clientes/${cliente.id}`, { metodo: "DELETE" }),
    onSuccess: () => {
      invalidar();
      toast.success(`${cliente.nome} excluído`);
      router.push("/canopus/clientes");
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <AlertDialog open={aberto} onOpenChange={(a) => !excluir.isPending && setAberto(a)}>
      <Button variant="outline" size="sm" onClick={() => setAberto(true)}>
        Excluir
      </Button>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Excluir {cliente.nome}?</AlertDialogTitle>
          <AlertDialogDescription>
            {temCotas
              ? "Este cliente ainda tem cotas. Exclua as cotas ou passe-as para outro cliente antes."
              : "O cadastro sai da plataforma. Não dá para desfazer."}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={excluir.isPending}>Cancelar</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={temCotas || excluir.isPending} onClick={() => excluir.mutate()}>
            {excluir.isPending ? "Excluindo…" : "Excluir"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/** Cadastra uma cota nova (cota = null) ou edita uma existente. */
function FormularioCota({ clienteId, cota, aoFechar }: { clienteId: number; cota: Cota | null; aoFechar: () => void }) {
  const invalidar = useInvalidarCadastro();
  const [valor, setValor] = useState<CotaFormulario>(() => (cota ? cotaParaFormulario(cota) : cotaVazia()));
  const salvar = useMutation({
    mutationFn: (v: CotaFormulario) =>
      cota
        ? api<{ cota: Cota }>(`/cotas/${cota.id}`, { metodo: "PUT", json: { ...v, cliente_id: clienteId } })
        : api<{ cota: Cota }>("/cotas", { metodo: "POST", json: { ...v, cliente_id: clienteId } }),
    onSuccess: ({ cota: salva }) => {
      invalidar();
      toast.success(`Cota ${tagCota(salva)} ${cota ? "atualizada" : "cadastrada"}`);
      aoFechar();
    },
  });

  return (
    <form
      className="flex flex-col gap-4 rounded-lg border p-4"
      onSubmit={(e) => {
        e.preventDefault();
        salvar.mutate(valor);
      }}
    >
      <h3 className="text-sm font-medium">{cota ? `Editar a cota ${tagCota(cota)}` : "Nova cota"}</h3>
      <CamposCota valor={valor} aoMudar={setValor} prefixo={cota ? `cota-${cota.id}` : "cota-nova"} />
      {salvar.isError && <p className="text-sm text-destructive">{salvar.error.message}</p>}
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={salvar.isPending || valor.grupo.trim() === "" || valor.cota.trim() === ""}>
          {salvar.isPending ? "Salvando…" : "Salvar"}
        </Button>
        <Button type="button" size="sm" variant="ghost" onClick={aoFechar} disabled={salvar.isPending}>
          Cancelar
        </Button>
      </div>
    </form>
  );
}

function ExcluirCota({ cota }: { cota: Cota }) {
  const invalidar = useInvalidarCadastro();
  const [aberto, setAberto] = useState(false);
  const excluir = useMutation({
    mutationFn: () => api<void>(`/cotas/${cota.id}`, { metodo: "DELETE" }),
    onSuccess: () => {
      invalidar();
      toast.success(`Cota ${tagCota(cota)} excluída`);
      setAberto(false);
    },
    // Cota com lance ou execução não se exclui: a API recusa e a mensagem explica.
    onError: (e) => toast.error(e.message),
  });

  return (
    <AlertDialog open={aberto} onOpenChange={(a) => !excluir.isPending && setAberto(a)}>
      <Button variant="ghost" size="sm" onClick={() => setAberto(true)}>
        Excluir
      </Button>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Excluir a cota {tagCota(cota)}?</AlertDialogTitle>
          <AlertDialogDescription>
            Cota com lance registrado ou já usada numa execução não pode ser excluída — nesse caso, desative para ela ficar fora
            das execuções.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={excluir.isPending}>Cancelar</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={excluir.isPending} onClick={() => excluir.mutate()}>
            {excluir.isPending ? "Excluindo…" : "Excluir"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
