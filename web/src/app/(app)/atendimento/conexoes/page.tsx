"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { AvisoEnsaioAtendimento } from "@/components/aviso-ensaio-atendimento";
import {
  BadgeEstadoWhatsapp,
  QrWhatsapp,
  formatarNumeroWhatsapp,
  tomDoEstadoWhatsapp,
} from "@/components/conexao-whatsapp";
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
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import type { BotNaConexao, ResumoAtendimento, SituacaoWhatsappAtendimento } from "@/lib/tipos";

// Conexões do Atendimento (só admin): o WhatsApp de cada bot.
//
// É a única tela da plataforma com **vários números numa página**, porque o serviço tem uma
// sessão por bot. Cada cartão é independente: o QR de um bot não serve para outro, e desconectar
// um não encosta nos demais.
//
// Dois avisos moram aqui porque é aqui que o estrago aconteceria: o QR pareia um aparelho na
// conta do bot, e a mensagem de teste **sai para um telefone de verdade mesmo em modo ensaio**.

export default function PaginaConexoesAtendimento() {
  const situacao = useQuery({
    queryKey: ["atendimento-whatsapp"],
    queryFn: () => api<SituacaoWhatsappAtendimento>("/atendimento/whatsapp"),
    // Enquanto algum bot está esperando leitura do QR, ele troca a cada ~20s.
    refetchInterval: (q) =>
      q.state.data?.bots.some((b) => b.estado === "aguardando_qr" || b.estado === "conectando") ? 2000 : 15_000,
  });
  const resumo = useQuery({
    queryKey: ["atendimento-resumo"],
    queryFn: () => api<ResumoAtendimento>("/atendimento/resumo"),
  });

  if (situacao.isError) {
    return (
      <Alert variant="destructive" className="max-w-2xl">
        <AlertDescription>{situacao.error.message}</AlertDescription>
      </Alert>
    );
  }
  if (!situacao.data) {
    return <Skeleton className="h-96 w-full max-w-4xl" />;
  }

  const dados = situacao.data;
  const conectados = dados.bots.filter((b) => b.conectado).length;
  const esperandoQr = dados.bots.filter((b) => b.estado === "aguardando_qr");

  return (
    <Colunas
      lateral={
        <>
          {resumo.data && <AvisoEnsaioAtendimento resumo={resumo.data} />}
          <Bloco
            titulo="O serviço"
            tom={dados.disponivel ? "ok" : "erro"}
            descricao={dados.disponivel ? undefined : "Sem ele, nenhum bot conversa."}
          >
            <Estados
              itens={[
                {
                  rotulo: "Transporte do WhatsApp",
                  valor: dados.disponivel ? "no ar" : "fora do ar",
                  tom: dados.disponivel ? "ok" : "erro",
                  detalhe: dados.motivo ?? undefined,
                },
                {
                  rotulo: "Números conectados",
                  valor: `${conectados} de ${dados.bots.length}`,
                  tom: conectados > 0 ? "ok" : "aviso",
                },
                {
                  rotulo: "Envio no servidor",
                  valor: dados.envio_habilitado_servico ? "liberado" : "desligado",
                  tom: dados.envio_habilitado_servico ? "aviso" : "neutro",
                },
                ...(dados.envio_habilitado_worker === undefined
                  ? []
                  : [
                      {
                        rotulo: "Freio do serviço",
                        valor: dados.envio_habilitado_worker ? "liberado" : "puxado",
                        tom: dados.envio_habilitado_worker ? "aviso" : "neutro",
                      } as const,
                    ]),
              ]}
            />
          </Bloco>
          <Bloco titulo="As três chaves do envio" descricao="Só leitura aqui.">
            <p className="text-sm text-muted-foreground">
              Para uma resposta sair, precisam estar ligadas: a chave do servidor, o envio daquele
              bot e o freio de mão do serviço. Qualquer uma desligada e a resposta fica guardada.
            </p>
            <Link href="/atendimento/configuracao" className={buttonVariants({ variant: "outline", size: "sm" })}>
              Ver em Configurações
            </Link>
          </Bloco>
        </>
      }
    >
      {!dados.disponivel && (
        <Alert variant="destructive">
          <AlertTitle>O serviço de WhatsApp do atendimento não está respondendo</AlertTitle>
          <AlertDescription>
            {dados.motivo ?? "Sem resposta."} Enquanto isso, nenhum bot recebe nem responde
            mensagem — o que o modelo já escreveu fica na fila.
          </AlertDescription>
        </Alert>
      )}

      {esperandoQr.length > 0 && (
        <Alert>
          <AlertTitle>
            {esperandoQr.length === 1
              ? "Um bot está esperando leitura do QR"
              : `${esperandoQr.length} bots estão esperando leitura do QR`}
          </AlertTitle>
          <AlertDescription>
            Cada QR pertence a <strong>um bot</strong>: confira o nome antes de escanear. Pareie
            apenas o chip daquele atendimento — nunca o das reservas nem o dos boletos.
          </AlertDescription>
        </Alert>
      )}

      <Secao
        titulo="Um número por bot"
        descricao="Duas conexões na mesma sessão derrubam uma à outra: cada bot tem a própria conta de WhatsApp."
      >
        {dados.bots.length === 0 ? (
          <Vazio>
            Nenhum bot cadastrado. Comece em{" "}
            <Link href="/atendimento/bots" className="underline">
              Bots
            </Link>
            .
          </Vazio>
        ) : (
          <div className="flex flex-col gap-5">
            {dados.bots.map((b) => (
              <CartaoDoBot key={b.id} bot={b} servicoNoAr={dados.disponivel} />
            ))}
          </div>
        )}
      </Secao>
    </Colunas>
  );
}

