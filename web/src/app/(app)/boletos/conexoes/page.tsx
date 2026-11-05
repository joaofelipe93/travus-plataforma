"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
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
import {
  BadgeEstadoWhatsapp,
  QrWhatsapp,
  formatarNumeroWhatsapp,
  tomDoEstadoWhatsapp,
} from "@/components/conexao-whatsapp";
import { Bloco, Estados, type ItemEstado } from "@/components/painel";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { rotuloStatusMensagem } from "@/lib/boletos";
import { useSessao } from "@/lib/sessao";
import type { EstadoWhatsapp, SituacaoWhatsappBoletos } from "@/lib/tipos";

// Conexão do WhatsApp **dos boletos**. É outro número: o do notificador de check-in publica num
// grupo, este fala no privado de cada cliente. Só admin — o QR dá acesso à conta.
//
// Esta tela conecta e confere; quem liga e desliga o envio é Configurações. A mensagem de teste
// daqui sai mesmo em modo ensaio, de propósito: é com ela que se confere o pareamento.

export default function PaginaConexoesBoletos() {
  const sessao = useSessao();
  if (sessao.data?.usuario.perfil !== "admin") {
    return (
      <Alert>
        <AlertTitle>Só administradores</AlertTitle>
        <AlertDescription>
          A conexão do WhatsApp dá acesso à conta que fala com os clientes: peça a um administrador.
        </AlertDescription>
      </Alert>
    );
  }
  return <Conexoes />;
}

