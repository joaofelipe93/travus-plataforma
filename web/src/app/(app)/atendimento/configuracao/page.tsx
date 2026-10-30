"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { AvisoAtendimento } from "@/components/aviso-atendimento";
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
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { api } from "@/lib/api";
import { tokensLegivel } from "@/lib/atendimento";
import { dataHora, useSessao } from "@/lib/sessao";
import type { Bot, DetalheBot, ResumoAtendimento } from "@/lib/tipos";

// Configurações do Atendimento. O serviço é diferente dos outros dois: **não há agendamento** —
// bot não começa conversa, ele responde quem escreve.
//
// Então o que esta tela faz é o atalho útil: **ligar e desligar os bots numa lista só**, sem
// abrir um por um. É o mesmo botão da tela do bot — o único que existe (decisão do usuário em
// 29/10/2026). Os tetos de gasto ficam em cada bot, junto das instruções dele.

export default function PaginaConfiguracaoAtendimento() {
  const sessao = useSessao();
  const podeLigar = sessao.data?.usuario.perfil !== "leitura";

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
        titulo="Quais bots estão ligados"
        descricao={
          podeLigar
            ? "Um botão por bot: ligado, ele responde quem escrever para o número dele."
            : "Um botão por bot. Seu perfil só vê esta tela; ligar e desligar é de administrador e operador."
        }
      >
        {lista.length === 0 ? (
          <Vazio>
            Nenhum bot cadastrado. Comece em{" "}
            <Link href="/atendimento/bots" className="underline">
              Bots
            </Link>
            .
          </Vazio>
        ) : (
          <ul className="flex max-w-3xl flex-col gap-3">
            {lista.map((b) => (
              <LinhaDoBot key={b.id} bot={b} podeLigar={podeLigar} />
            ))}
          </ul>
        )}
      </Secao>

      <Secao titulo="A chave da Anthropic" rotulo="A chave da Anthropic">
        <div className="flex max-w-3xl flex-col gap-2 rounded-lg border p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="font-heading font-semibold">Modelo disponível</p>
            <Badge variant={resumo.data.modelo_disponivel ? "default" : "destructive"}>
              {resumo.data.modelo_disponivel ? "configurada" : "ausente"}
            </Badge>
          </div>
          <p className="text-sm text-muted-foreground">
            É a mesma chave do assistente da tela inicial. Sem ela nenhum bot responde, mesmo
            ligado — e o modelo de cada bot se escolhe na tela do bot.
          </p>
        </div>
      </Secao>
    </Colunas>
  );
}

function LinhaDoBot({ bot, podeLigar }: { bot: Bot; podeLigar: boolean }) {
  const queryClient = useQueryClient();
  const [confirmar, setConfirmar] = useState(false);

  const definir = useMutation({
    mutationFn: (ligado: boolean) =>
      api<DetalheBot>(`/atendimento/bots/${bot.id}`, {
        metodo: "PUT",
        json: {
          nome: bot.nome,
          slug: bot.slug,
          modelo: bot.modelo,
          instrucoes: bot.instrucoes,
          saudacao: bot.saudacao ?? "",
          ativo: ligado,
          max_mensagens_hora: bot.max_mensagens_hora,
          max_tokens_conversa: bot.max_tokens_conversa,
          pausa_resposta_segundos: bot.pausa_resposta_segundos,
        },
      }),
    onSuccess: (_, ligado) => {
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bots"] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bot", bot.id] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-resumo"] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-whatsapp"] });
      toast.success(ligado ? `${bot.nome} ligado` : `${bot.nome} desligado`);
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3 rounded-lg border p-4">
      <div className="min-w-0">
        <Link href={`/atendimento/bots/${bot.id}`} className="font-medium hover:underline">
          {bot.nome}
        </Link>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {bot.perguntas ?? 0} pergunta(s) · {bot.conversas_abertas ?? 0} conversa(s) aberta(s) ·{" "}
          {bot.ativo ? "responde quem escrever" : "desligado"}
        </p>
      </div>
      <div className="flex items-center gap-3">
        <Label htmlFor={`ligado-${bot.id}`} className="text-sm text-muted-foreground">
          Ligado
        </Label>
        <Switch
          id={`ligado-${bot.id}`}
          checked={bot.ativo}
          disabled={!podeLigar || definir.isPending}
          onCheckedChange={(v) => {
            if (v) setConfirmar(true);
            else definir.mutate(false);
          }}
        />
      </div>

      <AlertDialog open={confirmar} onOpenChange={setConfirmar}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Ligar {bot.nome}?</AlertDialogTitle>
            <AlertDialogDescription>
              Quem escrever para o número deste bot passa a receber resposta, escrita pelo modelo na
              hora e sem revisão humana antes de sair. Dá para desligar a qualquer momento.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmar(false);
                definir.mutate(true);
              }}
            >
              Ligar
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
      <AvisoAtendimento resumo={resumo} />
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
            { rotulo: "Respostas que não saíram", valor: resumo.fila.canceladas_mes },
            {
              rotulo: "Próxima da fila",
              valor: resumo.fila.proxima ? dataHora(resumo.fila.proxima) : "—",
            },
          ]}
        />
      </Bloco>
      <Bloco titulo="Os tetos ficam em cada bot">
        <p className="text-sm text-muted-foreground">
          Respostas por hora, tokens por conversa e a pausa antes de responder são parte do bot, na{" "}
          <Link href="/atendimento/bots" className="underline">
            tela dele
          </Link>
          . Eles não travam nada: protegem a fatura.
        </p>
      </Bloco>
    </>
  );
}
