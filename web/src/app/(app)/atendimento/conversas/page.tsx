"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { Secao } from "@/components/pagina";
import { Vazio } from "@/components/painel";
import { formatarNumeroWhatsapp } from "@/components/conexao-whatsapp";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { corDoStatusConversa, motivoLegivel, rotuloStatusConversa, tokensLegivel } from "@/lib/atendimento";
import { dataHora } from "@/lib/sessao";
import type { Bot, ConversaResumo } from "@/lib/tipos";

// As conversas: quem escreveu para cada bot, em que etapa a conversa está e como terminou.
//
// É a tela de quem tria o que o bot colheu — e também onde se lê o que o bot RESPONDEU, que é a
// única forma honesta de saber se ele está indo bem. Em ensaio, a resposta que não saiu aparece
// do mesmo jeito, com o motivo.
//
// O endereço aceita ?bot=<id> e ?situacao=abertas, para o painel e a tela do bot poderem apontar
// para o recorte certo. useSearchParams precisa de <Suspense> no Next 16.

type Situacao = "todas" | "abertas" | "encerradas";

export default function PaginaConversas() {
  return (
    <Suspense fallback={<Skeleton className="h-64 w-full" />}>
      <Conversas />
    </Suspense>
  );
}

function Conversas() {
  const params = useSearchParams();
  const [bot, setBot] = useState<string>(params.get("bot") ?? "todos");
  const [situacao, setSituacao] = useState<Situacao>(
    params.get("situacao") === "abertas" ? "abertas" : params.get("situacao") === "encerradas" ? "encerradas" : "todas",
  );

  const bots = useQuery({
    queryKey: ["atendimento-bots"],
    queryFn: () => api<{ bots: Bot[] }>("/atendimento/bots"),
  });

  const busca = new URLSearchParams({ limite: "120" });
  if (bot !== "todos") busca.set("bot", bot);
  if (situacao !== "todas") busca.set("situacao", situacao);

  const conversas = useQuery({
    queryKey: ["atendimento-conversas", bot, situacao],
    queryFn: () => api<{ conversas: ConversaResumo[] }>(`/atendimento/conversas?${busca.toString()}`),
    refetchInterval: 20_000,
  });

  const lista = conversas.data?.conversas ?? [];
  const abertas = lista.filter((c) => c.status === "aberta").length;

  return (
    <Secao
      titulo="Conversas"
      descricao="Cada linha é uma pessoa que escreveu para um bot. Abra para ler a conversa inteira e os dados que ela rendeu."
      acoes={
        <div className="flex flex-wrap items-center gap-2">
          <Select value={bot} onValueChange={(v) => setBot(v ?? "todos")}>
            <SelectTrigger className="w-48" aria-label="Filtrar por bot">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="todos">Todos os bots</SelectItem>
              {(bots.data?.bots ?? []).map((b) => (
                <SelectItem key={b.id} value={String(b.id)}>
                  {b.nome}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={situacao} onValueChange={(v) => setSituacao((v ?? "todas") as Situacao)}>
            <SelectTrigger className="w-40" aria-label="Filtrar por situação">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="todas">Todas</SelectItem>
              <SelectItem value="abertas">Abertas</SelectItem>
              <SelectItem value="encerradas">Encerradas</SelectItem>
            </SelectContent>
          </Select>
          <Button variant="outline" onClick={() => void conversas.refetch()} disabled={conversas.isFetching}>
            {conversas.isFetching ? "Atualizando…" : "Atualizar"}
          </Button>
        </div>
      }
    >
      {conversas.isError && (
        <Alert variant="destructive">
          <AlertDescription>{conversas.error.message}</AlertDescription>
        </Alert>
      )}

      {conversas.isPending ? (
        <Skeleton className="h-64 w-full" />
      ) : lista.length === 0 ? (
        <Vazio>
          Nenhuma conversa neste recorte. Elas aparecem quando alguém escreve para um bot ligado.
        </Vazio>
      ) : (
        <>
          <p className="text-sm text-muted-foreground">
            {lista.length} conversa(s){abertas > 0 && ` · ${abertas} aberta(s)`}
          </p>
          <ul className="flex flex-col gap-3">
            {lista.map((c) => (
              <li key={c.id}>
                <Link
                  href={`/atendimento/conversas/${c.id}`}
                  className="flex flex-col gap-2 rounded-xl border bg-card/50 p-4 outline-none transition-colors hover:border-ouro/60 focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
                    <div className="min-w-0">
                      <p className="font-heading font-semibold">
                        {c.contato_nome || formatarNumeroWhatsapp(c.telefone)}
                      </p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        {c.contato_nome ? `${formatarNumeroWhatsapp(c.telefone)} · ` : ""}
                        {c.bot_nome}

                      </p>
                    </div>
                    <div className="flex items-center gap-3">
                      <Badge variant={corDoStatusConversa(c.status)}>{rotuloStatusConversa[c.status]}</Badge>
                      <span className="text-xs whitespace-nowrap text-muted-foreground">{dataHora(c.ultima_em)}</span>
                    </div>
                  </div>
                  <p className="truncate text-sm text-muted-foreground">
                    {c.ultima_direcao === "enviada" && <span className="text-foreground">bot: </span>}
                    {c.ultimo_texto || "sem mensagem"}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {c.mensagens} mensagem(ns) · {c.dados} dado(s) coletado(s) · {tokensLegivel(c.tokens)}
                    {c.motivo && ` · ${motivoLegivel(c.motivo)}`}
                  </p>
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
    </Secao>
  );
}
