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
  rotuloEstadoWhatsapp,
  tomDoEstadoWhatsapp,
} from "@/components/conexao-whatsapp";
import { Bloco, Estados } from "@/components/painel";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button, buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { useSessao } from "@/lib/sessao";
import type { EstadoWhatsapp, GrupoDestinoWhatsapp, SituacaoWhatsapp } from "@/lib/tipos";

// Conexão do notificador de check-in: o número que **publica no grupo** as reservas e os
// cancelamentos. Não é o número dos boletos, que fala no privado dos clientes. Só admin: o QR dá
// acesso à conta.
//
// Aqui se conecta, se confere e se desconecta. O grupo de destino é escolhido em Configurações.

// Mensagens da API vêm em minúscula e sem ponto final: aqui viram frase.
function frase(texto: string) {
  const t = texto.trim();
  return t.charAt(0).toUpperCase() + t.slice(1) + (/[.!?]$/.test(t) ? "" : ".");
}

export default function PaginaConexoesReservas() {
  const sessao = useSessao();
  if (sessao.data?.usuario.perfil !== "admin") {
    return (
      <Alert>
        <AlertTitle>Só administradores</AlertTitle>
        <AlertDescription>A conexão do WhatsApp dá acesso à conta: peça a um administrador.</AlertDescription>
      </Alert>
    );
  }
  return <Conexoes />;
}

function Conexoes() {
  const situacao = useQuery({
    queryKey: ["whatsapp"],
    queryFn: () => api<SituacaoWhatsapp>("/integracoes/whatsapp"),
    // O QR troca a cada ~20 s: enquanto não conecta, consulta a cada 2 s.
    refetchInterval: (q) => {
      const estado = q.state.data?.estado;
      return estado === "aguardando_qr" || estado === "conectando" || estado === "desconectado" ? 2000 : 15000;
    },
  });
  const dados = situacao.data;

  // Aviso quando o QR é lido com a tela aberta.
  const estadoAnterior = useRef<EstadoWhatsapp | undefined>(undefined);
  useEffect(() => {
    const estado = dados?.estado;
    if (estado === "conectado" && (estadoAnterior.current === "aguardando_qr" || estadoAnterior.current === "conectando")) {
      toast.success("WhatsApp conectado");
    }
    estadoAnterior.current = estado;
  }, [dados?.estado]);

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

  return (
    // A conexão (com o QR, quando precisa parear) fica na coluna larga; grupo e fila ao lado.
    <div className="grid items-start gap-8 xl:grid-cols-[minmax(0,32rem)_minmax(0,26rem)]">
      <Conexao dados={dados} />
      <div className="flex flex-col gap-8">
        {dados.disponivel && <Destino dados={dados} />}
        {dados.disponivel && <Fila fila={dados.fila} />}
      </div>
    </div>
  );
}

function Conexao({ dados }: { dados: SituacaoWhatsapp }) {
  const { estado } = dados;
  return (
    <Bloco
      titulo="Conexão"
      tom={tomDoEstadoWhatsapp(estado)}
      acoes={
        <>
          <BadgeEstadoWhatsapp
            estado={estado}
            rotulo={estado === "indisponivel" ? "notificador fora do ar" : rotuloEstadoWhatsapp[estado]}
          />
          {estado === "conectado" && <Desconectar />}
        </>
      }
    >
      {estado === "indisponivel" && (
        <Alert variant="destructive">
          <AlertDescription>
            {frase(dados.motivo ?? "O notificador de check-in não respondeu.")} Enquanto ele não responder, reservas
            novas podem não ser avisadas no grupo.
          </AlertDescription>
        </Alert>
      )}

      {estado === "aguardando_qr" && dados.qr && (
        <QrWhatsapp qr={dados.qr}>
          <ol className="flex list-decimal flex-col gap-2 pl-5">
            <li>
              No celular do <strong>número dedicado às notificações</strong>, abra o WhatsApp.
            </li>
            <li>
              Toque em <strong>Configurações → Dispositivos conectados → Conectar dispositivo</strong>.
            </li>
            <li>Aponte a câmera para este código. Ele muda sozinho a cada 20 segundos; não precisa recarregar.</li>
            <li className="text-muted-foreground">
              Quem ler este código passa a enviar mensagens por esse número: não compartilhe a tela.
            </li>
          </ol>
        </QrWhatsapp>
      )}

      {(estado === "conectando" || estado === "desconectado" || (estado === "aguardando_qr" && !dados.qr)) && (
        <p className="text-sm text-muted-foreground">Conectando ao WhatsApp… o QR aparece aqui em alguns segundos.</p>
      )}

      {estado === "conectado" && (
        <p className="text-sm">
          Conectado como{" "}
          <strong className="tabular-nums">
            {dados.numero ? formatarNumeroWhatsapp(dados.numero) : "número desconhecido"}
          </strong>
          . Aparece no celular como &quot;Travus Plataforma&quot;.
        </p>
      )}
    </Bloco>
  );
}

