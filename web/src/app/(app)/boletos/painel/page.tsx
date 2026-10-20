"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { AvisoEnsaio } from "@/components/aviso-ensaio";
import { BadgeEstadoWhatsapp, formatarNumeroWhatsapp, tomDoEstadoWhatsapp } from "@/components/conexao-whatsapp";
import { Colunas } from "@/components/pagina";
import { Bloco, Estados, Indicador, Indicadores, Vazio, type ItemEstado } from "@/components/painel";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { corDoStatusRodada, mesLegivel, rotuloStatusRodada, rotuloTipoRodada } from "@/lib/boletos";
import { dataHora, useSessao } from "@/lib/sessao";
import type {
  ClienteResumo,
  RespostaConfiguracaoBoletos,
  RodadaResumo,
  SituacaoWhatsappBoletos,
} from "@/lib/tipos";

// Painel do serviço de boletos: se alguma mensagem está saindo para cliente, o que já aconteceu
// no mês (envio e cobrança) e o que a plataforma vai fazer sozinha nos dias 8 e 14. Montado com
// as rotas que as outras telas já usam — a conexão do WhatsApp só é consultada para admin, que é
// quem pode vê-la.

/** Mês de referência das rodadas, "AAAA-MM" no relógio de quem está na tela. */
function mesAtual(): string {
  const agora = new Date();
  return `${agora.getFullYear()}-${String(agora.getMonth() + 1).padStart(2, "0")}`;
}

