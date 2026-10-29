"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { AvisoEnsaioAtendimento } from "@/components/aviso-ensaio-atendimento";
import { Colunas, Secao } from "@/components/pagina";
import { Bloco, Estados, Vazio } from "@/components/painel";
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
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { api } from "@/lib/api";
import { tokensLegivel } from "@/lib/atendimento";
import { dataHora, useSessao } from "@/lib/sessao";
import type { Bot, DetalheBot, ResumoAtendimento } from "@/lib/tipos";

// Configurações do Atendimento: o que a plataforma faz sem ninguém pedir.
//
// Aqui o serviço é diferente dos outros dois: não há agendamento — bot não começa conversa, ele
// responde quem escreve (decisão do usuário em 29/10/2026). Então o que esta tela controla é
// **quem pode falar com cliente de verdade**: a chave do servidor (só por deploy, mostrada aqui
// para explicar o estado) e o interruptor de cada bot, que só admin mexe.
//
// Os tetos de gasto ficam na tela de cada bot, junto das instruções dele: são parte do bot, não
// do serviço.

export default function PaginaConfiguracaoAtendimento() {
  const sessao = useSessao();
  const admin = sessao.data?.usuario.perfil === "admin";

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
    return <Skeleton className="h-96 w-full max-w-3xl" />;
  }

  const lista = bots.data.bots;
  return (
    <Colunas lateral={<Lateral resumo={resumo.data} />}>
      <Secao
        titulo="Quem pode falar com cliente"
        descricao="Duas chaves em série: a do servidor libera o serviço, e cada bot tem a sua. Com qualquer uma desligada, a resposta é composta e guardada sem sair."
      >
        <div className="flex max-w-3xl flex-col gap-4">
          <div className="flex flex-col gap-2 rounded-lg border p-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <p className="font-heading font-semibold">Chave do servidor</p>
              <Badge variant={resumo.data.envio_habilitado_servico ? "default" : "secondary"}>
                {resumo.data.envio_habilitado_servico ? "ligada" : "desligada"}
              </Badge>
            </div>
            <p className="text-sm text-muted-foreground">
              <code>ATENDIMENTO_ENVIO_HABILITADO</code> no servidor. Não se muda por tela de
              propósito: é o interruptor geral, e mexer nele é decisão de quem opera a plataforma.
            </p>
          </div>

          <div className="flex flex-col gap-2 rounded-lg border p-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <p className="font-heading font-semibold">Chave da Anthropic</p>
              <Badge variant={resumo.data.modelo_disponivel ? "default" : "destructive"}>
                {resumo.data.modelo_disponivel ? "configurada" : "ausente"}
              </Badge>
            </div>
            <p className="text-sm text-muted-foreground">
              É a mesma chave do assistente da tela inicial. Sem ela nenhum bot responde, mesmo
              ligado — e o modelo de cada bot se escolhe na tela do bot.
            </p>
          </div>
        </div>
      </Secao>

      <Secao
        titulo="Envio por bot"
        descricao={
          admin
            ? "Cada bot só manda mensagem se estiver ligado e autorizado. Autorizar é de administrador."
            : "Cada bot só manda mensagem se estiver ligado e autorizado. Quem autoriza é um administrador."
        }
      >
        {lista.length === 0 ? (
          <Vazio>
            Nenhum bot cadastrado. Comece em <Link href="/atendimento/bots" className="underline">Bots</Link>.
          </Vazio>
        ) : (
          <ul className="flex max-w-3xl flex-col gap-3">
            {lista.map((b) => (
              <LinhaDoBot key={b.id} bot={b} admin={admin} servidorLiberado={resumo.data.envio_habilitado_servico} />
            ))}
          </ul>
        )}
      </Secao>
    </Colunas>
  );
}

function LinhaDoBot({
  bot,
  admin,
  servidorLiberado,
}: {
  bot: Bot;
  admin: boolean;
  servidorLiberado: boolean;
}) {
  const queryClient = useQueryClient();
  const [confirmar, setConfirmar] = useState(false);

  const definir = useMutation({
    mutationFn: (habilitado: boolean) =>
      api<DetalheBot>(`/atendimento/bots/${bot.id}/envio`, {
        metodo: "PUT",
        json: { envio_habilitado: habilitado },
      }),
    onSuccess: (_, habilitado) => {
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bots"] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bot", bot.id] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-resumo"] });
      toast.success(habilitado ? `${bot.nome}: envio autorizado` : `${bot.nome}: volta ao ensaio`);
    },
    onError: (e) => toast.error(e.message),
  });

  const responde = bot.ativo && bot.envio_habilitado && servidorLiberado;
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3 rounded-lg border p-4">
      <div className="min-w-0">
        <Link href={`/atendimento/bots/${bot.id}`} className="font-medium hover:underline">
          {bot.nome}
        </Link>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {bot.ativo ? "bot ligado" : "bot desligado"} · {bot.etapas ?? 0} etapa(s) ·{" "}
          {responde ? "está respondendo no WhatsApp" : "em ensaio: nada sai"}
        </p>
      </div>
      <div className="flex items-center gap-3">
        <Label htmlFor={`envio-${bot.id}`} className="text-sm text-muted-foreground">
          Envio
        </Label>
        <Switch
          id={`envio-${bot.id}`}
          checked={bot.envio_habilitado}
          disabled={!admin || definir.isPending}
          onCheckedChange={(v) => {
            if (v) setConfirmar(true);
            else definir.mutate(false);
          }}
        />
      </div>

      <AlertDialog open={confirmar} onOpenChange={setConfirmar}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Autorizar o envio de {bot.nome}?</AlertDialogTitle>
            <AlertDialogDescription>
              Este bot passa a responder no WhatsApp de quem escrever para ele, com texto escrito
              pelo modelo na hora e sem revisão humana antes de sair.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Não</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmar(false);
                definir.mutate(true);
              }}
            >
              Autorizar
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </li>
  );
}

function Lateral({ resumo }: { resumo: ResumoAtendimento }) {
  return (
    <>
      <AvisoEnsaioAtendimento resumo={resumo} />
      <Bloco titulo="Não há agendamento" descricao="Bot não começa conversa.">
        <p className="text-sm text-muted-foreground">
          O bot só responde quem escreve para ele — não existe disparo em massa nem mensagem que a
          plataforma envie por conta própria neste serviço.
        </p>
      </Bloco>
      <Bloco titulo="O mês">
        <Estados
          itens={[
            { rotulo: "Tokens", valor: tokensLegivel(resumo.tokens_mes) },
            { rotulo: "Conversas hoje", valor: resumo.conversas.hoje },
            { rotulo: "Mensagens guardadas sem sair", valor: resumo.fila.canceladas_mes },
            {
              rotulo: "Próxima da fila",
              valor: resumo.fila.proxima ? dataHora(resumo.fila.proxima) : "—",
            },
          ]}
        />
      </Bloco>
      <Alert>
        <AlertTitle>Os tetos de gasto ficam em cada bot</AlertTitle>
        <AlertDescription>
          Respostas por hora, tokens por conversa e a pausa antes de responder são parte do bot, na{" "}
          <Link href="/atendimento/bots" className="underline">
            tela dele
          </Link>
          .
        </AlertDescription>
      </Alert>
    </>
  );
}
