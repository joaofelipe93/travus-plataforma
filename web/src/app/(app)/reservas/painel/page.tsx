"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { BadgeEstadoWhatsapp, formatarNumeroWhatsapp, tomDoEstadoWhatsapp } from "@/components/conexao-whatsapp";
import { Colunas } from "@/components/pagina";
import { Bloco, Estados, Indicador, Indicadores, Vazio, type ItemEstado } from "@/components/painel";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { canal, diaLocal, haQuanto, momento, partesDoDia, reais, somarDias } from "@/lib/reservas";
import { useSessao } from "@/lib/sessao";
import type { Reserva, RespostaReservas, SituacaoWhatsapp } from "@/lib/tipos";

// Painel do serviço de Reservas: quem chega, quem está hospedado, e se o aviso no grupo do
// WhatsApp está realmente saindo. As reservas vêm do webhook do PMS e são lidas do banco, então o
// painel funciona mesmo com o notificador fora do ar — e é justamente aí que ele mais serve.
//
// A conexão do WhatsApp só é consultada para admin, que é quem pode vê-la.

export default function PaginaPainelReservas() {
  const sessao = useSessao();
  const perfil = sessao.data?.usuario.perfil;
  const admin = perfil === "admin";

  const consulta = useQuery({
    queryKey: ["reservas"],
    queryFn: () => api<RespostaReservas>("/reservas"),
    refetchInterval: 60_000,
  });
  const whatsapp = useQuery({
    queryKey: ["whatsapp"],
    queryFn: () => api<SituacaoWhatsapp>("/integracoes/whatsapp"),
    enabled: admin,
  });

  const hoje = diaLocal();
  const amanha = somarDias(hoje, 1);
  const semana = somarDias(hoje, 7);
  const mes = hoje.slice(0, 7);
  const reservas = consulta.data?.reservas ?? [];
  const confirmadas = reservas.filter((r) => r.situacao === "confirmada");

  const chegamHoje = confirmadas.filter((r) => r.check_in === hoje);
  const chegamAmanha = confirmadas.filter((r) => r.check_in === amanha);
  const hospedados = confirmadas.filter((r) => ["hospedado", "sai_hoje"].includes(momento(r, hoje)));
  const proximas = confirmadas
    .filter((r) => r.check_in && r.check_in > hoje && r.check_in <= semana)
    .sort((a, b) => (a.check_in ?? "").localeCompare(b.check_in ?? ""));
  const doMes = confirmadas.filter((r) => r.check_in?.startsWith(mes));
  const valorMes = doMes.reduce((soma, r) => soma + (r.valor_centavos ?? 0), 0);
  const canceladasMes = reservas.filter((r) => r.situacao === "cancelada" && r.cancelada_em?.startsWith(mes)).length;
  const semAviso = reservas.filter((r) => r.mensagem === "falhou" || r.mensagem_cancelamento === "falhou");
  const naoReconhecidas = reservas.filter((r) => !r.reconhecida);
  const recentes = [...reservas]
    .sort((a, b) => b.recebida_em.localeCompare(a.recebida_em))
    .slice(0, 5);

  return (
    <div className="flex flex-col gap-10">
      {consulta.isError && (
        <Alert variant="destructive">
          <AlertDescription>{consulta.error.message}</AlertDescription>
        </Alert>
      )}
      {consulta.data?.limite_atingido && (
        <Alert>
          <AlertTitle>Mostrando as reservas mais recentes</AlertTitle>
          <AlertDescription>
            O histórico passou do limite lido de uma vez (3000 eventos): reservas antigas podem não estar nas contas
            desta tela.
          </AlertDescription>
        </Alert>
      )}

      {consulta.isPending ? (
        <Skeleton className="h-28 w-full" />
      ) : (
        <Indicadores>
          <Indicador
            rotulo={chegamHoje.length === 1 ? "Chega hoje" : "Chegam hoje"}
            valor={chegamHoje.length}
            tom={chegamHoje.length > 0 ? "ok" : "neutro"}
            detalhe={chegamAmanha.length > 0 ? `${chegamAmanha.length} chega(m) amanhã` : "Nada para amanhã"}
            href="/reservas/lista"
          />
          <Indicador
            rotulo="Hospedados agora"
            valor={hospedados.length}
            detalhe="Contando quem sai hoje"
            href="/reservas/lista"
          />
          <Indicador
            rotulo="Chegadas em 7 dias"
            valor={proximas.length}
            detalhe={`${doMes.length} reserva(s) no mês${canceladasMes > 0 ? ` · ${canceladasMes} cancelada(s)` : ""}`}
            href="/reservas/lista"
          />
          <Indicador
            rotulo="Avisos não entregues"
            valor={semAviso.length}
            tom={semAviso.length > 0 ? "erro" : "ok"}
            detalhe={semAviso.length > 0 ? "Reservas que o grupo não recebeu" : "Tudo que chegou foi avisado"}
            href="/reservas/lista"
          />
        </Indicadores>
      )}

      <Colunas
        lateral={
          <Lateral
            admin={admin}
            whatsapp={whatsapp.data}
            carregando={admin && whatsapp.isPending}
            valorMes={valorMes}
            reservasDoMes={doMes.length}
            naoReconhecidas={naoReconhecidas.length}
          />
        }
      >
        <Bloco
          titulo="Próximas chegadas"
          descricao="Os próximos sete dias, pela data de check-in."
          acoes={
            <Link href="/reservas/lista" className={buttonVariants({ variant: "outline", size: "sm" })}>
              Ver todas
            </Link>
          }
        >
          {consulta.isPending ? (
            <Skeleton className="h-28 w-full" />
          ) : chegamHoje.length === 0 && proximas.length === 0 ? (
            <Vazio>Nenhuma chegada nos próximos sete dias.</Vazio>
          ) : (
            <ul className="flex flex-col divide-y divide-border/70">
              {[...chegamHoje, ...proximas].slice(0, 7).map((r) => (
                <LinhaReserva key={r.chave} reserva={r} hoje={hoje} />
              ))}
            </ul>
          )}
        </Bloco>

        <Bloco titulo="Chegou do PMS" descricao="As últimas reservas e cancelamentos recebidos pelo webhook.">
          {consulta.isPending ? (
            <Skeleton className="h-24 w-full" />
          ) : recentes.length === 0 ? (
            <Vazio>Nenhum evento recebido ainda.</Vazio>
          ) : (
            <ul className="flex flex-col divide-y divide-border/70">
              {recentes.map((r) => (
                <li
                  key={r.chave}
                  className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-2 first:pt-0 last:pb-0"
                >
                  <span className="min-w-0 text-sm">
                    <strong className="font-medium">{r.hospede ?? "hóspede não informado"}</strong>
                    {r.imovel && <span className="text-muted-foreground"> · {r.imovel}</span>}
                  </span>
                  <span className="flex items-center gap-3 text-xs text-muted-foreground">
                    {r.situacao === "cancelada" ? (
                      <Badge variant="outline">cancelada</Badge>
                    ) : (
                      <Badge variant="secondary">confirmada</Badge>
                    )}
                    <span className="whitespace-nowrap">{haQuanto(r.recebida_em)}</span>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Bloco>
      </Colunas>
    </div>
  );
}

function LinhaReserva({ reserva: r, hoje }: { reserva: Reserva; hoje: string }) {
  const dia = r.check_in ? partesDoDia(r.check_in) : null;
  const c = canal(r.canal);
  const avisado = r.mensagem === "enviada" || r.mensagem === "resumo";
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 py-3 first:pt-0 last:pb-0">
      <div className="flex min-w-0 items-center gap-4">
        {dia && (
          <span className="w-12 shrink-0 text-center">
            <span className="block font-heading text-lg leading-none font-semibold tabular-nums">{dia.dia}</span>
            <span className="block text-xs text-muted-foreground">{dia.mes}</span>
          </span>
        )}
        <span className="min-w-0">
          <span className="block truncate text-sm font-medium">{r.hospede ?? "hóspede não informado"}</span>
          <span className="block truncate text-xs text-muted-foreground">
            {r.imovel ?? "imóvel não informado"}
            {r.noites ? ` · ${r.noites} noite(s)` : ""}
            {r.check_in === hoje ? " · chega hoje" : ""}
          </span>
        </span>
      </div>
      <span className="flex items-center gap-2 text-xs">
        {c && <span className={`rounded-md px-2 py-0.5 ${c.cor}`}>{c.nome}</span>}
        {!avisado && <Badge variant="outline">aviso {r.mensagem === "falhou" ? "falhou" : "pendente"}</Badge>}
      </span>
    </li>
  );
}

function Lateral({
  admin,
  whatsapp,
  carregando,
  valorMes,
  reservasDoMes,
  naoReconhecidas,
}: {
  admin: boolean;
  whatsapp: SituacaoWhatsapp | undefined;
  carregando: boolean;
  valorMes: number;
  reservasDoMes: number;
  naoReconhecidas: number;
}) {
  const fila = whatsapp?.fila;
  const itens: ItemEstado[] = whatsapp
    ? [
        {
          rotulo: "Conexão",
          valor: (
            <BadgeEstadoWhatsapp
              estado={whatsapp.estado}
              rotulo={whatsapp.estado === "indisponivel" ? "notificador fora do ar" : undefined}
            />
          ),
          detalhe: whatsapp.numero ? formatarNumeroWhatsapp(whatsapp.numero) : undefined,
        },
        {
          rotulo: "Grupo",
          valor: whatsapp.grupo ? (whatsapp.grupo.nome ?? whatsapp.grupo.jid) : "nenhum escolhido",
          tom: whatsapp.grupo ? "neutro" : "aviso",
        },
        {
          rotulo: "Fila",
          valor: `${fila?.pendente ?? 0} esperando`,
          tom: (fila?.falhou ?? 0) > 0 ? "erro" : "neutro",
          detalhe: (fila?.falhou ?? 0) > 0 ? `${fila?.falhou} sem entregar` : undefined,
        },
      ]
    : [];

  return (
    <>
      <Bloco titulo="O mês" descricao="Pelas reservas confirmadas com check-in neste mês.">
        <Estados
          itens={[
            { rotulo: "Reservas", valor: reservasDoMes },
            { rotulo: "Valor", valor: reais(valorMes) },
          ]}
        />
      </Bloco>

      <Bloco
        titulo="Aviso no grupo"
        descricao="O número que publica as reservas."
        tom={whatsapp ? tomDoEstadoWhatsapp(whatsapp.estado) : "neutro"}
        acoes={
          admin && (
            <Link href="/reservas/conexoes" className={buttonVariants({ variant: "outline", size: "sm" })}>
              Conexões
            </Link>
          )
        }
      >
        {!admin ? (
          <p className="text-sm text-muted-foreground">
            A conexão e o grupo de destino são de administrador. Se um aviso não saiu, a reserva fica marcada na
            lista e um administrador consegue ver o motivo.
          </p>
        ) : carregando ? (
          <Skeleton className="h-16 w-full" />
        ) : whatsapp ? (
          <Estados itens={itens} />
        ) : (
          <Vazio>Não foi possível consultar o notificador agora.</Vazio>
        )}
      </Bloco>

      {naoReconhecidas > 0 && (
        <Bloco titulo="Payloads não reconhecidos" tom="aviso">
          <p className="text-sm text-muted-foreground">
            {naoReconhecidas} evento(s) chegaram ao webhook com um formato que a plataforma não entende — nenhum campo
            conhecido. Eles ficam gravados, não geram aviso no grupo, e o vigia avisa por e-mail. Costuma ser automação
            nova do PMS apontada para a mesma URL.
          </p>
        </Bloco>
      )}
    </>
  );
}
