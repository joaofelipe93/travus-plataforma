// Regras de leitura das reservas (tela Reservas). As datas da estadia vêm em AAAA-MM-DD, sem
// fuso: são comparadas como texto com o "hoje" do navegador, que também é AAAA-MM-DD local.

import type { Reserva } from "./tipos";

// Onde a reserva está em relação a hoje. Cancelada vence tudo.
export type Momento = "cancelada" | "chega_hoje" | "hospedado" | "sai_hoje" | "futura" | "encerrada" | "sem_data";

export function diaLocal(data = new Date()): string {
  const d = new Date(data.getTime() - data.getTimezoneOffset() * 60_000);
  return d.toISOString().slice(0, 10);
}

export function somarDias(dia: string, dias: number): string {
  const [a, m, d] = dia.split("-").map(Number);
  return diaLocal(new Date(a, m - 1, d + dias));
}

export function momento(r: Reserva, hoje: string): Momento {
  if (r.situacao === "cancelada") return "cancelada";
  if (!r.check_in) return "sem_data";
  if (r.check_in === hoje) return "chega_hoje";
  if (r.check_out === hoje) return "sai_hoje";
  if (r.check_in > hoje) return "futura";
  if (r.check_out && r.check_out > hoje) return "hospedado";
  return "encerrada";
}

// "2026-09-11" → Date local (meia-noite), para formatar sem escorregar de dia pelo fuso.
export function dataDoDia(dia: string): Date {
  const [a, m, d] = dia.split("-").map(Number);
  return new Date(a, m - 1, d);
}

const fmtDia = new Intl.DateTimeFormat("pt-BR", { day: "2-digit" });
const fmtMes = new Intl.DateTimeFormat("pt-BR", { month: "short" });
const fmtSemana = new Intl.DateTimeFormat("pt-BR", { weekday: "short" });

export function partesDoDia(dia: string) {
  const d = dataDoDia(dia);
  return {
    dia: fmtDia.format(d),
    mes: fmtMes.format(d).replace(".", ""),
    semana: fmtSemana.format(d).replace(".", ""),
  };
}

const fmtReais = new Intl.NumberFormat("pt-BR", { style: "currency", currency: "BRL" });

export function reais(centavos: number) {
  return fmtReais.format(centavos / 100);
}

const fmtRelativo = new Intl.RelativeTimeFormat("pt-BR", { numeric: "auto" });

// "há 5 minutos", "ontem"… para quando a reserva chegou.
export function haQuanto(iso: string, agora = Date.now()): string {
  const s = Math.round((new Date(iso).getTime() - agora) / 1000);
  const abs = Math.abs(s);
  if (abs < 45) return "agora mesmo";
  if (abs < 3600) return fmtRelativo.format(Math.round(s / 60), "minute");
  if (abs < 86_400) return fmtRelativo.format(Math.round(s / 3600), "hour");
  if (abs < 30 * 86_400) return fmtRelativo.format(Math.round(s / 86_400), "day");
  return new Date(iso).toLocaleDateString("pt-BR");
}

// "5584999990000" → "+55 84 99999-0000" (outros formatos passam como vieram).
export function formatarTelefone(numero: string) {
  const digitos = numero.replace(/\D/g, "");
  const br = /^55(\d{2})(\d{4,5})(\d{4})$/.exec(digitos);
  return br ? `+55 ${br[1]} ${br[2]}-${br[3]}` : numero;
}

export function linkWhatsapp(numero: string) {
  const digitos = numero.replace(/\D/g, "");
  return digitos.length >= 10 ? `https://wa.me/${digitos}` : null;
}

// Canais conhecidos ganham nome e cor próprios; o resto aparece como veio, capitalizado.
const CANAIS: Record<string, { nome: string; cor: string }> = {
  airbnb: { nome: "Airbnb", cor: "bg-[#ff5a5f]/15 text-[#ff8a8e]" },
  booking: { nome: "Booking", cor: "bg-[#3b82f6]/15 text-[#8db8ff]" },
  "booking.com": { nome: "Booking", cor: "bg-[#3b82f6]/15 text-[#8db8ff]" },
  expedia: { nome: "Expedia", cor: "bg-[#facc15]/15 text-[#fde68a]" },
  direto: { nome: "Direto", cor: "bg-sucesso/15 text-sucesso" },
  direct: { nome: "Direto", cor: "bg-sucesso/15 text-sucesso" },
};

export function canal(nome: string | null) {
  if (!nome) return null;
  return CANAIS[nome.toLowerCase()] ?? { nome: nome.charAt(0).toUpperCase() + nome.slice(1), cor: "bg-accent text-accent-foreground" };
}

// Texto que a busca procura: hóspede, imóvel, telefone, e-mail e código.
export function textoDeBusca(r: Reserva) {
  return [r.hospede, r.imovel, r.telefone, r.email, r.chave, r.canal]
    .filter(Boolean)
    .join(" ")
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase();
}

export function normalizarBusca(texto: string) {
  return texto
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .trim();
}
