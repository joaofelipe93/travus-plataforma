"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { AvisoAtendimento } from "@/components/aviso-atendimento";
import { Colunas } from "@/components/pagina";
import { Bloco, Estados, Indicador, Indicadores, Vazio, type ItemEstado } from "@/components/painel";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { corDoStatusConversa, motivoLegivel, rotuloStatusConversa, tokensLegivel } from "@/lib/atendimento";
import { dataHora, useSessao } from "@/lib/sessao";
import type { Bot, ConversaResumo, ResumoAtendimento, SituacaoWhatsappAtendimento } from "@/lib/tipos";

// Painel do Atendimento: se algum bot está realmente falando com gente, quantas conversas estão
// abertas agora, o que o mês rendeu em dados coletados — e quanto isso custou em tokens, que é a
// pergunta que ninguém faz até a fatura chegar.
//
// O perfil leitura vê os números, mas não a lista de conversas (ela traz telefone e o que a
// pessoa escreveu); a consulta nem sai nesse caso.

export default function PaginaPainelAtendimento() {
  const sessao = useSessao();
  const perfil = sessao.data?.usuario.perfil;
  const podeVerConversas = perfil === "admin" || perfil === "operador";

  const resumo = useQuery({
    queryKey: ["atendimento-resumo"],
    queryFn: () => api<ResumoAtendimento>("/atendimento/resumo"),
    refetchInterval: 30_000,
  });
  const bots = useQuery({
    queryKey: ["atendimento-bots"],
    queryFn: () => api<{ bots: Bot[] }>("/atendimento/bots"),
  });
  // A situação dos números é rota de admin (ela traz QR): para os outros perfis, o painel
  // simplesmente não mostra esse bloco, como no painel dos boletos.
  const whatsapp = useQuery({
    queryKey: ["atendimento-whatsapp"],
    queryFn: () => api<SituacaoWhatsappAtendimento>("/atendimento/whatsapp"),
    enabled: perfil === "admin",
    refetchInterval: 30_000,
  });
  const conversas = useQuery({
    queryKey: ["atendimento-conversas", "painel"],
    queryFn: () => api<{ conversas: ConversaResumo[] }>("/atendimento/conversas?limite=8"),
    enabled: podeVerConversas,
    refetchInterval: 30_000,
  });

  const r = resumo.data;
  const lista = bots.data?.bots ?? [];
  const erro = resumo.error ?? bots.error ?? conversas.error;

  return (
    <div className="flex flex-col gap-10">
      {erro && (
        <Alert variant="destructive">
          <AlertDescription>{erro.message}</AlertDescription>
        </Alert>
      )}

      {!r ? (
        <Skeleton className="h-28 w-full" />
      ) : (
        <Indicadores>
          <Indicador
            rotulo="Bots ligados"
            valor={r.bots.ativos}
            tom={r.bots.ativos > 0 ? "ok" : "neutro"}
            detalhe={
              r.bots.total === 0
                ? "Nenhum bot cadastrado ainda"
                : `de ${r.bots.total} cadastrado(s) · bot ligado responde quem escrever`
            }
            href="/atendimento/bots"
          />
          <Indicador
            rotulo="Conversas abertas"
            valor={r.conversas.abertas}
            tom={r.conversas.abertas > 0 ? "destaque" : "neutro"}
            detalhe={`${r.conversas.hoje} começaram hoje`}
            href={podeVerConversas ? "/atendimento/conversas?situacao=abertas" : undefined}
          />
          <Indicador
            rotulo="Leads esperando revisão"
            valor={r.leads.novos}
            tom={r.leads.novos > 0 ? "destaque" : "neutro"}
            detalhe={`${r.leads.aproveitados} aproveitado(s) até agora · ${r.dados_coletados_mes} dado(s) no mês`}
            href={podeVerConversas ? "/atendimento/leads" : undefined}
          />
          <Indicador
            rotulo="Não entregues"
            valor={r.fila.falharam}
            tom={r.fila.falharam > 0 ? "erro" : "ok"}
            detalhe={
              r.fila.pendentes > 0
                ? `${r.fila.pendentes} na fila${r.fila.proxima ? ` · próxima ${dataHora(r.fila.proxima)}` : ""}`
                : "Nada esperando envio"
            }
          />
        </Indicadores>
      )}

      <Colunas
        lateral={
          r ? (
            <Lateral resumo={r} whatsapp={perfil === "admin" ? whatsapp.data : undefined} />
          ) : (
            <Skeleton className="h-64 w-full" />
          )
        }
      >
        <Bloco
          titulo="Os bots"
          descricao="Cada bot é um atendimento com número, fluxo e modelo próprios."
          acoes={
            <Link href="/atendimento/bots" className={buttonVariants({ variant: "outline", size: "sm" })}>
              Bots
            </Link>
          }
        >
          {bots.isPending ? (
            <Skeleton className="h-24 w-full" />
          ) : lista.length === 0 ? (
            <Vazio>
              Nenhum bot ainda. Um bot é o nome, o que ele pode responder e as etapas do que
              precisa descobrir — comece em <strong>Bots</strong>.
            </Vazio>
          ) : (
            <ul className="flex flex-col divide-y divide-border/70">
              {lista.map((b) => (
                <li key={b.id} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-3 first:pt-0 last:pb-0">
                  <div className="min-w-0">
                    <Link href={`/atendimento/bots/${b.id}`} className="text-sm font-medium hover:underline">
                      {b.nome}
                    </Link>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      {b.etapas ?? 0} etapa(s) · {b.conversas_abertas ?? 0} conversa(s) aberta(s)
                    </p>
                  </div>
                  <span className="flex items-center gap-2">
                    {b.ativo ? <Badge>ligado</Badge> : <Badge variant="secondary">desligado</Badge>}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Bloco>

        {podeVerConversas && (
          <Bloco
            titulo="Conversas recentes"
            acoes={
              <Link href="/atendimento/conversas" className={buttonVariants({ variant: "outline", size: "sm" })}>
                Ver todas
              </Link>
            }
          >
            {conversas.isPending ? (
              <Skeleton className="h-24 w-full" />
            ) : (conversas.data?.conversas.length ?? 0) === 0 ? (
              <Vazio>Nenhuma conversa ainda: elas aparecem quando alguém escreve para um bot.</Vazio>
            ) : (
              <ul className="flex flex-col divide-y divide-border/70">
                {conversas.data?.conversas.map((c) => (
                  <li key={c.id} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-3 first:pt-0 last:pb-0">
                    <div className="min-w-0">
                      <Link href={`/atendimento/conversas/${c.id}`} className="text-sm font-medium hover:underline">
                        {c.contato_nome || c.telefone}
                      </Link>
                      <p className="mt-0.5 truncate text-xs text-muted-foreground">
                        {c.bot_nome} · {c.ultima_direcao === "enviada" ? "bot: " : ""}
                        {c.ultimo_texto || "sem mensagem"}
                      </p>
                    </div>
                    <span className="flex items-center gap-3 text-xs text-muted-foreground">
                      <Badge variant={corDoStatusConversa(c.status)}>{rotuloStatusConversa[c.status]}</Badge>
                      <span className="whitespace-nowrap">{dataHora(c.ultima_em)}</span>
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Bloco>
        )}
      </Colunas>
    </div>
  );
}

function Lateral({
  resumo,
  whatsapp,
}: {
  resumo: ResumoAtendimento;
  whatsapp: SituacaoWhatsappAtendimento | undefined;
}) {
  const gasto: ItemEstado[] = [
    { rotulo: "Tokens no mês", valor: tokensLegivel(resumo.tokens_mes), detalhe: "entrada + saída" },
    { rotulo: "Dados coletados", valor: resumo.dados_coletados_mes },
    {
      rotulo: "Pediram atendente",
      valor: resumo.conversas.pediram_humano_mes,
      tom: resumo.conversas.pediram_humano_mes > 0 ? "aviso" : "neutro",
      detalhe: motivoLegivel("pediu_humano") ?? undefined,
    },
  ];
  return (
    <>
      <AvisoAtendimento resumo={resumo} />
      <Bloco
        titulo="O mês"
        descricao="Cada mensagem respondida é uma chamada ao modelo: o gasto cresce com a conversa."
        acoes={
          <Link href="/atendimento/configuracao" className={buttonVariants({ variant: "outline", size: "sm" })}>
            Ajustar
          </Link>
        }
      >
        <Estados itens={gasto} />
      </Bloco>
      {whatsapp && (
        <Bloco
          titulo="Os números"
          descricao="Um WhatsApp por bot."
          tom={whatsapp.disponivel ? (whatsapp.bots.some((b) => b.conectado) ? "ok" : "aviso") : "erro"}
          acoes={
            <Link href="/atendimento/conexoes" className={buttonVariants({ variant: "outline", size: "sm" })}>
              Conexões
            </Link>
          }
        >
          <Estados
            itens={[
              {
                rotulo: "Transporte",
                valor: whatsapp.disponivel ? "no ar" : "fora do ar",
                tom: whatsapp.disponivel ? "ok" : "erro",
              },
              {
                rotulo: "Conectados",
                valor: `${whatsapp.bots.filter((b) => b.conectado).length} de ${whatsapp.bots.length}`,
                tom: whatsapp.bots.some((b) => b.conectado) ? "ok" : "aviso",
              },
              ...(whatsapp.bots.some((b) => b.estado === "aguardando_qr")
                ? [{ rotulo: "Esperando QR", valor: "sim", tom: "aviso" } as const]
                : []),
            ]}
          />
        </Bloco>
      )}
      <Bloco
        titulo="O funil"
        descricao="Conversa que colhe algo vira lead; quem promove a cliente é uma pessoa."
      >
        <Estados
          itens={[
            { rotulo: "Leads no mês", valor: resumo.leads.no_mes },
            {
              rotulo: "Esperando revisão",
              valor: resumo.leads.novos,
              tom: resumo.leads.novos > 0 ? "destaque" : "neutro",
            },
            { rotulo: "Viraram cliente", valor: resumo.leads.aproveitados, tom: "ok" },
            { rotulo: "Descartados", valor: resumo.leads.descartados },
          ]}
        />
      </Bloco>
      <Bloco
        titulo="Respostas que não saíram"
        descricao="Guardadas na conversa, com o motivo: teto da hora, ou falha na entrega."
      >
        <Estados
          itens={[
            { rotulo: "No mês (teto da hora)", valor: resumo.fila.canceladas_mes },
            {
              rotulo: "Não entregues",
              valor: resumo.fila.falharam,
              tom: resumo.fila.falharam > 0 ? "erro" : "ok",
            },
          ]}
        />
      </Bloco>
    </>
  );
}
