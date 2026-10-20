"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { BadgeEstadoWhatsapp } from "@/components/conexao-whatsapp";
import { Bloco, Estados } from "@/components/painel";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { useSessao } from "@/lib/sessao";
import type { GrupoDestinoWhatsapp, GrupoWhatsapp, SituacaoWhatsapp } from "@/lib/tipos";

// Configuração do serviço de Reservas: **para qual grupo do WhatsApp** vão os avisos. É a única
// decisão que a tela toma neste serviço — os horários do resumo diário (08h "para Hoje" e 17h
// "para Amanhã") e a deduplicação são do notificador, e o webhook é cadastrado no PMS.
//
// Só admin: escolher o grupo muda para onde a plataforma publica dados de hóspedes.

export default function PaginaConfiguracaoReservas() {
  const sessao = useSessao();
  if (sessao.data?.usuario.perfil !== "admin") {
    return (
      <Alert>
        <AlertTitle>Só administradores</AlertTitle>
        <AlertDescription>
          O grupo que recebe os avisos é escolhido por um administrador: os avisos levam nome e telefone de
          hóspedes.
        </AlertDescription>
      </Alert>
    );
  }
  return <Configuracao />;
}

function Configuracao() {
  const situacao = useQuery({
    queryKey: ["whatsapp"],
    queryFn: () => api<SituacaoWhatsapp>("/integracoes/whatsapp"),
    refetchInterval: 15000,
  });
  const dados = situacao.data;

  if (situacao.isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{situacao.error.message}</AlertDescription>
      </Alert>
    );
  }
  if (!dados) {
    return <Skeleton className="h-72 w-full max-w-3xl" />;
  }

  return (
    <div className="flex max-w-3xl flex-col gap-8">
      <GrupoDestino dados={dados} />

      <Bloco
        titulo="Quando o grupo é avisado"
        descricao="Os horários são do notificador e não se mudam pela tela."
      >
        <Estados
          itens={[
            { rotulo: "Resumo da manhã", valor: "08h", detalhe: "reservas confirmadas para hoje" },
            { rotulo: "Resumo da tarde", valor: "17h", detalhe: "reservas confirmadas para amanhã" },
            {
              rotulo: "Avisos avulsos",
              valor: "na hora",
              detalhe: "reserva ou cancelamento que chega depois do resumo do dia, e check-in realizado",
            },
          ]}
        />
        <p className="text-sm text-muted-foreground">
          Reserva que chega antes do resumo entra nele, em vez de virar uma mensagem solta. Payload sem id da
          reserva e sem data de check-in não vira mensagem: fica gravado e quem avisa é o vigia.
        </p>
      </Bloco>
    </div>
  );
}

function descreverGrupo(grupo: GrupoDestinoWhatsapp) {
  return grupo.nome ?? grupo.jid;
}

function GrupoDestino({ dados }: { dados: SituacaoWhatsapp }) {
  const conectado = dados.estado === "conectado";
  const [escolhendo, setEscolhendo] = useState(false);
  const grupo = dados.grupo;

  return (
    <Bloco
      titulo="Grupo que recebe os avisos"
      descricao="Reservas, cancelamentos e check-ins são publicados nele."
      tom={grupo ? "neutro" : "aviso"}
      acoes={
        conectado ? (
          <Button variant={grupo ? "outline" : "default"} onClick={() => setEscolhendo((v) => !v)}>
            {escolhendo ? "Fechar lista" : grupo ? "Trocar grupo" : "Escolher grupo"}
          </Button>
        ) : (
          <>
            <BadgeEstadoWhatsapp estado={dados.estado} />
            <Link href="/reservas/conexoes" className={buttonVariants({ variant: "outline", size: "sm" })}>
              Conectar
            </Link>
          </>
        )
      }
    >
      {grupo ? (
        <div className="text-sm">
          <p className="font-medium">{descreverGrupo(grupo)}</p>
          <p className="font-mono text-xs text-muted-foreground">{grupo.jid}</p>
          {grupo.origem === "ambiente" && (
            <p className="mt-2 text-muted-foreground">
              Definido no servidor (CHECKIN_WHATSAPP_GROUP_JID). Escolher um grupo aqui passa a valer no lugar dele.
            </p>
          )}
        </div>
      ) : (
        <Alert>
          <AlertTitle>Nenhum grupo escolhido</AlertTitle>
          <AlertDescription>
            As reservas são gravadas e aparecem na tela, mas nenhuma mensagem é enviada.{" "}
            {conectado ? "Escolha o grupo abaixo." : "Conecte o WhatsApp em Conexões para escolher o grupo."}
          </AlertDescription>
        </Alert>
      )}

      {conectado && escolhendo && <ListaGrupos atual={grupo?.jid} aoEscolher={() => setEscolhendo(false)} />}
    </Bloco>
  );
}

function ListaGrupos({ atual, aoEscolher }: { atual?: string; aoEscolher: () => void }) {
  const queryClient = useQueryClient();
  const grupos = useQuery({
    queryKey: ["whatsapp", "grupos"],
    queryFn: () => api<{ grupos: GrupoWhatsapp[] }>("/integracoes/whatsapp/grupos"),
  });
  const definir = useMutation({
    mutationFn: (g: GrupoWhatsapp) => api("/integracoes/whatsapp/grupo", { metodo: "PUT", json: { jid: g.jid } }),
    onSuccess: (_, g) => {
      toast.success(`As notificações vão para "${g.nome}"`);
      void queryClient.invalidateQueries({ queryKey: ["whatsapp"] });
      aoEscolher();
    },
    onError: (e) => toast.error(e.message),
  });

  if (grupos.isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{grupos.error.message}</AlertDescription>
      </Alert>
    );
  }
  return (
    <div className="rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Grupo</TableHead>
            <TableHead className="text-right">Participantes</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {grupos.isPending && (
            <TableRow>
              <TableCell colSpan={3}>
                <Skeleton className="h-5 w-full" />
              </TableCell>
            </TableRow>
          )}
          {grupos.data?.grupos.length === 0 && (
            <TableRow>
              <TableCell colSpan={3} className="py-6 text-center text-muted-foreground">
                Este número não participa de nenhum grupo. Adicione-o ao grupo no celular e abra a lista de novo.
              </TableCell>
            </TableRow>
          )}
          {grupos.data?.grupos.map((g) => (
            <TableRow key={g.jid}>
              <TableCell>{g.nome}</TableCell>
              <TableCell className="text-right tabular-nums">{g.participantes}</TableCell>
              <TableCell className="text-right">
                {g.jid === atual ? (
                  <Badge variant="secondary">em uso</Badge>
                ) : (
                  <Button size="sm" variant="outline" disabled={definir.isPending} onClick={() => definir.mutate(g)}>
                    Usar este grupo
                  </Button>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
