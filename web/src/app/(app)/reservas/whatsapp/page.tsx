"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { QRCodeSVG } from "qrcode.react";
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
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { useSessao } from "@/lib/sessao";
import type { EstadoWhatsapp, GrupoDestinoWhatsapp, GrupoWhatsapp, SituacaoWhatsapp } from "@/lib/tipos";

// Conexão do notificador de check-in (reservas e cancelamentos no grupo do WhatsApp). Só admin:
// o QR dá acesso à conta. A API repassa tudo ao notificador; quem decide a permissão é ela.

const rotuloEstado: Record<EstadoWhatsapp, string> = {
  conectado: "conectado",
  aguardando_qr: "aguardando leitura do QR",
  conectando: "conectando",
  desconectado: "desconectado",
  indisponivel: "notificador fora do ar",
};

// Mensagens da API vêm em minúscula e sem ponto final: aqui viram frase.
function frase(texto: string) {
  const t = texto.trim();
  return t.charAt(0).toUpperCase() + t.slice(1) + (/[.!?]$/.test(t) ? "" : ".");
}

// "5511900000000" → "+55 11 90000-0000" (outros formatos só ganham o +).
function formatarNumero(numero: string) {
  const br = /^55(\d{2})(\d{4,5})(\d{4})$/.exec(numero);
  return br ? `+55 ${br[1]} ${br[2]}-${br[3]}` : `+${numero}`;
}

export default function PaginaWhatsapp() {
  const sessao = useSessao();
  const admin = sessao.data?.usuario.perfil === "admin";

  if (!admin) {
    return (
      <Alert>
        <AlertTitle>Só administradores</AlertTitle>
        <AlertDescription>A conexão do WhatsApp dá acesso à conta: peça a um administrador.</AlertDescription>
      </Alert>
    );
  }
  return <Whatsapp />;
}

function Whatsapp() {
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

  return (
    <div className="flex max-w-3xl flex-col gap-6">
      {situacao.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{situacao.error.message}</AlertDescription>
        </Alert>
      ) : !dados ? (
        <Skeleton className="h-64 w-full" />
      ) : (
        <>
          <Conexao dados={dados} />
          {dados.disponivel && <GrupoDestino dados={dados} />}
          {dados.disponivel && <Fila fila={dados.fila} />}
        </>
      )}
    </div>
  );
}

function Secao({ titulo, acao, children }: { titulo: string; acao?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="rounded-lg border p-5">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-medium">{titulo}</h2>
        {acao}
      </div>
      {children}
    </section>
  );
}

function Conexao({ dados }: { dados: SituacaoWhatsapp }) {
  const { estado } = dados;
  const badge =
    estado === "conectado" ? (
      <Badge variant="secondary">{rotuloEstado[estado]}</Badge>
    ) : estado === "indisponivel" ? (
      <Badge variant="destructive">{rotuloEstado[estado]}</Badge>
    ) : (
      <Badge variant="outline">{rotuloEstado[estado]}</Badge>
    );

  return (
    <Secao titulo="Conexão" acao={badge}>
      {estado === "indisponivel" && (
        <Alert variant="destructive">
          <AlertDescription>
            {frase(dados.motivo ?? "O notificador de check-in não respondeu.")} Enquanto ele não responder, reservas novas podem
            não ser avisadas no grupo.
          </AlertDescription>
        </Alert>
      )}

      {estado === "aguardando_qr" && dados.qr && (
        <div className="flex flex-col gap-6 sm:flex-row sm:items-start">
          {/* Fundo branco e margem: leitores de QR falham no tema escuro sem contraste. */}
          <div className="self-start rounded-lg bg-white p-4">
            <QRCodeSVG value={dados.qr} size={240} marginSize={0} title="QR para conectar o WhatsApp" />
          </div>
          <ol className="flex list-decimal flex-col gap-2 pl-5 text-sm">
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
        </div>
      )}

      {(estado === "conectando" || estado === "desconectado" || (estado === "aguardando_qr" && !dados.qr)) && (
        <p className="text-sm text-muted-foreground">Conectando ao WhatsApp… o QR aparece aqui em alguns segundos.</p>
      )}

      {estado === "conectado" && (
        <div className="flex flex-wrap items-center justify-between gap-4">
          <p className="text-sm">
            Conectado como <strong className="tabular-nums">{dados.numero ? formatarNumero(dados.numero) : "número desconhecido"}</strong>.
          </p>
          <Desconectar />
        </div>
      )}
    </Secao>
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
      queryClient.invalidateQueries({ queryKey: ["whatsapp"] });
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <>
      <Button variant="outline" onClick={() => setConfirmando(true)}>
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

function GrupoDestino({ dados }: { dados: SituacaoWhatsapp }) {
  const conectado = dados.estado === "conectado";
  const [escolhendo, setEscolhendo] = useState(false);
  const grupo = dados.grupo;

  return (
    <Secao
      titulo="Grupo que recebe as notificações"
      acao={
        conectado && (
          <div className="flex flex-wrap gap-2">
            {grupo && <EnviarTeste grupo={grupo} />}
            <Button variant={grupo ? "outline" : "default"} onClick={() => setEscolhendo((v) => !v)}>
              {escolhendo ? "Fechar lista" : grupo ? "Trocar grupo" : "Escolher grupo"}
            </Button>
          </div>
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
            As reservas são gravadas, mas nenhuma mensagem é enviada.{" "}
            {conectado ? "Escolha o grupo abaixo." : "Conecte o WhatsApp para escolher o grupo."}
          </AlertDescription>
        </Alert>
      )}

      {conectado && escolhendo && <ListaGrupos atual={grupo?.jid} aoEscolher={() => setEscolhendo(false)} />}
    </Secao>
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
      queryClient.invalidateQueries({ queryKey: ["whatsapp"] });
      aoEscolher();
    },
    onError: (e) => toast.error(e.message),
  });

  if (grupos.isError) {
    return (
      <Alert variant="destructive" className="mt-4">
        <AlertDescription>{grupos.error.message}</AlertDescription>
      </Alert>
    );
  }
  return (
    <div className="mt-4 rounded-lg border">
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
      <Button variant="outline" onClick={() => setConfirmando(true)}>
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
    <Secao titulo="Mensagens">
      <dl className="grid grid-cols-3 gap-4 text-sm">
        <div>
          <dt className="text-muted-foreground">Na fila</dt>
          <dd className="text-lg font-medium tabular-nums">{fila.pendente ?? 0}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Enviadas</dt>
          <dd className="text-lg font-medium tabular-nums">{fila.enviada ?? 0}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Falharam</dt>
          <dd className={`text-lg font-medium tabular-nums ${falhou > 0 ? "text-destructive" : ""}`}>{falhou}</dd>
        </div>
      </dl>
      {falhou > 0 && (
        <p className="mt-3 text-sm text-muted-foreground">
          Mensagens que falharam 6 vezes não são mais tentadas: confira o grupo e os logs do serviço checkin-whatsapp.
        </p>
      )}
    </Secao>
  );
}
