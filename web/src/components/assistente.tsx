"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowUpIcon, DatabaseIcon, Loader2Icon, RotateCcwIcon, SparklesIcon, SquareIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { RespostaMarkdown } from "@/components/resposta-markdown";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";
import { conversar, historicoParaEnviar, type EstadoAssistente, type EventoAssistente } from "@/lib/assistente";

type Fala = {
  id: number;
  papel: "usuario" | "assistente";
  texto: string;
  // Só do assistente: o que ele consultou, o que está consultando agora e como terminou.
  consultas?: string[];
  consultando?: string | null;
  situacao?: "respondendo" | "ok" | "erro" | "interrompida";
  erro?: string;
};

// A conversa sobrevive à troca de tela (fica no módulo), mas não à recarga: nada de dados de
// hóspedes guardados no navegador.
let conversaGuardada: Fala[] = [];
let proximoId = 1;

const sugestoesGerais = [
  "Quantos dry-runs foram feitos este mês e como terminaram?",
  "Quantos PDFs de comprovante já foram enviados ao Google Drive?",
  "Tem algum alerta ativo na plataforma agora?",
  "Quais foram as últimas execuções do Canopus?",
];
const sugestoesReservas = ["Quais reservas têm check-in amanhã?", "Quantas reservas chegaram hoje?"];

