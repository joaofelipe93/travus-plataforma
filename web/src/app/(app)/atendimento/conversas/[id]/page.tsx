"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useParams } from "next/navigation";
import { formatarNumeroWhatsapp } from "@/components/conexao-whatsapp";
import { Colunas, Secao } from "@/components/pagina";
import { Bloco, Estados, Vazio } from "@/components/painel";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import {
  corDoStatusConversa,
  corDoStatusMensagem,
  motivoLegivel,
  rotuloStatusConversa,
  rotuloStatusMensagemBot,
  tokensLegivel,
} from "@/lib/atendimento";
import { dataHora } from "@/lib/sessao";
import type { DetalheConversa, MensagemBot } from "@/lib/tipos";

// Uma conversa: o que a pessoa escreveu, o que o bot respondeu e os dados que saíram daí.
//
// A mensagem que NÃO saiu aparece igual, marcada e com o motivo: é o modo ensaio, e é esta tela
// que responde "o que o bot teria dito?" antes de alguém autorizar o envio.

export default function PaginaConversa() {
  const params = useParams<{ id: string }>();
  const id = Number(params.id);

  const consulta = useQuery({
    queryKey: ["atendimento-conversa", id],
    queryFn: () => api<DetalheConversa>(`/atendimento/conversas/${id}`),
    enabled: Number.isFinite(id),
    // Conversa aberta pode receber mensagem a qualquer momento.
    refetchInterval: (q) => (q.state.data?.conversa.status === "aberta" ? 10_000 : false),
  });

  if (consulta.isError) {
    return (
      <Alert variant="destructive" className="max-w-2xl">
        <AlertDescription>{consulta.error.message}</AlertDescription>
      </Alert>
    );
  }
  if (!consulta.data) {
    return <Skeleton className="h-96 w-full max-w-4xl" />;
  }

  const { conversa: c, mensagens, dados, lead } = consulta.data;
  const naoSaiu = mensagens.filter((m) => m.status === "cancelada" || m.status === "falhou").length;

  return (
    <Colunas
      lateralFixa
      lateral={
        <>
          <Bloco titulo="A conversa" tom={c.status === "erro" ? "erro" : "neutro"}>
            <Estados
              itens={[
                {
                  rotulo: "Situação",
                  valor: <Badge variant={corDoStatusConversa(c.status)}>{rotuloStatusConversa[c.status]}</Badge>,
                  detalhe: motivoLegivel(c.motivo) ?? undefined,
                },
                {
                  rotulo: "Etapa",
                  valor: c.etapa_nome ?? "—",
                  detalhe:
                    c.etapa_ordem && c.etapas_do_bot > 0 ? `${c.etapa_ordem} de ${c.etapas_do_bot}` : undefined,
                },
                { rotulo: "Começou", valor: dataHora(c.iniciada_em) },
                { rotulo: "Última mensagem", valor: dataHora(c.ultima_em) },
                ...(c.encerrada_em ? [{ rotulo: "Encerrou", valor: dataHora(c.encerrada_em) }] : []),
                {
                  rotulo: "Gasto",
                  valor: tokensLegivel(c.tokens_entrada + c.tokens_saida),
                  detalhe: `${c.tokens_entrada} entrada · ${c.tokens_saida} saída`,
                },
              ]}
            />
          </Bloco>
          {lead && (
            <Bloco
              titulo="O lead desta conversa"
              tom={lead.status === "novo" ? "destaque" : "ok"}
              descricao={
                lead.status === "novo"
                  ? "Esperando alguém revisar e decidir."
                  : lead.status === "aproveitado"
                    ? "Já virou cliente no cadastro."
                    : "Foi descartado, com motivo."
              }
              acoes={
                <Link href="/atendimento/leads" className={buttonVariants({ variant: "outline", size: "sm" })}>
                  Revisar
                </Link>
              }
            />
          )}
          <Bloco
            titulo="O bot"
            acoes={
              <Link
                href={`/atendimento/bots/${c.bot_id}`}
                className={buttonVariants({ variant: "outline", size: "sm" })}
              >
                Abrir
              </Link>
            }
          >
            <Estados itens={[{ rotulo: "Atendimento", valor: c.bot_nome, detalhe: c.bot_slug }]} />
          </Bloco>
        </>
      }
    >
      <Secao
        titulo={c.contato_nome || formatarNumeroWhatsapp(c.telefone)}
        descricao={
          c.contato_nome
            ? `${formatarNumeroWhatsapp(c.telefone)} — o nome vem do WhatsApp e pode não ser o da pessoa.`
            : "O WhatsApp desta pessoa não informou nome."
        }
        acoes={
          <Link href="/atendimento/conversas" className={buttonVariants({ variant: "outline", size: "sm" })}>
            Todas as conversas
          </Link>
        }
      >
        {naoSaiu > 0 && (
          <Alert>
            <AlertDescription>
              {naoSaiu} mensagem(ns) desta conversa <strong>não chegaram</strong> à pessoa. Elas
              aparecem abaixo marcadas, com o motivo (teto da hora, ou falha na entrega).
            </AlertDescription>
          </Alert>
        )}
        {mensagens.length === 0 ? (
          <Vazio>Nenhuma mensagem gravada nesta conversa.</Vazio>
        ) : (
          <ul className="flex flex-col gap-3">
            {mensagens.map((m) => (
              <Mensagem key={m.id} mensagem={m} />
            ))}
          </ul>
        )}
      </Secao>

      <Secao
        titulo="Dados coletados"
        descricao={
          lead
            ? "O que esta conversa rendeu. O lead abaixo saiu daqui."
            : "O que esta conversa rendeu. Quando ela encerrar, isto vira um lead para alguém revisar."
        }
      >
        {dados.length === 0 ? (
          <Vazio>Nada coletado ainda nesta conversa.</Vazio>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Dado</TableHead>
                <TableHead>Valor</TableHead>
                <TableHead>Etapa</TableHead>
                <TableHead>Quando</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {dados.map((d) => (
                <TableRow key={d.campo}>
                  <TableCell className="font-medium">{d.campo}</TableCell>
                  <TableCell>{d.valor}</TableCell>
                  <TableCell className="text-sm text-muted-foreground">{d.etapa_nome ?? "—"}</TableCell>
                  <TableCell className="text-xs whitespace-nowrap text-muted-foreground">
                    {dataHora(d.criado_em)}
                    {d.atualizado_em !== d.criado_em && ` · corrigido ${dataHora(d.atualizado_em)}`}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Secao>
    </Colunas>
  );
}

function Mensagem({ mensagem: m }: { mensagem: MensagemBot }) {
  const doBot = m.direcao === "enviada";
  const problema = m.status === "cancelada" || m.status === "falhou";
  return (
    <li className={doBot ? "flex justify-end" : "flex justify-start"}>
      <div
        className={[
          "flex max-w-[44rem] min-w-0 flex-col gap-1.5 rounded-xl border p-4",
          doBot ? "bg-card/80" : "bg-card/30",
          problema ? "border-aviso/50" : "border-border",
        ].join(" ")}
      >
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <span className="font-medium text-foreground">{doBot ? "Bot" : "Pessoa"}</span>
          <span>{dataHora(m.criada_em)}</span>
          {doBot && (
            <Badge variant={corDoStatusMensagem(m.status)}>{rotuloStatusMensagemBot[m.status]}</Badge>
          )}
          {m.tentativas > 1 && <span>{m.tentativas} tentativas</span>}
        </div>
        <p className="text-sm whitespace-pre-wrap">{m.texto}</p>
        {m.motivo && <p className="text-xs text-aviso">Não saiu: {m.motivo}</p>}
        {m.ultimo_erro && <p className="text-xs text-destructive">Erro: {m.ultimo_erro}</p>}
      </div>
    </li>
  );
}