function Conexoes() {
  const queryClient = useQueryClient();
  const [telefone, setTelefone] = useState("");
  const [confirmandoDesconexao, setConfirmandoDesconexao] = useState(false);

  const situacao = useQuery({
    queryKey: ["whatsapp-boletos"],
    queryFn: () => api<SituacaoWhatsappBoletos>("/integracoes/whatsapp-boletos"),
    // O QR troca a cada ~20 s: enquanto não conecta, consulta a cada 2 s.
    refetchInterval: (q) => {
      const estado = q.state.data?.estado;
      return estado === "aguardando_qr" || estado === "conectando" || estado === "desconectado" ? 2000 : 15000;
    },
  });
  const dados = situacao.data;

  const estadoAnterior = useRef<EstadoWhatsapp | undefined>(undefined);
  useEffect(() => {
    const estado = dados?.estado;
    if (estado === "conectado" && (estadoAnterior.current === "aguardando_qr" || estadoAnterior.current === "conectando")) {
      toast.success("WhatsApp dos boletos conectado");
    }
    estadoAnterior.current = estado;
  }, [dados?.estado]);

  const teste = useMutation({
    mutationFn: () =>
      api<{ enfileirada: number }>("/integracoes/whatsapp-boletos/teste", { metodo: "POST", json: { telefone } }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["whatsapp-boletos"] });
      toast.success("Mensagem de teste na fila: deve chegar em alguns segundos");
    },
    onError: (e) => toast.error(e.message),
  });

  const desconectar = useMutation({
    mutationFn: () =>
      api<void>("/integracoes/whatsapp-boletos/desconectar", { metodo: "POST", json: { confirmar: true } }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["whatsapp-boletos"] });
      toast.success("Sessão apagada: leia o QR novo para parear");
    },
    onError: (e) => toast.error(e.message),
  });

  if (situacao.isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{situacao.error.message}</AlertDescription>
      </Alert>
    );
  }
  if (!dados) {
    return <Skeleton className="h-96 w-full max-w-3xl" />;
  }

  const chaves: ItemEstado[] = [
    {
      rotulo: "Chave do servidor",
      valor: dados.envio_habilitado_servidor ? "ligada" : "desligada",
      tom: dados.envio_habilitado_servidor ? "ok" : "aviso",
      detalhe: "BOLETOS_ENVIO_HABILITADO",
    },
    {
      rotulo: "Configuração da tela",
      valor: dados.envio_habilitado_configuracao ? "ligada" : "desligada",
      tom: dados.envio_habilitado_configuracao ? "ok" : "aviso",
    },
    {
      rotulo: "Serviço de envio",
      valor: dados.envio_habilitado_servico ? "ligado" : "desligado",
      tom: dados.envio_habilitado_servico ? "ok" : "aviso",
    },
    { rotulo: "Pausa entre mensagens", valor: `${dados.pausa_envio_segundos}s` },
  ];

  const fila = Object.entries(dados.fila);

  return (
    <div className="flex flex-col gap-8">
      <Alert className="max-w-3xl">
        <AlertTitle>Este é o número que fala com os clientes</AlertTitle>
        <AlertDescription>
          Não é o mesmo do grupo de reservas. Pareie aqui o chip dedicado aos boletos — se você ler este QR
          com o número das reservas, os avisos de check-in param de sair.
        </AlertDescription>
      </Alert>

      {!dados.disponivel ? (
        <Alert variant="destructive" className="max-w-3xl">
          <AlertTitle>Serviço fora do ar</AlertTitle>
          <AlertDescription>{dados.motivo ?? "o serviço de envio não respondeu"}</AlertDescription>
        </Alert>
      ) : (
        // A conexão (e o QR, quando precisa parear) é o assunto principal; o resto é consulta.
        <div className="grid items-start gap-8 xl:grid-cols-[minmax(0,32rem)_minmax(0,26rem)]">
          <Bloco
            titulo="Conexão"
            tom={tomDoEstadoWhatsapp(dados.estado)}
            descricao={dados.numero ? `Conectado como ${formatarNumeroWhatsapp(dados.numero)}.` : undefined}
            acoes={
              <>
                <BadgeEstadoWhatsapp estado={dados.estado} />
                {dados.estado === "conectado" && (
                  <Button variant="outline" size="sm" onClick={() => setConfirmandoDesconexao(true)}>
                    Desconectar
                  </Button>
                )}
              </>
            }
          >
            {dados.qr ? (
              <QrWhatsapp qr={dados.qr}>
                <ol className="flex list-decimal flex-col gap-2 pl-5">
                  <li>
                    No celular do <strong>chip dos boletos</strong>, abra o WhatsApp.
                  </li>
                  <li>
                    Toque em <strong>Configurações → Dispositivos conectados → Conectar dispositivo</strong>.
                  </li>
                  <li>Aponte a câmera para este código. Ele muda sozinho a cada 20 segundos.</li>
                  <li className="text-muted-foreground">
                    Quem ler este código passa a mandar mensagem aos clientes por esse número: não compartilhe a tela.
                  </li>
                </ol>
              </QrWhatsapp>
            ) : dados.estado === "conectado" ? (
              <p className="text-sm text-muted-foreground">
                O número está pareado. Os boletos saem pela fila, com pausa entre mensagens.
              </p>
            ) : (
              <p className="text-sm text-muted-foreground">Conectando ao WhatsApp… o QR aparece aqui em alguns segundos.</p>
            )}

            <AlertDialog
              open={confirmandoDesconexao}
              onOpenChange={(aberto) => {
                if (!desconectar.isPending) setConfirmandoDesconexao(aberto);
              }}
            >
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Desconectar o WhatsApp dos boletos?</AlertDialogTitle>
                  <AlertDialogDescription>
                    A sessão é apagada e o número precisa ser pareado de novo. Os boletos que estiverem na fila
                    esperam até alguém parear.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel disabled={desconectar.isPending}>Voltar</AlertDialogCancel>
                  <AlertDialogAction disabled={desconectar.isPending} onClick={() => desconectar.mutate()}>
                    {desconectar.isPending ? "Desconectando…" : "Desconectar"}
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </Bloco>

          <div className="flex flex-col gap-8">
            <Bloco
              titulo="Situação do envio"
              descricao="Três chaves precisam estar ligadas para um boleto sair. A mensagem de teste não depende delas: basta o número estar conectado."
              acoes={
                <Link href="/boletos/configuracao" className={buttonVariants({ variant: "outline", size: "sm" })}>
                  Configurações
                </Link>
              }
            >
              <Estados itens={chaves} />
              {fila.length > 0 && (
                <p className="text-sm text-muted-foreground">
                  Fila:{" "}
                  {fila
                    .map(
                      ([status, n]) =>
                        `${n} ${rotuloStatusMensagem[status as keyof typeof rotuloStatusMensagem] ?? status}`,
                    )
                    .join(" · ")}
                </p>
              )}
            </Bloco>

            <Bloco
              titulo="Mensagem de teste"
              descricao="Vai para um número de verdade (use o seu). Sai mesmo em modo ensaio — é com ela que se confere o pareamento —, mas passa pela fila: chega em alguns segundos."
            >
              <div className="flex flex-wrap items-end gap-2">
                <div className="grid gap-1.5">
                  <Label htmlFor="telefone-teste">Telefone</Label>
                  <Input
                    id="telefone-teste"
                    className="w-56"
                    placeholder="(11) 90000-0000"
                    value={telefone}
                    onChange={(e) => setTelefone(e.target.value)}
                  />
                </div>
                <Button
                  variant="outline"
                  onClick={() => teste.mutate()}
                  disabled={teste.isPending || telefone.trim() === "" || dados.estado !== "conectado"}
                >
                  {teste.isPending ? "Enfileirando…" : "Mandar teste"}
                </Button>
              </div>
            </Bloco>
          </div>
        </div>
      )}
    </div>
  );
}