export default function PaginaPainelBoletos() {
  const sessao = useSessao();
  const admin = sessao.data?.usuario.perfil === "admin";

  const config = useQuery({
    queryKey: ["boletos-configuracao"],
    queryFn: () => api<RespostaConfiguracaoBoletos>("/boletos/configuracao"),
  });
  const rodadas = useQuery({
    queryKey: ["boletos-rodadas"],
    queryFn: () => api<{ rodadas: RodadaResumo[] }>("/boletos/rodadas"),
    refetchInterval: (q) =>
      q.state.data?.rodadas.some((r) => r.status === "na_fila" || r.status === "em_andamento") ? 5000 : 60_000,
  });
  const carteira = useQuery({
    queryKey: ["clientes", "boletos"],
    queryFn: () => api<{ clientes: ClienteResumo[] }>("/clientes?servico=boletos"),
  });
  const whatsapp = useQuery({
    queryKey: ["whatsapp-boletos"],
    queryFn: () => api<SituacaoWhatsappBoletos>("/integracoes/whatsapp-boletos"),
    enabled: admin,
  });

  const lista = rodadas.data?.rodadas ?? [];
  const mes = mesAtual();
  const doMes = lista.filter((r) => r.referencia === mes);
  const envioDoMes = doMes.find((r) => r.tipo === "envio");
  const cobrancaDoMes = doMes.find((r) => r.tipo === "cobranca");
  const ativa = lista.find((r) => r.status === "na_fila" || r.status === "em_andamento");

  const clientes = carteira.data?.clientes ?? [];
  const semDocumento = clientes.filter((c) => !c.cpf_cnpj).length;
  const fila = config.data?.fila;

  const erro = config.error ?? rodadas.error ?? carteira.error;

  return (
    <div className="flex flex-col gap-10">
      {erro && (
        <Alert variant="destructive">
          <AlertDescription>{erro.message}</AlertDescription>
        </Alert>
      )}

      {config.isPending || rodadas.isPending || carteira.isPending ? (
        <Skeleton className="h-28 w-full" />
      ) : (
        <Indicadores>
          <Indicador
            rotulo="Clientes na rodada"
            valor={clientes.length - semDocumento}
            tom={semDocumento > 0 ? "aviso" : "neutro"}
            detalhe={
              semDocumento > 0
                ? `De ${clientes.length} ativos em Boletos: ${semDocumento} sem CPF/CNPJ ficam de fora`
                : `Os ${clientes.length} clientes ativos em Boletos têm CPF/CNPJ`
            }
            href="/clientes"
          />
          <Indicador
            rotulo="Boletos encontrados no mês"
            valor={envioDoMes?.parcelas ?? 0}
            detalhe={envioDoMes ? `Envio nº ${envioDoMes.id}, ${mesLegivel(mes)}` : `Nenhum envio em ${mesLegivel(mes)}`}
            href={envioDoMes ? `/boletos/rodadas/${envioDoMes.id}` : "/boletos/rodadas"}
          />
          <Indicador
            rotulo="Mensagens na fila"
            valor={fila?.pendentes ?? 0}
            tom={(fila?.pendentes ?? 0) > 0 ? "destaque" : "neutro"}
            detalhe={fila?.proxima ? `Próxima: ${dataHora(fila.proxima)}` : "Nada esperando envio"}
          />
          <Indicador
            rotulo="Não entregues"
            valor={fila?.falharam ?? 0}
            tom={(fila?.falharam ?? 0) > 0 ? "erro" : "ok"}
            detalhe="Mensagens que desistiram de tentar"
            href={admin ? "/boletos/conexoes" : undefined}
          />
        </Indicadores>
      )}

      <Colunas
        lateral={
          config.data ? (
            <Lateral dados={config.data} whatsapp={admin ? whatsapp.data : undefined} admin={admin} />
          ) : (
            <Skeleton className="h-64 w-full" />
          )
        }
      >
        <Bloco
          titulo={`O mês de ${mesLegivel(mes)}`}
          descricao="O envio busca os boletos em aberto e guarda o PDF; a cobrança pergunta de novo depois e avisa quem continua devendo."
          tom={ativa ? "destaque" : "neutro"}
          acoes={
            <Link href="/boletos/rodadas" className={buttonVariants({ variant: "outline", size: "sm" })}>
              Rodadas
            </Link>
          }
        >
          {rodadas.isPending ? (
            <Skeleton className="h-24 w-full" />
          ) : (
            <div className="flex flex-col gap-4">
              <RodadaDoMes rotulo="Envio" rodada={envioDoMes} vazio="Ainda não houve envio neste mês." />
              <RodadaDoMes
                rotulo="Cobrança"
                rodada={cobrancaDoMes}
                vazio={
                  envioDoMes && (envioDoMes.status === "concluida" || envioDoMes.status === "concluida_com_erros")
                    ? "Ainda não houve cobrança neste mês."
                    : "Sem um envio concluído no mês, a cobrança é recusada: ela não teria com o que comparar."
                }
              />
            </div>
          )}
        </Bloco>

        <Bloco
          titulo="Rodadas recentes"
          acoes={
            <Link href="/boletos/rodadas" className={buttonVariants({ variant: "outline", size: "sm" })}>
              Ver todas
            </Link>
          }
        >
          {rodadas.isPending ? (
            <Skeleton className="h-24 w-full" />
          ) : lista.length === 0 ? (
            <Vazio>Nenhuma rodada ainda. Crie uma de envio para buscar os boletos em aberto do mês.</Vazio>
          ) : (
            <ul className="flex flex-col divide-y divide-border/70">
              {lista.slice(0, 6).map((r) => (
                <li
                  key={r.id}
                  className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-2 first:pt-0 last:pb-0"
                >
                  <Link href={`/boletos/rodadas/${r.id}`} className="text-sm font-medium hover:underline">
                    nº {r.id} · {rotuloTipoRodada[r.tipo]} de {mesLegivel(r.referencia)}
                    {r.origem === "agendada" && <span className="ml-2 text-xs text-muted-foreground">automática</span>}
                  </Link>
                  <span className="flex items-center gap-3 text-xs text-muted-foreground">
                    <span className="tabular-nums">
                      {r.consultados + r.sem_boleto}/{r.total} clientes · {r.parcelas} boleto(s)
                    </span>
                    <Badge variant={corDoStatusRodada(r.status)}>{rotuloStatusRodada[r.status]}</Badge>
                    <span className="whitespace-nowrap">{dataHora(r.finalizada_em ?? r.criada_em)}</span>
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

function RodadaDoMes({ rotulo, rodada, vazio }: { rotulo: string; rodada: RodadaResumo | undefined; vazio: string }) {
  if (!rodada) {
    return (
      <div>
        <p className="font-heading font-semibold">{rotulo}</p>
        <Vazio>{vazio}</Vazio>
      </div>
    );
  }
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
      <div className="min-w-0">
        <Link href={`/boletos/rodadas/${rodada.id}`} className="font-heading font-semibold hover:underline">
          {rotulo} · nº {rodada.id}
        </Link>
        <p className="mt-1 text-sm text-muted-foreground">
          {rodada.consultados + rodada.sem_boleto}/{rodada.total} cliente(s) consultado(s) · {rodada.parcelas} boleto(s)
          {rodada.com_erro > 0 && ` · ${rodada.com_erro} com erro`} · por {rodada.criada_por_nome}
        </p>
      </div>
      <Badge variant={corDoStatusRodada(rodada.status)}>{rotuloStatusRodada[rodada.status]}</Badge>
    </div>
  );
}

function Lateral({
  dados,
  whatsapp,
  admin,
}: {
  dados: RespostaConfiguracaoBoletos;
  whatsapp: SituacaoWhatsappBoletos | undefined;
  admin: boolean;
}) {
  const c = dados.configuracao;
  const agendamento: ItemEstado[] = [
    {
      rotulo: "Rodadas automáticas",
      valor: c.agendamento_ativo ? "ligadas" : "desligadas",
      tom: c.agendamento_ativo ? "ok" : "neutro",
      detalhe: c.agendamento_ativo ? `dias ${c.dia_envio} e ${c.dia_cobranca}` : undefined,
    },
    ...(c.agendamento_ativo
      ? ([
          { rotulo: "Próximo envio", valor: dataHora(c.proximo_envio), detalhe: c.motivo_envio || undefined },
          { rotulo: "Próxima cobrança", valor: dataHora(c.proxima_cobranca), detalhe: c.motivo_cobranca || undefined },
        ] satisfies ItemEstado[])
      : []),
    { rotulo: "Pausa entre mensagens", valor: `${c.pausa_envio_segundos}s`, detalhe: `teto de ${c.max_mensagens_rodada}` },
  ];

  return (
    <>
      <AvisoEnsaio dados={dados} />
      <Bloco
        titulo="Roda sozinho"
        descricao="Horário de Brasília."
        acoes={
          <Link href="/boletos/configuracao" className={buttonVariants({ variant: "outline", size: "sm" })}>
            Ajustar
          </Link>
        }
      >
        <Estados itens={agendamento} />
      </Bloco>

      {admin && (
        <Bloco
          titulo="WhatsApp dos clientes"
          descricao="Outro número: este fala no privado de cada cliente."
          tom={whatsapp ? tomDoEstadoWhatsapp(whatsapp.estado) : "neutro"}
          acoes={
            <Link href="/boletos/conexoes" className={buttonVariants({ variant: "outline", size: "sm" })}>
              Conexões
            </Link>
          }
        >
          {!whatsapp ? (
            <Skeleton className="h-16 w-full" />
          ) : (
            <Estados
              itens={[
                {
                  rotulo: "Conexão",
                  valor: <BadgeEstadoWhatsapp estado={whatsapp.estado} />,
                  detalhe: whatsapp.numero ? formatarNumeroWhatsapp(whatsapp.numero) : undefined,
                },
                {
                  rotulo: "Serviço de envio",
                  valor: whatsapp.envio_habilitado_servico ? "ligado" : "desligado",
                  tom: whatsapp.envio_habilitado_servico ? "ok" : "neutro",
                },
              ]}
            />
          )}
        </Bloco>
      )}
    </>
  );
}
