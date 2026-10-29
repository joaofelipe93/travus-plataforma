"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { AvisoEnsaioAtendimento } from "@/components/aviso-ensaio-atendimento";
import { Colunas, Secao } from "@/components/pagina";
import { Bloco, Estados, Vazio } from "@/components/painel";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import type { Bot, ResumoAtendimento } from "@/lib/tipos";

// Conexões do Atendimento (só admin): a conta de WhatsApp de cada bot.
//
// O transporte ainda não existe — o worker que conecta os números é a etapa D da issue #100 —, e
// esta tela diz isso em vez de mostrar um QR que não funciona. Ela já existe por dois motivos:
// todo serviço da plataforma tem a mesma forma (Painel, Configurações, Conexões), e é aqui que
// alguém vem perguntar "por que o bot não respondeu?". Quando a etapa D entrar, cada linha ganha
// o QR e o número conectado.

export default function PaginaConexoesAtendimento() {
  const resumo = useQuery({
    queryKey: ["atendimento-resumo"],
    queryFn: () => api<ResumoAtendimento>("/atendimento/resumo"),
  });
  const bots = useQuery({
    queryKey: ["atendimento-bots"],
    queryFn: () => api<{ bots: Bot[] }>("/atendimento/bots"),
  });

  if (resumo.isError || bots.isError) {
    return (
      <Alert variant="destructive" className="max-w-2xl">
        <AlertDescription>{(resumo.error ?? bots.error)?.message}</AlertDescription>
      </Alert>
    );
  }
  if (!resumo.data || !bots.data) {
    return <Skeleton className="h-80 w-full max-w-3xl" />;
  }

  const lista = bots.data.bots;
  const ativos = lista.filter((b) => b.ativo);

  return (
    <Colunas
      lateral={
        <>
          <AvisoEnsaioAtendimento resumo={resumo.data} />
          <Bloco titulo="O que já funciona sem WhatsApp">
            <p className="text-sm text-muted-foreground">
              Cadastrar bots, montar o fluxo e ler as conversas. Enquanto o transporte não existe,
              as conversas chegam pela rota interna da API — e a resposta fica guardada na conversa.
            </p>
          </Bloco>
        </>
      }
    >
      <Secao
        titulo="WhatsApp dos bots"
        descricao="Cada bot tem o próprio número: duas conexões na mesma sessão derrubam uma à outra."
      >
        <Alert>
          <AlertTitle>O transporte ainda não está instalado</AlertTitle>
          <AlertDescription>
            O serviço que conecta os números de WhatsApp dos bots é a próxima etapa. Até lá, nenhum
            bot aparece conectado aqui, e nenhuma mensagem sai — mesmo com as chaves de envio
            ligadas. Isto é uma falta de peça, não um erro de configuração.
          </AlertDescription>
        </Alert>

        {lista.length === 0 ? (
          <Vazio>
            Nenhum bot cadastrado ainda. Comece em{" "}
            <Link href="/atendimento/bots" className="underline">
              Bots
            </Link>
            .
          </Vazio>
        ) : (
          <ul className="flex max-w-3xl flex-col gap-3">
            {lista.map((b) => (
              <li key={b.id} className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 rounded-lg border p-4">
                <div className="min-w-0">
                  <Link href={`/atendimento/bots/${b.id}`} className="font-medium hover:underline">
                    {b.nome}
                  </Link>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    sessão <code>{b.slug}</code> · {b.ativo ? "bot ligado" : "bot desligado"}
                  </p>
                </div>
                <Badge variant="secondary">sem conexão</Badge>
              </li>
            ))}
          </ul>
        )}
      </Secao>

      <Secao titulo="Quando a conexão existir" rotulo="O que vem na próxima etapa">
        <div className="max-w-3xl">
          <Estados
            itens={[
              {
                rotulo: "Um número por bot",
                valor: `${ativos.length} bot(s) ligado(s)`,
                detalhe: "cada um pareado uma vez, pelo QR",
              },
              {
                rotulo: "Pareamento",
                valor: "só na VM de produção",
                tom: "aviso",
                detalhe: "chip de produção nunca se pareia em máquina de desenvolvimento",
              },
              {
                rotulo: "Fila de respostas",
                valor: `${resumo.data.fila.pendentes} esperando`,
                tom: resumo.data.fila.pendentes > 0 ? "destaque" : "neutro",
                detalhe: "sairão quando houver conexão e envio autorizado",
              },
            ]}
          />
        </div>
      </Secao>

      <div className="max-w-3xl">
        <Link href="/atendimento/configuracao" className={buttonVariants({ variant: "outline" })}>
          Ver as chaves de envio
        </Link>
      </div>
    </Colunas>
  );
}
