"use client";

// Um cartão por reserva (tela Reservas): imóvel e canal no topo, a estadia em destaque, o
// hóspede e os números no meio, e no rodapé quando chegou e se o aviso saiu no grupo. A
// reserva cancelada fica apagada, com o motivo; a que acabou de chegar ganha um destaque.

import { cn } from "cn";
import {
  Ban,
  BedDouble,
  CheckCheck,
  Clock,
  ExternalLink,
  Mail,
  MessageCircleOff,
  Phone,
  TriangleAlert,
  Users,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import {
  canal as canalDe,
  formatarTelefone,
  haQuanto,
  linkWhatsapp,
  type Momento,
  momento as momentoDe,
  partesDoDia,
  reais,
} from "@/lib/reservas";
import type { MensagemReserva, Reserva } from "@/lib/tipos";

const SELO: Record<Momento, { rotulo: string; classe: string } | null> = {
  cancelada: { rotulo: "Cancelada", classe: "bg-destructive/15 text-destructive" },
  chega_hoje: { rotulo: "Chega hoje", classe: "bg-sucesso/20 text-sucesso" },
  hospedado: { rotulo: "Hospedado", classe: "bg-sucesso/10 text-sucesso" },
  sai_hoje: { rotulo: "Sai hoje", classe: "bg-aviso/15 text-aviso" },
  futura: { rotulo: "Confirmada", classe: "bg-accent text-accent-foreground" },
  encerrada: { rotulo: "Encerrada", classe: "bg-muted text-muted-foreground" },
  sem_data: null,
};

// Faixa colorida à esquerda: lê o estado do cartão de longe, antes do texto.
const FAIXA: Record<Momento, string> = {
  cancelada: "before:bg-destructive/60",
  chega_hoje: "before:bg-sucesso",
  hospedado: "before:bg-sucesso/50",
  sai_hoje: "before:bg-aviso",
  futura: "before:bg-chart-2",
  encerrada: "before:bg-border",
  sem_data: "before:bg-border",
};

const AVISO: Record<MensagemReserva, { rotulo: string; Icone: typeof CheckCheck; classe: string }> = {
  enviada: { rotulo: "avisado no grupo", Icone: CheckCheck, classe: "text-sucesso" },
  pendente: { rotulo: "aviso na fila", Icone: Clock, classe: "text-aviso" },
  falhou: { rotulo: "aviso falhou", Icone: TriangleAlert, classe: "text-destructive" },
  sem_mensagem: { rotulo: "sem aviso (nenhum grupo escolhido)", Icone: MessageCircleOff, classe: "text-muted-foreground" },
};

export function CartaoReserva({ reserva: r, hoje, nova = false }: { reserva: Reserva; hoje: string; nova?: boolean }) {
  const m = momentoDe(r, hoje);
  const selo = SELO[m];
  const canal = canalDe(r.canal);
  const cancelada = m === "cancelada";
  const whats = r.telefone ? linkWhatsapp(r.telefone) : null;
  const aviso = AVISO[cancelada && r.mensagem_cancelamento ? r.mensagem_cancelamento : r.mensagem];

  return (
    <article
      className={cn(
        "relative flex flex-col gap-4 overflow-hidden rounded-xl border border-border/70 bg-card p-5 pl-6 transition-shadow duration-700",
        "before:absolute before:inset-y-0 before:left-0 before:w-1",
        FAIXA[m],
        cancelada && "bg-card/60",
        m === "encerrada" && "opacity-75",
        nova && "shadow-[0_0_0_2px_var(--ouro),0_0_28px_-6px_var(--ouro)] motion-safe:animate-in motion-safe:fade-in motion-safe:zoom-in-95",
      )}
      aria-label={`Reserva de ${r.hospede ?? "hóspede sem nome"}${r.imovel ? ` em ${r.imovel}` : ""}`}
    >
      <header className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className={cn("truncate font-heading text-lg leading-tight", cancelada && "text-muted-foreground")}>
            {r.imovel ?? "Imóvel não informado"}
          </h3>
          {canal && (
            <span className={cn("mt-1.5 inline-flex rounded-full px-2 py-0.5 text-xs font-medium", canal.cor)}>{canal.nome}</span>
          )}
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1.5">
          {nova && <Badge className="bg-ouro text-primary-foreground">Nova</Badge>}
          {selo && <span className={cn("rounded-full px-2.5 py-0.5 text-xs font-medium", selo.classe)}>{selo.rotulo}</span>}
        </div>
      </header>

      <Estadia reserva={r} riscada={cancelada} />

      <div className="flex flex-col gap-2">
        <p className={cn("truncate text-base font-medium", cancelada && "text-muted-foreground")}>
          {r.hospede ?? "Hóspede não informado"}
        </p>
        <ul className="flex flex-wrap gap-x-4 gap-y-1.5 text-sm text-muted-foreground">
          {r.hospedes !== null && (
            <li className="flex items-center gap-1.5">
              <Users className="size-3.5" aria-hidden />
              {r.hospedes} {r.hospedes === 1 ? "hóspede" : "hóspedes"}
            </li>
          )}
          {r.valor_centavos !== null && (
            <li className={cn("tabular-nums text-foreground", cancelada && "text-muted-foreground line-through")}>
              {reais(r.valor_centavos)}
            </li>
          )}
        </ul>
        {(r.telefone || r.email) && (
          <ul className="flex flex-wrap gap-x-4 gap-y-1.5 text-sm">
            {r.telefone && (
              <li>
                {whats ? (
                  <a
                    href={whats}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-1.5 text-muted-foreground hover:text-foreground focus-visible:text-foreground"
                    title="Abrir conversa no WhatsApp"
                  >
                    <Phone className="size-3.5" aria-hidden />
                    <span className="tabular-nums">{formatarTelefone(r.telefone)}</span>
                  </a>
                ) : (
                  <span className="flex items-center gap-1.5 text-muted-foreground">
                    <Phone className="size-3.5" aria-hidden />
                    {r.telefone}
                  </span>
                )}
              </li>
            )}
            {r.email && (
              <li className="flex min-w-0 items-center gap-1.5 text-muted-foreground">
                <Mail className="size-3.5 shrink-0" aria-hidden />
                <span className="truncate">{r.email}</span>
              </li>
            )}
          </ul>
        )}
      </div>

      {cancelada && r.motivo_cancelamento && (
        <p className="flex items-start gap-2 rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <Ban className="mt-0.5 size-3.5 shrink-0" aria-hidden />
          {r.motivo_cancelamento}
        </p>
      )}
      {!r.reconhecida && (
        <p className="flex items-start gap-2 rounded-lg bg-aviso/10 px-3 py-2 text-sm text-aviso">
          <TriangleAlert className="mt-0.5 size-3.5 shrink-0" aria-hidden />
          Formato não reconhecido: o PMS mandou campos novos.
        </p>
      )}

      <footer className="mt-auto flex flex-wrap items-center justify-between gap-2 border-t border-border/60 pt-3 text-xs text-muted-foreground">
        <span title={new Date(cancelada && r.cancelada_em ? r.cancelada_em : r.recebida_em).toLocaleString("pt-BR")}>
          {cancelada && r.cancelada_em ? `cancelada ${haQuanto(r.cancelada_em)}` : `recebida ${haQuanto(r.recebida_em)}`}
        </span>
        <span className="flex items-center gap-3">
          {r.link_precheckin && !cancelada && (
            <a
              href={r.link_precheckin}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-1 hover:text-foreground focus-visible:text-foreground"
            >
              pré-check-in <ExternalLink className="size-3" aria-hidden />
            </a>
          )}
          <span className={cn("flex items-center gap-1", aviso.classe)} title={aviso.rotulo}>
            <aviso.Icone className="size-3.5" aria-hidden />
            <span className="sr-only sm:not-sr-only">{aviso.rotulo}</span>
          </span>
        </span>
      </footer>
    </article>
  );
}

function Estadia({ reserva: r, riscada }: { reserva: Reserva; riscada: boolean }) {
  if (!r.check_in) {
    return <p className="text-sm text-muted-foreground">Datas não informadas</p>;
  }
  const entrada = partesDoDia(r.check_in);
  const saida = r.check_out ? partesDoDia(r.check_out) : null;
  return (
    <div className={cn("flex items-center gap-3 rounded-lg bg-background/40 px-3 py-2.5", riscada && "opacity-60")}>
      <Dia rotulo="entrada" partes={entrada} riscada={riscada} />
      <div className="flex flex-1 flex-col items-center gap-1 text-xs text-muted-foreground">
        <span className="h-px w-full bg-border" aria-hidden />
        {r.noites !== null && (
          <span className="flex items-center gap-1">
            <BedDouble className="size-3.5" aria-hidden />
            {r.noites} {r.noites === 1 ? "noite" : "noites"}
          </span>
        )}
      </div>
      {saida ? <Dia rotulo="saída" partes={saida} riscada={riscada} /> : <span className="text-sm text-muted-foreground">sem saída</span>}
    </div>
  );
}

function Dia({ rotulo, partes, riscada }: { rotulo: string; partes: ReturnType<typeof partesDoDia>; riscada: boolean }) {
  return (
    <div className="flex flex-col items-center leading-none">
      <span className="text-[0.65rem] tracking-wider text-muted-foreground uppercase">{rotulo}</span>
      <span className={cn("mt-1 font-heading text-2xl tabular-nums", riscada && "line-through decoration-1")}>{partes.dia}</span>
      <span className="mt-0.5 text-xs text-muted-foreground">
        {partes.mes} · {partes.semana}
      </span>
    </div>
  );
}