function CartaoDoBot({ bot, servicoNoAr }: { bot: BotNaConexao; servicoNoAr: boolean }) {
  const queryClient = useQueryClient();
  const [telefone, setTelefone] = useState("");
  const [confirmarTeste, setConfirmarTeste] = useState(false);
  const [confirmarDesconectar, setConfirmarDesconectar] = useState(false);

  const invalidar = () => {
    void queryClient.invalidateQueries({ queryKey: ["atendimento-whatsapp"] });
  };

  const testar = useMutation({
    mutationFn: () =>
      api<{ enviada: boolean }>(`/atendimento/bots/${bot.id}/whatsapp/teste`, {
        metodo: "POST",
        json: { telefone: telefone.replace(/\D/g, "") },
      }),
    onSuccess: () => {
      toast.success("Mensagem de teste enviada");
      setTelefone("");
      invalidar();
    },
    onError: (e) => toast.error(e.message),
  });

  const desconectar = useMutation({
    mutationFn: () =>
      api<{ desconectado: boolean }>(`/atendimento/bots/${bot.id}/whatsapp/desconectar`, {
        metodo: "POST",
        json: { confirmar: true },
      }),
    onSuccess: () => {
      toast.success("Sessão apagada: um QR novo aparece em instantes");
      invalidar();
    },
    onError: (e) => toast.error(e.message),
  });

  const naFila = bot.fila.pendente ?? 0;
  const naoEntregues = bot.fila.falhou ?? 0;

  return (
    <Bloco
      titulo={bot.nome}
      tom={tomDoEstadoWhatsapp(bot.estado)}
      descricao={
        bot.sem_sessao && servicoNoAr
          ? bot.ativo
            ? "O serviço não abriu sessão para este bot: ele pode estar acima do teto de sessões."
            : "O bot está desligado, então o serviço não abre sessão para ele."
          : undefined
      }
      acoes={<BadgeEstadoWhatsapp estado={bot.estado} />}
    >
      <Estados
        itens={[
          {
            rotulo: "Número",
            valor: bot.numero ? formatarNumeroWhatsapp(bot.numero) : "não pareado",
            tom: bot.numero ? "ok" : "neutro",
            detalhe: bot.aberta_ha_ms ? `conectado há ${duracao(bot.aberta_ha_ms)}` : undefined,
          },
          {
            rotulo: "Envio deste bot",
            valor: bot.envio_habilitado ? "autorizado" : "em ensaio",
            tom: bot.envio_habilitado ? "aviso" : "neutro",
          },
          {
            rotulo: "Respostas na fila",
            valor: naFila,
            tom: naFila > 0 ? "destaque" : "neutro",
            detalhe: naoEntregues > 0 ? `${naoEntregues} não entregue(s)` : undefined,
          },
          ...(bot.ultimo_erro ? [{ rotulo: "Último erro", valor: bot.ultimo_erro, tom: "erro" } as const] : []),
        ]}
      />

      {bot.qr && (
        <QrWhatsapp qr={bot.qr}>
          <p className="font-heading font-semibold">Conectar o WhatsApp de {bot.nome}</p>
          <ol className="mt-2 flex list-decimal flex-col gap-1 pl-5">
            <li>
              No celular <strong>deste atendimento</strong>, abra o WhatsApp.
            </li>
            <li>Toque em Dispositivos conectados → Conectar dispositivo.</li>
            <li>Aponte para este QR (ele troca a cada ~20 segundos).</li>
          </ol>
          <p className="mt-3 text-muted-foreground">
            O aparelho aparece como <strong>Travus {bot.nome}</strong> na lista do celular.
          </p>
        </QrWhatsapp>
      )}

      {bot.conectado && (
        <div className="flex flex-col gap-4 rounded-lg border border-destructive/50 p-4">
          <div>
            <p className="font-heading font-semibold">Mensagem de teste</p>
            <p className="mt-1 text-sm text-muted-foreground">
              Sai pelo número deste bot <strong>para o telefone que você digitar</strong> — e sai
              mesmo em modo ensaio, porque é com ela que se confere o pareamento. Use o seu próprio
              número.
            </p>
          </div>
          <div className="flex flex-wrap items-end gap-3">
            <div className="flex min-w-56 flex-col gap-2">
              <Label htmlFor={`telefone-${bot.id}`}>Telefone com DDI e DDD</Label>
              <Input
                id={`telefone-${bot.id}`}
                value={telefone}
                onChange={(e) => setTelefone(e.target.value)}
                placeholder="5511999999999"
                inputMode="numeric"
              />
            </div>
            <Button
              variant="outline"
              disabled={telefone.replace(/\D/g, "").length < 12 || testar.isPending}
              onClick={() => setConfirmarTeste(true)}
            >
              {testar.isPending ? "Enviando…" : "Enviar teste"}
            </Button>
          </div>
        </div>
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="min-w-0 text-xs text-muted-foreground">
          Desconectar apaga a sessão deste bot e gera um QR novo. As respostas dele ficam na fila
          até alguém parear de novo. Os outros bots não são afetados.
        </p>
        <Button
          variant="outline"
          size="sm"
          disabled={!servicoNoAr || bot.sem_sessao || desconectar.isPending}
          onClick={() => setConfirmarDesconectar(true)}
        >
          Desconectar e gerar novo QR
        </Button>
      </div>

      <AlertDialog open={confirmarTeste} onOpenChange={setConfirmarTeste}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Enviar mensagem de teste?</AlertDialogTitle>
            <AlertDialogDescription>
              Uma mensagem vai sair <strong>agora</strong> do número de {bot.nome} para{" "}
              {telefone.replace(/\D/g, "")}. É uma mensagem de verdade, no WhatsApp de verdade.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmarTeste(false);
                testar.mutate();
              }}
            >
              Enviar
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={confirmarDesconectar} onOpenChange={setConfirmarDesconectar}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Desconectar o WhatsApp de {bot.nome}?</AlertDialogTitle>
            <AlertDialogDescription>
              O aparelho é desvinculado e a sessão apagada. Para o bot voltar a responder, alguém
              precisa escanear um QR novo com o celular deste atendimento.
              {naFila > 0 && ` Há ${naFila} resposta(s) na fila; elas esperam.`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmarDesconectar(false);
                desconectar.mutate();
              }}
            >
              Desconectar
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Bloco>
  );
}

/** "3 h", "12 min": tempo de conexão aberta, curto o bastante para caber na linha. */
function duracao(ms: number): string {
  const minutos = Math.round(ms / 60_000);
  if (minutos < 60) return `${minutos} min`;
  const horas = Math.round(minutos / 60);
  if (horas < 48) return `${horas} h`;
  return `${Math.round(horas / 24)} dias`;
}
