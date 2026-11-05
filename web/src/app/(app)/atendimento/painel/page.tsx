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
import { corDoStatusConversa, rotuloStatusConversa, tokensLegivel } from "@/lib/atendimento";
import { dataHora, useSessao } from "@/lib/sessao";
import type { Bot, ConversaResumo, ResumoAtendimento, SituacaoWhatsappAtendimento } from "@/lib/tipos";

// Painel do Atendimento. Ele tinha onze peças, e três diziam quase a mesma coisa (o mês, o funil
// e "respostas que não saíram") — o usuário reclamou, com razão. Agora responde três perguntas,
// nesta ordem:
//
//   1. **os quatro números**: bots ligados, conversas abertas, leads esperando, não entregues;
//   2. **o que precisa de você**: uma lista só de coisas acionáveis, com o link de cada uma.
//      Nada aqui aparece quando está tudo em ordem — e isso é a informação;
//   3. **o que aconteceu**: as conversas recentes, e o mês ao lado.
//
// A lista de bots saiu daqui: a aba Bots faz isso melhor, com cartão, estado e interruptor. O
// indicador "Bots ligados" leva lá.
//
// O perfil leitura vê os números, mas não a lista de conversas (ela traz telefone e o que a
// pessoa escreveu); a consulta nem sai nesse caso.