function Desconectar() {
  const queryClient = useQueryClient();
  const [confirmando, setConfirmando] = useState(false);
  const desconectar = useMutation({
    mutationFn: () => api("/integracoes/whatsapp/desconectar", { metodo: "POST", json: { confirmar: true } }),
    onSuccess: () => {
      setConfirmando(false);
      toast("Sessão encerrada: escaneie o novo QR para voltar a notificar");
      void queryClient.invalidateQueries({ queryKey: ["whatsapp"] });
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setConfirmando(true)}>
        Desconectar e gerar novo QR
      </Button>
      <AlertDialog
        open={confirmando}
        onOpenChange={(aberto) => {
          if (!desconectar.isPending) setConfirmando(aberto);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Desconectar o WhatsApp?</AlertDialogTitle>
            <AlertDialogDescription>
              O aparelho é removido dos dispositivos conectados e as notificações param até alguém escanear o novo QR. As
              reservas que chegarem nesse meio tempo ficam na fila e são enviadas depois. Use para trocar de número ou
              quando a conexão travar.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={desconectar.isPending}>Cancelar</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={desconectar.isPending} onClick={() => desconectar.mutate()}>
              {desconectar.isPending ? "Desconectando…" : "Desconectar"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function descreverGrupo(grupo: GrupoDestinoWhatsapp) {
  return grupo.nome ?? grupo.jid;
}

/** Para onde os avisos vão hoje: quem muda isso é Configurações. */
function Destino({ dados }: { dados: SituacaoWhatsapp }) {
  const grupo = dados.grupo;
  return (
    <Bloco
      titulo="Destino dos avisos"
      tom={grupo ? "neutro" : "aviso"}
      acoes={
        <Link href="/reservas/configuracao" className={buttonVariants({ variant: "outline", size: "sm" })}>
          {grupo ? "Trocar" : "Escolher grupo"}
        </Link>
      }
    >
      {grupo ? (
        <>
          <Estados
            itens={[
              {
                rotulo: "Grupo",
                valor: descreverGrupo(grupo),
                detalhe: grupo.origem === "ambiente" ? "definido no servidor" : undefined,
              },
            ]}
          />
          {dados.estado === "conectado" && <EnviarTeste grupo={grupo} />}
        </>
      ) : (
        <p className="text-sm text-muted-foreground">
          Nenhum grupo escolhido: as reservas são gravadas e aparecem na tela, mas nada é publicado.
        </p>
      )}
    </Bloco>
  );
}

function EnviarTeste({ grupo }: { grupo: GrupoDestinoWhatsapp }) {
  const [confirmando, setConfirmando] = useState(false);
  const enviar = useMutation({
    mutationFn: () => api("/integracoes/whatsapp/teste", { metodo: "POST", json: {} }),
    onSuccess: () => {
      setConfirmando(false);
      toast.success("Mensagem de teste enviada");
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <>
      <Button variant="outline" className="self-start" onClick={() => setConfirmando(true)}>
        Enviar mensagem de teste
      </Button>
      <AlertDialog
        open={confirmando}
        onOpenChange={(aberto) => {
          if (!enviar.isPending) setConfirmando(aberto);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Enviar mensagem de teste?</AlertDialogTitle>
            <AlertDialogDescription>
              Todos os participantes de &quot;{descreverGrupo(grupo)}&quot; vão receber uma mensagem avisando que é um teste do
              notificador.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={enviar.isPending}>Cancelar</AlertDialogCancel>
            <AlertDialogAction disabled={enviar.isPending} onClick={() => enviar.mutate()}>
              {enviar.isPending ? "Enviando…" : "Enviar"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function Fila({ fila }: { fila: SituacaoWhatsapp["fila"] }) {
  const falhou = fila.falhou ?? 0;
  return (
    <Bloco titulo="Mensagens" tom={falhou > 0 ? "erro" : "neutro"}>
      <dl className="grid grid-cols-3 gap-4 text-sm">
        <div>
          <dt className="text-muted-foreground">Na fila</dt>
          <dd className="font-heading text-lg font-medium tabular-nums">{fila.pendente ?? 0}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Enviadas</dt>
          <dd className="font-heading text-lg font-medium tabular-nums">{fila.enviada ?? 0}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Falharam</dt>
          <dd className={`font-heading text-lg font-medium tabular-nums ${falhou > 0 ? "text-destructive" : ""}`}>
            {falhou}
          </dd>
        </div>
      </dl>
      {falhou > 0 && (
        <p className="text-sm text-muted-foreground">
          Mensagens que falharam 6 vezes não são mais tentadas: confira o grupo e os logs do serviço checkin-whatsapp.
        </p>
      )}
    </Bloco>
  );
}
