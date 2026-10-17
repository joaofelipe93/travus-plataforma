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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { useSessao } from "@/lib/sessao";
import { rotuloStatusMensagem } from "@/lib/boletos";
import type { EstadoWhatsapp, SituacaoWhatsappBoletos } from "@/lib/tipos";

// Conexão do WhatsApp **dos boletos**. É outro número: o do notificador de check-in publica num
// grupo, este fala no privado de cada cliente. Só admin — o QR dá acesso à conta.

const rotuloEstado: Record<EstadoWhatsapp, string> = {
  conectado: "conectado",
  aguardando_qr: "aguardando leitura do QR",
  conectando: "conectando",
  desconectado: "desconectado",
  indisponivel: "serviço fora do ar",
};

function formatarNumero(numero: string) {
  const so = numero.split(":")[0] ?? numero;
  const br = /^55(\d{2})(\d{4,5})(\d{4})$/.exec(so);
  return br ? `+55 ${br[1]} ${br[2]}-${br[3]}` : `+${so}`;
}

export default function PaginaWhatsappBoletos() {
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
  return <WhatsappBoletos />;
}

function WhatsappBoletos() {
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
    mutationFn: () => api<{ enfileirada: number }>("/integracoes/whatsapp-boletos/teste", { metodo: "POST", json: { telefone } }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["whatsapp-boletos"] });
      toast.success("Mensagem de teste na fila");
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

  return (
    <div className="flex max-w-3xl flex-col gap-6">
      <Alert>
        <AlertTitle>Este é o número que fala com os clientes</AlertTitle>
        <AlertDescription>
          Não é o mesmo do grupo de reservas. Pareie aqui o chip dedicado aos boletos — se você ler este QR
          com o número das reservas, os avisos de check-in param de sair.
        </AlertDescription>
      </Alert>

      {!dados.disponivel ? (
        <Alert variant="destructive">
          <AlertTitle>Serviço fora do ar</AlertTitle>
          <AlertDescription>{dados.motivo ?? "o serviço de envio não respondeu"}</AlertDescription>
        </Alert>
      ) : (
        <>
          <section className="flex flex-col gap-3 rounded-lg border p-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h2 className="font-heading font-semibold">Conexão</h2>
                <p className="text-sm text-muted-foreground">
                  <Badge variant={dados.estado === "conectado" ? "default" : "outline"}>
                    {rotuloEstado[dados.estado]}
                  </Badge>
                  {dados.numero && <span className="ml-2">{formatarNumero(dados.numero)}</span>}
                </p>
              </div>
              {dados.estado === "conectado" && (
                <>
                  <Button variant="outline" onClick={() => setConfirmandoDesconexao(true)}>
                    Desconectar
                  </Button>
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
                          A sessão é apagada e o número precisa ser pareado de novo. Os boletos que estiverem
                          na fila esperam até alguém parear.
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
                </>
              )}
            </div>

            {dados.qr && (
              <div className="flex flex-col items-start gap-2">
                <p className="text-sm text-muted-foreground">
                  No celular do chip dos boletos: WhatsApp → Dispositivos conectados → Conectar dispositivo.
                </p>
                <div className="rounded-lg bg-white p-3">
                  <QRCodeSVG value={dados.qr} size={240} />
                </div>
              </div>
            )}
          </section>

          <section className="flex flex-col gap-3 rounded-lg border p-4">
            <div>
              <h2 className="font-heading font-semibold">Situação do envio</h2>
              <p className="text-sm text-muted-foreground">
                Três chaves precisam estar ligadas para uma mensagem sair.
              </p>
            </div>
            <ul className="flex flex-col gap-1 text-sm">
              <li>
                Chave do servidor (<code>BOLETOS_ENVIO_HABILITADO</code>):{" "}
                <strong>{dados.envio_habilitado_servidor ? "ligada" : "desligada"}</strong>
              </li>
              <li>
                Configuração da tela de boletos:{" "}
                <strong>{dados.envio_habilitado_configuracao ? "ligada" : "desligada"}</strong>
              </li>
              <li>
                Serviço de envio: <strong>{dados.envio_habilitado_servico ? "ligado" : "desligado"}</strong>
              </li>
              <li>Pausa entre mensagens: {dados.pausa_envio_segundos}s</li>
            </ul>
            {Object.keys(dados.fila).length > 0 && (
              <p className="text-sm text-muted-foreground">
                Fila:{" "}
                {Object.entries(dados.fila)
                  .map(([status, n]) => `${n} ${rotuloStatusMensagem[status as keyof typeof rotuloStatusMensagem] ?? status}`)
                  .join(" · ")}
              </p>
            )}
          </section>

          <section className="flex flex-col gap-3 rounded-lg border p-4">
            <div>
              <h2 className="font-heading font-semibold">Mensagem de teste</h2>
              <p className="text-sm text-muted-foreground">
                Vai para um número de verdade (use o seu). Entra na mesma fila do envio, então respeita a
                pausa e as chaves acima.
              </p>
            </div>
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
          </section>
        </>
      )}
    </div>
  );
}