export default function PaginaPainelAtendimento() {
  const sessao = useSessao();
  const perfil = sessao.data?.usuario.perfil;
  const podeVerConversas = perfil === "admin" || perfil === "operador";
  const admin = perfil === "admin";

  const resumo = useQuery({
    queryKey: ["atendimento-resumo"],
    queryFn: () => api<ResumoAtendimento>("/atendimento/resumo"),
    refetchInterval: 30_000,
  });
  const bots = useQuery({
    queryKey: ["atendimento-bots"],
    queryFn: () => api<{ bots: Bot[] }>("/atendimento/bots"),
  });
  // A situação dos números é rota de admin (ela traz QR): para os outros perfis o painel não
  // mostra o que depende dela, como no painel dos boletos.
  const whatsapp = useQuery({
    queryKey: ["atendimento-whatsapp"],
    queryFn: () => api<SituacaoWhatsappAtendimento>("/atendimento/whatsapp"),
    enabled: admin,
    refetchInterval: 30_000,
  });
  const conversas = useQuery({
    queryKey: ["atendimento-conversas", "painel"],
    queryFn: () => api<{ conversas: ConversaResumo[] }>("/atendimento/conversas?limite=8"),
    enabled: podeVerConversas,
    refetchInterval: 30_000,
  });

  const r = resumo.data;
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
            detalhe={`${r.leads.aproveitados} viraram cliente até agora`}
            href={podeVerConversas ? "/atendimento/leads" : undefined}
          />
          <Indicador
            rotulo="Respostas não entregues"
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
            <Lateral resumo={r} whatsapp={admin ? whatsapp.data : undefined} />
          ) : (
            <Skeleton className="h-64 w-full" />
          )
        }
      >
        {r && (
          <PrecisaDeVoce
            resumo={r}
            bots={bots.data?.bots ?? []}
            whatsapp={admin ? whatsapp.data : undefined}
            podeVerLeads={podeVerConversas}
          />
        )}

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
              <Vazio>Nenhuma conversa ainda: elas aparecem quando alguém escreve para um bot ligado.</Vazio>
            ) : (
              <ul className="flex flex-col divide-y divide-border/70">
                {conversas.data?.conversas.map((c) => (
                  <li
                    key={c.id}
                    className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-3 first:pt-0 last:pb-0"
                  >
                    <div className="min-w-0">
                      <Link
                        href={`/atendimento/conversas/${c.id}`}
                        className="text-sm font-medium hover:underline"
                      >
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

/** Uma pendência do painel: o que está errado e o que fazer. */
type Pendencia = { texto: string; acao: string; href: string; tom: "aviso" | "erro" | "destaque" };

/**
 * "Precisa de você": só o que é acionável, com o link da ação.
 *
 * Vazio é informação — significa que o serviço está em ordem. Por isso o bloco não desaparece
 * quando não há nada: ele diz que não há nada.
 */
function PrecisaDeVoce({
  resumo,
  bots,
  whatsapp,
  podeVerLeads,
}: {
  resumo: ResumoAtendimento;
  bots: Bot[];
  whatsapp: SituacaoWhatsappAtendimento | undefined;
  podeVerLeads: boolean;
}) {
  const pendencias: Pendencia[] = [];

  if (!resumo.modelo_disponivel) {
    pendencias.push({
      texto: "Falta a chave da Anthropic no servidor: nenhum bot responde, mesmo ligado.",
      acao: "Ver em Configurações",
      href: "/atendimento/configuracao",
      tom: "erro",
    });
  }
  if (whatsapp && !whatsapp.disponivel) {
    pendencias.push({
      texto: "O serviço de WhatsApp dos bots não está respondendo: ninguém recebe nem é respondido.",
      acao: "Ver em Conexões",
      href: "/atendimento/conexoes",
      tom: "erro",
    });
  }
  if (resumo.fila.falharam > 0) {
    pendencias.push({
      texto: `${resumo.fila.falharam} resposta(s) não chegaram a quem escreveu. A conversa mostra o motivo em cada uma.`,
      acao: "Ver conversas",
      href: "/atendimento/conversas",
      tom: "erro",
    });
  }
  // Bot ligado sem número conectado é a causa mais comum de "o bot não respondeu".
  const semNumero = (whatsapp?.bots ?? []).filter((b) => b.ativo && !b.conectado);
  if (semNumero.length > 0) {
    pendencias.push({
      texto:
        semNumero.length === 1
          ? `O bot ${semNumero[0]!.nome} está ligado, mas sem número conectado: as respostas dele ficam na fila.`
          : `${semNumero.length} bots estão ligados sem número conectado: as respostas deles ficam na fila.`,
      acao: "Parear em Conexões",
      href: "/atendimento/conexoes",
      tom: "aviso",
    });
  }
  if (podeVerLeads && resumo.leads.novos > 0) {
    pendencias.push({
      texto: `${resumo.leads.novos} lead(s) esperando revisão: alguém precisa decidir se viram cliente.`,
      acao: "Revisar leads",
      href: "/atendimento/leads",
      tom: "destaque",
    });
  }
  if (podeVerLeads && resumo.conversas.pediram_humano_mes > 0) {
    pendencias.push({
      texto: `${resumo.conversas.pediram_humano_mes} pessoa(s) pediram atendente neste mês. O bot avisou que alguém responderia.`,
      acao: "Ver conversas",
      href: "/atendimento/conversas?situacao=encerradas",
      tom: "aviso",
    });
  }
  // Bot ligado sem pergunta nenhuma: ele conversa, mas não descobre nada.
  const semPerguntas = bots.filter((b) => b.ativo && (b.perguntas ?? 0) === 0);
  if (semPerguntas.length > 0) {
    pendencias.push({
      texto:
        semPerguntas.length === 1
          ? `O bot ${semPerguntas[0]!.nome} está ligado sem perguntas: ele conversa, mas não descobre nada de quem escreve.`
          : `${semPerguntas.length} bots estão ligados sem perguntas: eles conversam, mas não descobrem nada.`,
      acao: "Abrir Bots",
      href: "/atendimento/bots",
      tom: "aviso",
    });
  }
  if (bots.length > 0 && resumo.bots.ativos === 0) {
    pendencias.push({
      texto: "Nenhum bot está ligado: quem escrever não recebe resposta.",
      acao: "Abrir Bots",
      href: "/atendimento/bots",
      tom: "aviso",
    });
  }

  return (
    <Bloco
      titulo="Precisa de você"
      tom={pendencias.some((p) => p.tom === "erro") ? "erro" : pendencias.length > 0 ? "aviso" : "ok"}
      descricao={
        pendencias.length === 0
          ? undefined
          : "Só o que depende de alguém. O resto do serviço anda sozinho."
      }
    >
      {pendencias.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          Nada esperando você: nenhum lead sem revisão, nenhuma resposta sem entregar e nenhum bot
          ligado pela metade.
        </p>
      ) : (
        <ul className="flex flex-col divide-y divide-border/70">
          {pendencias.map((p) => (
            <li
              key={p.texto}
              className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 py-3 first:pt-0 last:pb-0"
            >
              <p className="min-w-0 text-sm">{p.texto}</p>
              <Link href={p.href} className={buttonVariants({ variant: "outline", size: "sm" })}>
                {p.acao}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Bloco>
  );
}

function Lateral({
  resumo,
  whatsapp,
}: {
  resumo: ResumoAtendimento;
  whatsapp: SituacaoWhatsappAtendimento | undefined;
}) {
  // Um bloco só para o mês, em vez de três dizendo quase a mesma coisa: o funil de cima para
  // baixo e, no fim, o que ele custou.
  const mes: ItemEstado[] = [
    { rotulo: "Conversas começadas", valor: resumo.conversas.hoje, detalhe: "hoje" },
    { rotulo: "Leads criados", valor: resumo.leads.no_mes },
    { rotulo: "Viraram cliente", valor: resumo.leads.aproveitados, tom: resumo.leads.aproveitados > 0 ? "ok" : "neutro" },
    { rotulo: "Descartados", valor: resumo.leads.descartados },
    { rotulo: "Respostas que não saíram", valor: resumo.fila.canceladas_mes, detalhe: "teto da hora" },
    { rotulo: "Tokens", valor: tokensLegivel(resumo.tokens_mes), detalhe: "entrada + saída" },
  ];

  return (
    <>
      <AvisoAtendimento resumo={resumo} />
      <Bloco
        titulo="O mês"
        descricao="Conversa que colhe algo vira lead; quem promove a cliente é uma pessoa. Cada resposta é uma chamada ao modelo."
      >
        <Estados itens={mes} />
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
    </>
  );
}
