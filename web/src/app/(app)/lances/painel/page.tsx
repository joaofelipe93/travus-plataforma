"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { Bloco, Estados, Indicador, Indicadores, Vazio, type ItemEstado } from "@/components/painel";
import { Colunas } from "@/components/pagina";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { execucaoTerminou, nomeTipo, rotuloDisparo, situacaoExecucao } from "@/lib/execucoes";
import { dataHora, podeEditar, useSessao } from "@/lib/sessao";
import type { ClienteResumo, ExecucaoResumo, RespostaConfiguracaoCanopus } from "@/lib/tipos";

// Painel do serviço de Lances: o que está acontecendo agora no Newcon, o que a plataforma vai
// fazer sozinha (dry-run automático e, se o admin ligou, o lance real automático) e qual é a
// carteira que entra na próxima execução. Tudo montado com as rotas que as outras telas já usam:
// o painel não sabe nada que a tela de Execuções, a de Cotas e a de Configurações não saibam.

export default function PaginaPainelLances() {
  const sessao = useSessao();
  const perfil = sessao.data?.usuario.perfil;

  const execucoes = useQuery({
    queryKey: ["execucoes"],
    queryFn: () => api<{ execucoes: ExecucaoResumo[] }>("/execucoes"),
    // Com execução ativa, o painel acompanha sozinho.
    refetchInterval: (q) => (q.state.data?.execucoes.some((e) => !execucaoTerminou(e.status)) ? 5000 : 60_000),
  });
  const config = useQuery({
    queryKey: ["configuracao-canopus"],
    queryFn: () => api<RespostaConfiguracaoCanopus>("/configuracao/canopus"),
  });
  const carteira = useQuery({
    queryKey: ["clientes", "lances"],
    queryFn: () => api<{ clientes: ClienteResumo[] }>("/clientes?servico=lances"),
  });

  const lista = execucoes.data?.execucoes ?? [];
  const ativas = lista.filter((e) => !execucaoTerminou(e.status));
  const ultima = lista.find((e) => execucaoTerminou(e.status));
  const clientes = carteira.data?.clientes ?? [];
  const cotasNoDryRun = clientes.reduce((soma, c) => soma + c.cotas_ativas, 0);
  const erroNaUltima = ultima?.com_erro ?? 0;

  const erro = execucoes.error ?? config.error ?? carteira.error;

  return (
    <div className="flex flex-col gap-10">
      {erro && (
        <Alert variant="destructive">
          <AlertDescription>{erro.message}</AlertDescription>
        </Alert>
      )}

      {carteira.isPending || execucoes.isPending ? (
        <Skeleton className="h-28 w-full" />
      ) : (
        <Indicadores>
          <Indicador
            rotulo="Clientes em Lances"
            valor={clientes.length}
            detalhe="Ativos no serviço, no cadastro de Clientes"
            href="/clientes"
          />
          <Indicador
            rotulo="Cotas no próximo dry-run"
            valor={cotasNoDryRun}
            detalhe="Cotas ativas desses clientes"
            href="/lances/cotas"
          />
          <Indicador
            rotulo="Execuções em andamento"
            valor={ativas.length}
            tom={ativas.length > 0 ? "destaque" : "neutro"}
            detalhe={ativas.length > 0 ? "O worker está no Newcon" : "Nada rodando agora"}
            href="/lances/execucoes"
          />
          <Indicador
            rotulo="Cotas com erro na última"
            valor={erroNaUltima}
            tom={erroNaUltima > 0 ? "erro" : "ok"}
            detalhe={ultima ? `Execução nº ${ultima.id}, ${dataHora(ultima.finalizada_em ?? ultima.criada_em)}` : "Nenhuma execução ainda"}
            href={ultima ? `/lances/execucoes/${ultima.id}` : "/lances/execucoes"}
          />
        </Indicadores>
      )}

      <Colunas lateral={config.data ? <RodaSozinho dados={config.data} /> : <Skeleton className="h-64 w-full" />}>
        <Bloco
          titulo={ativas.length > 0 ? "Acontecendo agora" : "Última execução"}
          descricao={
            ativas.length > 0
              ? "Uma execução por credencial do Newcon: as outras esperam na fila."
              : "Dry-run vai até a tela de credenciamento e não confirma lance."
          }
          tom={ativas.length > 0 ? "destaque" : "neutro"}
          acoes={
            podeEditar(perfil) && (
              <Link href="/lances/execucoes/nova" className={buttonVariants()}>
                Dar Lance
              </Link>
            )
          }
        >
          {execucoes.isPending ? (
            <Skeleton className="h-16 w-full" />
          ) : ativas.length > 0 ? (
            <div className="flex flex-col gap-3">
              {ativas.map((e) => (
                <LinhaExecucao key={e.id} execucao={e} />
              ))}
            </div>
          ) : ultima ? (
            <LinhaExecucao execucao={ultima} />
          ) : (
            <Vazio>Nenhuma execução ainda. O dry-run é o jeito seguro de conferir as cotas no Newcon.</Vazio>
          )}
        </Bloco>

        <Bloco
          titulo="Execuções recentes"
          acoes={
            <Link href="/lances/execucoes" className={buttonVariants({ variant: "outline", size: "sm" })}>
              Ver todas
            </Link>
          }
        >
          {execucoes.isPending ? (
            <Skeleton className="h-24 w-full" />
          ) : lista.length === 0 ? (
            <Vazio>Nada executado até agora.</Vazio>
          ) : (
            <ul className="flex flex-col divide-y divide-border/70">
              {lista.slice(0, 6).map((e) => (
                <li key={e.id} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-2 first:pt-0 last:pb-0">
                  <Link href={`/lances/execucoes/${e.id}`} className="text-sm font-medium hover:underline">
                    nº {e.id} · {nomeTipo(e.tipo)}
                    {e.origem === "agendada" && <span className="ml-2 text-xs text-muted-foreground">automática</span>}
                  </Link>
                  <span className="flex items-center gap-3 text-xs text-muted-foreground">
                    <span className="tabular-nums">
                      {e.sucesso}/{e.total} ok{e.com_erro > 0 && ` · ${e.com_erro} com erro`}
                    </span>
                    <Badge variant={situacaoExecucao[e.status].variante}>{situacaoExecucao[e.status].rotulo}</Badge>
                    <span className="whitespace-nowrap">{dataHora(e.criada_em)}</span>
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

function LinhaExecucao({ execucao: e }: { execucao: ExecucaoResumo }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
      <div className="min-w-0">
        <Link href={`/lances/execucoes/${e.id}`} className="font-heading font-semibold hover:underline">
          nº {e.id} · {nomeTipo(e.tipo)}
        </Link>
        <p className="mt-1 text-sm text-muted-foreground">
          {e.total} cota(s) · {e.sucesso} pronta(s) · {e.com_erro} com erro
          {e.restantes > 0 && !execucaoTerminou(e.status) ? ` · ${e.restantes} restante(s)` : ""} · por {e.criada_por_nome}
        </p>
      </div>
      <Badge variant={situacaoExecucao[e.status].variante}>
        {e.cancelamento_solicitado && e.status === "em_andamento" ? "Cancelando" : situacaoExecucao[e.status].rotulo}
      </Badge>
    </div>
  );
}

/** O que a plataforma faz sem ninguém pedir — e o estado das travas do lance real. */
function RodaSozinho({ dados }: { dados: RespostaConfiguracaoCanopus }) {
  const c = dados.configuracao;
  const horario = `dia ${c.dry_run_dia} às ${String(c.dry_run_hora).padStart(2, "0")}:${String(c.dry_run_minuto).padStart(2, "0")}`;
  const registraSozinha = dados.lance_real_habilitado && c.lance_real_automatico;

  const agendamento: ItemEstado[] = [
    {
      rotulo: "Dry-run automático",
      valor: c.dry_run_automatico ? "ligado" : "desligado",
      tom: c.dry_run_automatico ? "ok" : "neutro",
      detalhe: c.dry_run_automatico ? horario : undefined,
    },
    ...(c.dry_run_automatico
      ? [{ rotulo: "Próximo", valor: dataHora(c.proximo_dry_run) } satisfies ItemEstado]
      : []),
    {
      rotulo: "Último disparo",
      valor: c.ultimo_disparo_em ? dataHora(c.ultimo_disparo_em) : "nenhum",
      tom: c.ultimo_disparo_resultado && c.ultimo_disparo_resultado !== "criada" ? "aviso" : "neutro",
      detalhe: c.ultimo_disparo_resultado ? rotuloDisparo[c.ultimo_disparo_resultado] : undefined,
    },
    {
      rotulo: "Aviso por e-mail",
      valor: c.aviso_email ? "ligado" : "desligado",
      tom: c.aviso_email && !dados.email_configurado ? "aviso" : "neutro",
      detalhe: c.aviso_email && !dados.email_configurado ? "sem SMTP no servidor" : undefined,
    },
  ];

  const lanceReal: ItemEstado[] = [
    {
      rotulo: "Chave do servidor",
      valor: dados.lance_real_habilitado ? "ligada" : "desligada",
      tom: dados.lance_real_habilitado ? "aviso" : "neutro",
      detalhe: "LANCE_REAL_HABILITADO",
    },
    {
      rotulo: "Lance real automático",
      valor: c.lance_real_automatico ? "ligado" : "desligado",
      tom: c.lance_real_automatico ? "aviso" : "neutro",
      detalhe: c.lance_real_automatico ? `até ${c.lance_real_max_cotas} cotas` : undefined,
    },
    ...(c.lance_real_automatico
      ? [
          {
            rotulo: "Espera antes de registrar",
            valor: `${c.lance_real_espera_minutos} min`,
            detalhe: "janela para desligar ou cancelar",
          } satisfies ItemEstado,
        ]
      : []),
    { rotulo: "Prazo para aprovar", valor: `${c.validade_dry_run_minutos} min`, detalhe: "contado do fim do dry-run" },
  ];

  return (
    <>
      <Bloco
        titulo="Roda sozinho"
        descricao="Agendamento do mês, em Configurações."
        acoes={
          <Link href="/lances/configuracao" className={buttonVariants({ variant: "outline", size: "sm" })}>
            Ajustar
          </Link>
        }
      >
        <Estados itens={agendamento} />
      </Bloco>

      <Bloco titulo="Lance real" tom={registraSozinha ? "erro" : "neutro"}>
        {registraSozinha && (
          <Alert variant="destructive">
            <AlertTitle>A plataforma registra lance sozinha</AlertTitle>
            <AlertDescription>
              Terminado o dry-run automático, ela espera {c.lance_real_espera_minutos} min e aprova o lance real
              sem ninguém na tela. Lance registrado não se desfaz.
              {c.lance_real_automatico_por_nome && ` Ligado por ${c.lance_real_automatico_por_nome}.`}
            </AlertDescription>
          </Alert>
        )}
        <Estados itens={lanceReal} />
      </Bloco>

      <Bloco
        titulo="Comprovantes no Drive"
        descricao="Para onde vai o PDF de cada lance registrado."
        acoes={
          <Link href="/lances/conexoes" className={buttonVariants({ variant: "outline", size: "sm" })}>
            Conexões
          </Link>
        }
      >
        <Estados
          itens={[
            {
              rotulo: "Pasta em uso",
              valor:
                c.drive_pasta_origem === "nenhuma" ? (
                  "nenhuma"
                ) : (
                  <span className="font-mono text-xs">{c.drive_pasta_em_vigor}</span>
                ),
              tom: c.drive_pasta_origem === "nenhuma" ? "aviso" : "neutro",
              detalhe: c.drive_pasta_origem === "ambiente" ? "definida no servidor" : undefined,
            },
          ]}
        />
        {c.drive_pasta_origem === "nenhuma" && (
          <p className="text-sm text-muted-foreground">
            Sem pasta, o comprovante fica guardado na plataforma e não sobe para o Drive.
          </p>
        )}
      </Bloco>
    </>
  );
}
