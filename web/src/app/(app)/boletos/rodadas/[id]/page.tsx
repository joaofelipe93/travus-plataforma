"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useParams } from "next/navigation";
import { toast } from "sonner";
import { Colunas, Secao } from "@/components/pagina";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { dataHora, useSessao } from "@/lib/sessao";
import {
  corDoStatusRodada,
  mesLegivel,
  rotuloStatusCliente,
  rotuloStatusMensagem,
  rotuloStatusRodada,
  rotuloTipoRodada,
  valor,
  vencimento,
} from "@/lib/boletos";
import type { DetalheRodada } from "@/lib/tipos";

// Detalhe de uma rodada de boletos: o que foi encontrado por cliente, as parcelas com o PDF, as
// mensagens (com o texto que foi ou seria enviado) e o log.
//
// A mensagem "não enviada" com motivo é o modo ensaio: é aqui que se confere o que sairia antes
// de ligar o envio de verdade.

export default function PaginaRodada() {
  const params = useParams<{ id: string }>();
  const id = Number(params.id);
  const sessao = useSessao();
  const queryClient = useQueryClient();

  const consulta = useQuery({
    queryKey: ["boletos-rodada", id],
    queryFn: () => api<DetalheRodada>(`/boletos/rodadas/${id}`),
    refetchInterval: (q) => {
      const s = q.state.data?.rodada.status;
      return s === "na_fila" || s === "em_andamento" ? 4000 : false;
    },
    enabled: Number.isFinite(id),
  });

  const cancelar = useMutation({
    mutationFn: () => api<DetalheRodada>(`/boletos/rodadas/${id}/cancelar`, { metodo: "POST", json: {} }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["boletos-rodada", id] });
      void queryClient.invalidateQueries({ queryKey: ["boletos-rodadas"] });
      toast.success("Cancelamento pedido");
    },
    onError: (e) => toast.error(e.message),
  });

  if (consulta.isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{consulta.error.message}</AlertDescription>
      </Alert>
    );
  }
  if (!consulta.data) {
    return <Skeleton className="h-96 w-full" />;
  }

  const { rodada, clientes, parcelas, mensagens, eventos, cobranca } = consulta.data;
  const emAndamento = rodada.status === "na_fila" || rodada.status === "em_andamento";
  const podeCancelar = emAndamento && !rodada.cancelamento_solicitado && sessao.data?.usuario.perfil !== "leitura";
  const nomeDoCliente = (rodadaClienteId: number | null) =>
    clientes.find((c) => c.id === rodadaClienteId)?.cliente_nome ?? "—";

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <Link href="/boletos/rodadas" className="text-sm text-muted-foreground underline-offset-4 hover:underline">
            ← todas as rodadas
          </Link>
          <h2 className="font-heading text-xl font-semibold">
            Rodada nº {rodada.id} · {rotuloTipoRodada[rodada.tipo]} de {mesLegivel(rodada.referencia)}
          </h2>
          <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <Badge variant={corDoStatusRodada(rodada.status)}>{rotuloStatusRodada[rodada.status]}</Badge>
            criada por {rodada.criada_por_nome} em {dataHora(rodada.criada_em)}
            {rodada.origem === "agendada" && <span>· automática</span>}
            {rodada.finalizada_em && <span>· terminou em {dataHora(rodada.finalizada_em)}</span>}
          </p>
        </div>
        {podeCancelar && (
          <Button variant="outline" onClick={() => cancelar.mutate()} disabled={cancelar.isPending}>
            Cancelar rodada
          </Button>
        )}
      </header>

      {rodada.erro && (
        <Alert variant="destructive">
          <AlertTitle>A rodada parou</AlertTitle>
          <AlertDescription>{rodada.erro}</AlertDescription>
        </Alert>
      )}
      {rodada.cancelamento_solicitado && emAndamento && (
        <Alert>
          <AlertDescription>
            Cancelamento pedido{rodada.cancelada_por_nome ? ` por ${rodada.cancelada_por_nome}` : ""}: o worker
            termina o cliente atual e para.
          </AlertDescription>
        </Alert>
      )}
      {cobranca && (
        <Alert>
          <AlertTitle>Comparação com o envio do mês</AlertTitle>
          <AlertDescription>
            {cobranca.pagas} de {cobranca.enviadas} parcela(s) mandadas no envio não voltaram como abertas —
            essas foram pagas. A cobrança vai só para quem continua com a parcela em aberto.
          </AlertDescription>
        </Alert>
      )}

      {/* O log corre ao lado: dá para ler o que o worker fez sem perder de vista as mensagens
          e os boletos que ele encontrou. */}
      <Colunas
        lateral={
          <Secao titulo="Log">
            <ol className="flex max-h-[34rem] flex-col gap-1.5 overflow-y-auto text-sm">
              {eventos.map((e) => (
                <li key={e.id} className="flex flex-col">
                  <span className="text-xs text-muted-foreground tabular-nums">{dataHora(e.criado_em)}</span>
                  <span className={e.nivel === "erro" ? "text-destructive" : e.nivel === "aviso" ? "text-amber-500" : ""}>
                    {e.mensagem}
                  </span>
                </li>
              ))}
              {eventos.length === 0 && <li className="text-sm text-muted-foreground">Nenhum evento ainda.</li>}
            </ol>
          </Secao>
        }
      >
        <section className="flex flex-col gap-3">
          <h3 className="font-heading font-semibold">Mensagens ({mensagens.length})</h3>
          {mensagens.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Nenhuma mensagem montada{emAndamento ? " ainda: elas são montadas quando a rodada termina." : "."}
            </p>
          ) : (
            <div className="flex flex-col gap-3">
              {mensagens.map((m) => (
                <article key={m.id} className="rounded-lg border p-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="font-medium">{m.destino_nome}</span>
                    <span className="flex items-center gap-2 text-xs text-muted-foreground">
                      <Badge variant={m.status === "enviada" ? "default" : m.status === "falhou" ? "destructive" : "outline"}>
                        {rotuloStatusMensagem[m.status]}
                      </Badge>
                      {m.pdf_id && (
                        <a href={`/api/arquivos/${m.pdf_id}`} target="_blank" rel="noreferrer" className="underline-offset-4 hover:underline">
                          boleto anexado
                        </a>
                      )}
                      {m.enviada_em && <span>em {dataHora(m.enviada_em)}</span>}
                    </span>
                  </div>
                  <pre className="mt-2 whitespace-pre-wrap font-sans text-sm text-muted-foreground">{m.texto}</pre>
                  {m.ultimo_erro && <p className="mt-2 text-xs text-destructive">{m.ultimo_erro}</p>}
                </article>
              ))}
            </div>
          )}
        </section>

        <section className="flex flex-col gap-3">
          <h3 className="font-heading font-semibold">Boletos encontrados ({parcelas.length})</h3>
          {parcelas.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nenhuma parcela em aberto.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Cliente</TableHead>
                  <TableHead>Cota</TableHead>
                  <TableHead>Parcela</TableHead>
                  <TableHead>Vencimento</TableHead>
                  <TableHead className="text-right">Valor</TableHead>
                  <TableHead>Boleto</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {parcelas.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell>{nomeDoCliente(p.rodada_cliente_id)}</TableCell>
                    <TableCell className="tabular-nums">
                      {p.grupo}-{p.cota}-{p.versao}
                    </TableCell>
                    <TableCell className="tabular-nums">{p.parcela}</TableCell>
                    <TableCell className="tabular-nums">{vencimento(p.vencimento)}</TableCell>
                    <TableCell className="text-right tabular-nums">{valor(p.valor_centavos)}</TableCell>
                    <TableCell>
                      {p.pdf_id ? (
                        <a href={`/api/arquivos/${p.pdf_id}`} target="_blank" rel="noreferrer" className="underline-offset-4 hover:underline">
                          abrir PDF
                        </a>
                      ) : (
                        <span className="text-xs text-muted-foreground">sem PDF</span>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </section>

        <section className="flex flex-col gap-3">
          <h3 className="font-heading font-semibold">Clientes ({clientes.length})</h3>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Cliente</TableHead>
                <TableHead>Situação</TableHead>
                <TableHead className="text-right">Cotas no site</TableHead>
                <TableHead className="text-right">No cadastro</TableHead>
                <TableHead>Observação</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {clientes.map((c) => (
                <TableRow key={c.id}>
                  <TableCell>
                    <Link href={`/clientes/${c.cliente_id}`} className="underline-offset-4 hover:underline">
                      {c.cliente_nome}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Badge variant={c.status === "erro" ? "destructive" : c.status === "consultado" ? "default" : "outline"}>
                      {rotuloStatusCliente[c.status]}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{c.cotas_no_site}</TableCell>
                  <TableCell className="text-right tabular-nums">{c.cotas_casadas}</TableCell>
                  <TableCell className="text-sm text-muted-foreground">{c.erro ?? "—"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </section>

      </Colunas>
    </div>
  );
}