// Bloco do assistente na página Início: altura própria, as mensagens rolam dentro dele e a
// página continua rolando normalmente com os outros blocos embaixo.
export function Assistente() {
  const estado = useQuery({
    queryKey: ["assistente"],
    queryFn: () => api<EstadoAssistente>("/assistente"),
    staleTime: 60_000,
  });
  const [falas, setFalas] = useState<Fala[]>(conversaGuardada);
  const [texto, setTexto] = useState("");
  const controle = useRef<AbortController | null>(null);
  const rolagem = useRef<HTMLDivElement>(null);
  const campo = useRef<HTMLTextAreaElement>(null);
  const respondendo = falas.some((f) => f.situacao === "respondendo");

  useEffect(() => {
    conversaGuardada = falas;
  }, [falas]);
  // Rola só dentro do bloco (scrollIntoView arrastaria a página inteira junto).
  useEffect(() => {
    const r = rolagem.current;
    if (r) r.scrollTop = r.scrollHeight;
  }, [falas]);
  // Sair da tela no meio da resposta cancela a chamada.
  useEffect(() => () => controle.current?.abort(), []);

  function atualizarResposta(id: number, mudar: (f: Fala) => Fala) {
    setFalas((atual) => atual.map((f) => (f.id === id ? mudar(f) : f)));
  }

  async function perguntar(pergunta: string) {
    pergunta = pergunta.trim();
    if (!pergunta || respondendo) return;
    const historico = historicoParaEnviar([
      ...falas.map((f) => ({ papel: f.papel, texto: f.texto })),
      { papel: "usuario" as const, texto: pergunta },
    ]);
    const idResposta = proximoId + 1;
    proximoId += 2;
    setFalas((atual) => [
      ...atual,
      { id: idResposta - 1, papel: "usuario", texto: pergunta },
      { id: idResposta, papel: "assistente", texto: "", consultas: [], consultando: null, situacao: "respondendo" },
    ]);
    setTexto("");

    const c = new AbortController();
    controle.current = c;
    const aoEvento = (e: EventoAssistente) => {
      atualizarResposta(idResposta, (f) => {
        switch (e.tipo) {
          case "texto":
            return { ...f, texto: f.texto + e.texto, consultando: null };
          case "ferramenta":
            return {
              ...f,
              consultando: e.rotulo,
              consultas: f.consultas?.includes(e.rotulo) ? f.consultas : [...(f.consultas ?? []), e.rotulo],
            };
          case "erro":
            return { ...f, situacao: "erro", erro: e.mensagem, consultando: null };
          case "fim":
            return { ...f, situacao: "ok", consultando: null };
        }
      });
    };
    try {
      await conversar(historico, aoEvento, c.signal);
    } catch (e) {
      const interrompida = c.signal.aborted;
      atualizarResposta(idResposta, (f) => ({
        ...f,
        consultando: null,
        situacao: interrompida ? "interrompida" : "erro",
        erro: interrompida ? undefined : e instanceof Error ? e.message : String(e),
      }));
    } finally {
      // Fluxo que terminou sem "fim" (conexão caiu): não fica girando.
      atualizarResposta(idResposta, (f) =>
        f.situacao === "respondendo" ? { ...f, situacao: "erro", erro: "A conexão caiu antes do fim da resposta." } : f,
      );
      controle.current = null;
      campo.current?.focus();
    }
  }

  function novaConversa() {
    controle.current?.abort();
    setFalas([]);
    campo.current?.focus();
  }

  const sugestoes = estado.data?.reservas ? [...sugestoesReservas, ...sugestoesGerais] : sugestoesGerais;
  const indisponivel = estado.data && !estado.data.disponivel;

  return (
    <section
      aria-labelledby="titulo-assistente"
      className="flex h-[min(620px,calc(100svh-11rem))] min-h-[420px] flex-col overflow-hidden rounded-xl border bg-card"
    >
      <header className="flex shrink-0 items-center justify-between gap-4 border-b px-5 py-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <SparklesIcon className="size-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0">
            <h2 id="titulo-assistente" className="text-sm font-semibold">
              Assistente
            </h2>
            <p className="truncate text-xs text-muted-foreground">
              Pergunte sobre o Canopus{estado.data?.reservas ? ", as reservas" : ""} e a saúde da plataforma. Ele só consulta.
            </p>
          </div>
        </div>
        {falas.length > 0 && (
          <Button variant="ghost" size="sm" onClick={novaConversa}>
            <RotateCcwIcon />
            Nova conversa
          </Button>
        )}
      </header>

      <div ref={rolagem} className="min-h-0 flex-1 overflow-y-auto">
        <div className="flex flex-col gap-6 px-5 py-5">
          {indisponivel && (
            <Alert>
              <AlertTitle>O assistente ainda não foi configurado</AlertTitle>
              <AlertDescription>
                Falta a chave da Anthropic no servidor (ANTHROPIC_API_KEY no deploy/.env). Peça a um administrador.
              </AlertDescription>
            </Alert>
          )}

          {falas.length === 0 && !indisponivel && (
            <div className="flex flex-col gap-3">
              <p className="text-sm text-muted-foreground">
                Execuções, lances, comprovantes no Drive{estado.data?.reservas ? ", reservas" : ""} e alertas: ele consulta os
                dados na hora. Comece por uma destas:
              </p>
              <div className="grid gap-2 sm:grid-cols-2">
                {sugestoes.map((s) => (
                  <button
                    key={s}
                    type="button"
                    disabled={!estado.data?.disponivel}
                    onClick={() => perguntar(s)}
                    className="rounded-lg border bg-background/40 px-3.5 py-2.5 text-left text-sm text-muted-foreground transition-colors outline-none hover:border-muted-foreground/40 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
                  >
                    {s}
                  </button>
                ))}
              </div>
            </div>
          )}

          {falas.map((f) =>
            f.papel === "usuario" ? (
              <div key={f.id} className="flex justify-end">
                <p className="max-w-[85%] rounded-2xl rounded-br-md bg-secondary px-4 py-2.5 text-sm whitespace-pre-wrap">{f.texto}</p>
              </div>
            ) : (
              <Resposta key={f.id} fala={f} />
            ),
          )}
        </div>
      </div>

      <div className="shrink-0 border-t px-5 pt-3 pb-3">
        <form
          className="flex w-full items-end gap-2 rounded-lg border bg-background/60 p-1.5 focus-within:border-ring/60"
          onSubmit={(e) => {
            e.preventDefault();
            perguntar(texto);
          }}
        >
          <label htmlFor="pergunta" className="sr-only">
            Pergunta ao assistente
          </label>
          <textarea
            id="pergunta"
            ref={campo}
            value={texto}
            onChange={(e) => setTexto(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault();
                perguntar(texto);
              }
            }}
            rows={1}
            maxLength={2000}
            disabled={indisponivel}
            placeholder={indisponivel ? "Assistente indisponível" : "Pergunte algo… (Enter envia, Shift+Enter quebra a linha)"}
            className="field-sizing-content max-h-32 min-h-9 flex-1 resize-none bg-transparent px-2 py-2 text-sm outline-none placeholder:text-muted-foreground disabled:opacity-50"
          />
          {respondendo ? (
            <Button type="button" size="icon" variant="secondary" onClick={() => controle.current?.abort()} aria-label="Parar a resposta">
              <SquareIcon className="fill-current" />
            </Button>
          ) : (
            <Button type="submit" size="icon" disabled={!texto.trim() || !estado.data?.disponivel} aria-label="Enviar pergunta">
              <ArrowUpIcon />
            </Button>
          )}
        </form>
        <p className="mt-2 text-center text-[0.7rem] text-muted-foreground">
          O assistente pode errar. Confira números importantes nas telas de cada serviço.
        </p>
      </div>
    </section>
  );
}

function Resposta({ fala }: { fala: Fala }) {
  const esperando = fala.situacao === "respondendo" && !fala.texto;
  return (
    <div className="flex flex-col gap-2" aria-live={fala.situacao === "respondendo" ? "polite" : undefined}>
      {fala.consultas && fala.consultas.length > 0 && (
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <DatabaseIcon className="size-3.5" />
          {fala.consultas.join(" · ")}
        </p>
      )}
      {fala.texto && <RespostaMarkdown texto={fala.texto} />}
      {(esperando || (fala.situacao === "respondendo" && fala.consultando)) && (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2Icon className="size-4 animate-spin" />
          {fala.consultando ? `${fala.consultando}…` : "Pensando…"}
        </p>
      )}
      {fala.situacao === "erro" && <p className="text-sm text-destructive">{fala.erro}</p>}
      {fala.situacao === "interrompida" && <p className="text-xs text-muted-foreground">Resposta interrompida.</p>}
    </div>
  );
}
