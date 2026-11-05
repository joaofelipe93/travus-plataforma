"use client";

import { QRCodeSVG } from "qrcode.react";
import { Badge } from "@/components/ui/badge";
import type { Tom } from "@/components/painel";
import type { EstadoWhatsapp } from "@/lib/tipos";

// Peças comuns às duas conexões de WhatsApp da plataforma: a do notificador de reservas (publica
// num grupo) e a dos boletos (fala no privado dos clientes). São **números diferentes**, com
// serviços, filas e telas separadas de propósito — o que se compartilha aqui é só a aparência do
// estado e do QR, para as duas telas não divergirem com o tempo.

export const rotuloEstadoWhatsapp: Record<EstadoWhatsapp, string> = {
  conectado: "conectado",
  aguardando_qr: "aguardando leitura do QR",
  conectando: "conectando",
  desconectado: "desconectado",
  indisponivel: "serviço fora do ar",
};

export function tomDoEstadoWhatsapp(estado: EstadoWhatsapp): Tom {
  if (estado === "conectado") return "ok";
  if (estado === "indisponivel") return "erro";
  return "aviso";
}

/** O estado da conexão sempre com a mesma cor: conectado é verde, fora do ar é vermelho. */
export function BadgeEstadoWhatsapp({ estado, rotulo }: { estado: EstadoWhatsapp; rotulo?: string }) {
  const variante = estado === "conectado" ? "secondary" : estado === "indisponivel" ? "destructive" : "outline";
  return <Badge variant={variante}>{rotulo ?? rotuloEstadoWhatsapp[estado]}</Badge>;
}

/** "5511900000000" (ou "…:12@s.whatsapp.net") → "+55 11 90000-0000". */
export function formatarNumeroWhatsapp(numero: string) {
  const digitos = (numero.split(":")[0] ?? numero).replace(/\D/g, "");
  const br = /^55(\d{2})(\d{4,5})(\d{4})$/.exec(digitos);
  return br ? `+55 ${br[1]} ${br[2]}-${br[3]}` : `+${digitos}`;
}

/**
 * O QR do pareamento, com as instruções ao lado. Fundo branco e margem: leitor de QR falha no
 * tema escuro sem contraste.
 */
export function QrWhatsapp({ qr, children }: { qr: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-6 sm:flex-row sm:items-start">
      <div className="self-start rounded-lg bg-white p-4">
        <QRCodeSVG value={qr} size={240} marginSize={0} title="QR para conectar o WhatsApp" />
      </div>
      <div className="min-w-0 text-sm">{children}</div>
    </div>
  );
}
