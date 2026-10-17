"use client";

// Painel de detalhe do cliente: contato, cotas e lances, com as ações do cadastro. É o lado
// direito do espaço de Clientes e também o que a rota /clientes/[id] mostra.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { SheetCota, SheetEditarCliente, useInvalidarCadastro } from "@/components/cadastro-sheets";
import { Comprovante } from "@/components/comprovante";
import { TabelaCotas } from "@/components/tabela-cotas";
import { TracoCotas } from "@/components/traco-cotas";
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
import { pausarConsultasDeCota, desfazer, tirarCotaDoCache } from "@/lib/cache-cadastro";
import { dataHora, documento, tagCota } from "@/lib/sessao";
import type { Cliente, Cota, LanceCliente, ServicoDoCliente } from "@/lib/tipos";

type Detalhe = { cliente: Cliente; cotas: Cota[]; lances: LanceCliente[]; servicos: ServicoDoCliente[] };

// O cadastro é um só; é aqui que se escolhe em que serviços este cliente entra. Desligar não
// apaga nada: as cotas e o histórico continuam, o cliente é que sai da fila daquele serviço.
function ServicosDoCliente({
  clienteID,
  nome,
  servicos,
  cotasAtivas,
  temDocumento,
  editavel,
}: {
  clienteID: number;
  nome: string;
  servicos: ServicoDoCliente[];
  cotasAtivas: number;
  temDocumento: boolean;
  editavel: boolean;
}) {
  const queryClient = useQueryClient();
  // O serviço que está esperando confirmação para ser desligado.
  const [desligando, setDesligando] = useState<ServicoDoCliente | null>(null);

  const alterar = useMutation({
    mutationFn: ({ servico, ativo }: { servico: string; ativo: boolean }) =>
      api(`/clientes/${clienteID}/servicos/${servico}`, { metodo: "PUT", json: { ativo } }),
    onSuccess: (_, { servico, ativo }) => {
      setDesligando(null);
      const rotulo = servicos.find((s) => s.id === servico)?.nome ?? servico;
      toast.success(ativo ? `${rotulo} ligado para ${nome}` : `${rotulo} desligado para ${nome}`);
      void queryClient.invalidateQueries({ queryKey: ["cliente", String(clienteID)] });
      void queryClient.invalidateQueries({ queryKey: ["clientes"] });
    },
    onError: (e) => {
      setDesligando(null);
      toast.error(e.message);
    },
  });

  // O que o cliente perde ao sair de cada serviço, com o número que importa.
  const consequencia = (servico: ServicoDoCliente) => {
    if (servico.id === "lances") {
      return cotasAtivas > 0
        ? `As ${cotasAtivas} cota(s) ativas dele param de entrar nos dry-runs e nos lances do Newcon.`
        : "Ele não tem cota ativa, então nada muda nas execuções por enquanto.";
    }
    if (servico.id === "boletos") {
      return temDocumento
        ? "Ele para de entrar nas rodadas do mês: não recebe mais o boleto nem a cobrança no WhatsApp."
        : "Ele está sem CPF/CNPJ, então já não entrava nas rodadas.";
    }
    return "Ele sai da fila desse serviço.";
  };

  return (
    <section className="flex flex-col gap-3">
      <div>
        <h3 className="font-medium">Serviços</h3>
        <p className="mt-0.5 text-sm text-muted-foreground">
          Em que o cadastro deste cliente é usado. Desligar não apaga cota nem histórico.
        </p>
      </div>
      <ul className="grid gap-3 sm:grid-cols-2">
        {servicos.map((s) => (
          <li key={s.id} className="flex items-start gap-3 rounded-lg border p-4">
            <Switch
              className="mt-0.5"
              checked={s.ativo}
              disabled={!editavel || alterar.isPending}
              onCheckedChange={(ligar) =>
                ligar ? alterar.mutate({ servico: s.id, ativo: true }) : setDesligando(s)
              }
              aria-label={`${s.ativo ? "Desligar" : "Ligar"} ${s.nome} para ${nome}`}
            />
            <div className="min-w-0">
              <p className="font-medium">{s.nome}</p>
              <p className="mt-0.5 text-sm text-muted-foreground">{s.descricao}</p>
              {s.atualizado_em && (
                <p className="mt-1.5 text-xs text-muted-foreground">
                  {s.ativo ? "Ligado" : "Desligado"}
                  {s.alterado_por_nome ? ` por ${s.alterado_por_nome}` : ""} em {dataHora(s.atualizado_em)}
                </p>
              )}
            </div>
          </li>
        ))}
      </ul>

      <AlertDialog open={desligando !== null} onOpenChange={(aberto) => !alterar.isPending && !aberto && setDesligando(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Desligar {desligando?.nome} para {nome}?</AlertDialogTitle>
            <AlertDialogDescription>
              {desligando && consequencia(desligando)} O cadastro continua como está e dá para ligar de novo
              quando quiser.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={alterar.isPending}>Voltar</AlertDialogCancel>
            <AlertDialogAction
              disabled={alterar.isPending}
              onClick={() => desligando && alterar.mutate({ servico: desligando.id, ativo: false })}
            >
              {alterar.isPending ? "Desligando…" : "Desligar"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}

function descreverOrigem(origem: string) {
  const importacao = /^importacao:(\d+)$/.exec(origem);
  if (importacao) return `veio da importação nº ${importacao[1]}`;
  return origem === "cadastro" ? "cadastrado na plataforma" : origem;
}

export function PainelCliente({ id, editavel }: { id: number; editavel: boolean }) {
  const dados = useQuery({
    queryKey: ["cliente", String(id)],
    queryFn: () => api<Detalhe>(`/clientes/${id}`),
  });
  const [editando, setEditando] = useState(false);
  // null: nenhum painel de cota aberto. "nova" ou a cota sendo editada.
  const [cotaAberta, setCotaAberta] = useState<Cota | "nova" | null>(null);

  if (dados.isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{dados.error.message}</AlertDescription>
      </Alert>
    );
  }

  const cliente = dados.data?.cliente;
  const cotas = dados.data?.cotas;
  const ativas = cotas?.filter((c) => c.ativa).length ?? 0;

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            {cliente ? (
              <h2 className="font-heading text-xl font-semibold break-words">{cliente.nome}</h2>
            ) : (
              <Skeleton className="h-7 w-64" />
            )}
            {cliente && (
              <p className="mt-1 text-sm text-muted-foreground">
                {[cliente.telefone, cliente.email, documento(cliente.cpf_cnpj)].filter(Boolean).join(" · ") ||
                  "Sem telefone, e-mail e CPF/CNPJ"}
              </p>
            )}
          </div>
          {editavel && cliente && (
            <div className="flex shrink-0 gap-2">
              <Button variant="outline" size="sm" onClick={() => setEditando(true)}>
                Editar
              </Button>
              <ExcluirCliente cliente={cliente} temCotas={(cotas?.length ?? 0) > 0} />
            </div>
          )}
        </div>

        {cotas && (
          <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
            <div className="flex min-w-40 flex-col gap-1.5">
              <TracoCotas total={cotas.length} ativas={ativas} tamanho="grande" />
              <p className="text-sm text-muted-foreground">
                {cotas.length === 0 ? "Nenhuma cota" : `${ativas} de ${cotas.length} cota(s) ativa(s)`}
              </p>
            </div>
            {cliente && (
              <p className="text-sm text-muted-foreground">
                {descreverOrigem(cliente.origem)} em {dataHora(cliente.criado_em)}
              </p>
            )}
          </div>
        )}
      </header>

      {dados.data && (
        <ServicosDoCliente
          clienteID={id}
          nome={dados.data.cliente.nome}
          servicos={dados.data.servicos}
          cotasAtivas={ativas}
          temDocumento={dados.data.cliente.cpf_cnpj !== null}
          editavel={editavel}
        />
      )}

      <section className="flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <h3 className="font-medium">Cotas</h3>
          {editavel && cliente && (
            <Button variant="outline" size="sm" onClick={() => setCotaAberta("nova")}>
              Nova cota
            </Button>
          )}
        </div>
        <TabelaCotas
          cotas={cotas}
          carregando={dados.isPending}
          editavel={editavel}
          mostrarCliente={false}
          mensagemVazia={editavel ? "Sem cotas ainda. Use “Nova cota” para incluir a primeira." : "Este cliente não tem cotas."}
          acoes={
            editavel
              ? (c) => (
                  <div className="flex justify-end gap-1">
                    <Button variant="ghost" size="sm" onClick={() => setCotaAberta(c)}>
                      Editar
                    </Button>
                    <ExcluirCota cota={c} />
                  </div>
                )
              : undefined
          }
        />
      </section>

      <section className="flex flex-col gap-3">
        <h3 className="font-medium">Lances</h3>
        <HistoricoLances lances={dados.data?.lances} carregando={dados.isPending} editavel={editavel} />
        {editavel && cotas && cotas.length > 0 && <BuscarComprovante cotas={cotas} />}
      </section>

      {editavel && cliente && editando && (
        <SheetEditarCliente cliente={cliente} aberto aoFechar={() => setEditando(false)} />
      )}
      {editavel && cliente && cotaAberta !== null && (
        <SheetCota
          clienteId={cliente.id}
          clienteNome={cliente.nome}
          cota={cotaAberta === "nova" ? null : cotaAberta}
          aberto
          aoFechar={() => setCotaAberta(null)}
        />
      )}
    </div>
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
      router.push("/clientes");
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

function ExcluirCota({ cota }: { cota: Cota }) {
  const queryClient = useQueryClient();
  const [aberto, setAberto] = useState(false);
  const excluir = useMutation({
    mutationFn: () => api<void>(`/cotas/${cota.id}`, { metodo: "DELETE" }),
    // A cota some da tela na hora; se a API recusar (cota com histórico), ela volta.
    onMutate: async () => {
      setAberto(false);
      const anterior = await pausarConsultasDeCota(queryClient);
      tirarCotaDoCache(queryClient, cota.id);
      return { anterior };
    },
    onSuccess: () => toast.success(`Cota ${tagCota(cota)} excluída`),
    onError: (e, _v, contexto) => {
      desfazer(queryClient, contexto?.anterior);
      toast.error(e.message);
    },
    onSettled: () => {
      for (const chave of ["cliente", "clientes", "cotas"]) {
        queryClient.invalidateQueries({ queryKey: [chave] });
      }
    },
  });

  return (
    <AlertDialog open={aberto} onOpenChange={setAberto}>
      <Button variant="ghost" size="sm" onClick={() => setAberto(true)}>
        Excluir
      </Button>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Excluir a cota {tagCota(cota)}?</AlertDialogTitle>
          <AlertDialogDescription>
            Cota com lance registrado ou já usada numa execução não pode ser excluída — nesse caso, desative para ela ficar
            fora das execuções.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancelar</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={() => excluir.mutate()}>
            Excluir
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
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
                  <Link className="hover:underline" href={`/lances/execucoes/${l.execucao_id}`}>
                    {l.protocolo}
                  </Link>
                ) : (
                  l.protocolo
                )}
                {l.parcelas_em_atraso && (
                  <Badge variant="outline" className="ml-2">
                    Parcelas em atraso
                  </Badge>
                )}
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
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={reimprimir.isPending}
                    onClick={() => reimprimir.mutate({ cota_id: l.cota_id, protocolo: l.protocolo })}
                  >
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
    onSuccess: ({ id }) => router.push(`/lances/execucoes/${id}`),
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
        Abre o Histórico da cota no Newcon e reimprime o comprovante. Não registra lance. Sem o envio ao Drive, o PDF fica só na
        plataforma.
      </p>
      {reimprimir.isError && <p className="w-full text-sm text-destructive">{reimprimir.error.message}</p>}
    </form>
  );
}
