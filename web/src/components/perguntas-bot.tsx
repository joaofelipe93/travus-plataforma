"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import { campoDaPergunta } from "@/lib/atendimento";
import type { PerguntaBot } from "@/lib/tipos";

// As perguntas de um atendimento, na ordem (decisão do usuário em 29/10/2026, no lugar das
// "etapas com objetivo e campos", que ele não gostou de montar).
//
// A ideia é a de uma lista: **o que este atendimento precisa descobrir, de cima para baixo**. O
// bot faz uma pergunta por vez, com as palavras dele, conforme a conversa — por isso o texto
// aqui é o que se quer saber, não a frase que vai ser enviada.
//
// Cada pergunta guarda a resposta num "dado", que é o que aparece no lead e no cadastro. O nome
// do dado sai do texto da pergunta e fica escondido até alguém querer mexer: é detalhe técnico,
// não decisão de quem escreve o atendimento.

type Rascunho = PerguntaBot & { chave: string; dadoEditado: boolean };

let sequencia = 0;
function novaChave(): string {
  sequencia += 1;
  return `p${sequencia}`;
}

function paraRascunho(perguntas: PerguntaBot[]): Rascunho[] {
  return perguntas.map((q) => ({ ...q, chave: novaChave(), dadoEditado: true }));
}

export function PerguntasDoBot({
  botId,
  perguntas,
  podeEditar,
}: {
  botId: number;
  perguntas: PerguntaBot[];
  podeEditar: boolean;
}) {
  const queryClient = useQueryClient();
  const [rascunho, setRascunho] = useState<Rascunho[]>(() => paraRascunho(perguntas));
  const [mexido, setMexido] = useState(false);
  const [mostrarDados, setMostrarDados] = useState(false);

  function alterar(proximo: Rascunho[]) {
    setRascunho(proximo);
    setMexido(true);
  }

  const salvar = useMutation({
    mutationFn: () =>
      api<{ perguntas: PerguntaBot[] }>(`/atendimento/bots/${botId}/perguntas`, {
        metodo: "PUT",
        json: {
          perguntas: rascunho.map((q) => ({
            texto: q.texto,
            campo: q.campo.trim() === "" ? campoDaPergunta(q.texto) : q.campo,
            orientacao: q.orientacao?.trim() === "" ? null : q.orientacao,
            obrigatoria: q.obrigatoria,
          })),
        },
      }),
    onSuccess: (res) => {
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bot", botId] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bots"] });
      setRascunho(paraRascunho(res.perguntas));
      setMexido(false);
      toast.success(res.perguntas.length === 0 ? "Perguntas apagadas" : "Perguntas salvas");
    },
    onError: (e) => toast.error(e.message),
  });

  function acrescentar() {
    alterar([
      ...rascunho,
      { chave: novaChave(), id: 0, ordem: rascunho.length + 1, texto: "", campo: "", orientacao: null, obrigatoria: true, dadoEditado: false },
    ]);
  }

  function mover(indice: number, direcao: -1 | 1) {
    const destino = indice + direcao;
    if (destino < 0 || destino >= rascunho.length) return;
    const copia = [...rascunho];
    [copia[indice], copia[destino]] = [copia[destino], copia[indice]];
    alterar(copia);
  }

  return (
    <div className="flex flex-col gap-4">
      {rascunho.length === 0 ? (
        <Alert>
          <AlertDescription>
            Sem perguntas, o bot só conversa: responde o que as instruções permitem e encerra quando
            o assunto acaba. Para ele <strong>descobrir</strong> algo de quem escreve — o nome da
            empresa, quantas pessoas, o melhor horário —, acrescente perguntas.
          </AlertDescription>
        </Alert>
      ) : (
        <ol className="flex flex-col gap-3">
          {rascunho.map((q, i) => (
            <li
              key={q.chave}
              className="flex flex-col gap-3 rounded-lg border bg-card/50 p-4"
            >
              <div className="flex items-start gap-3">
                <span className="mt-2 w-6 shrink-0 text-right font-heading text-sm text-muted-foreground tabular-nums">
                  {i + 1}.
                </span>
                <div className="flex min-w-0 flex-1 flex-col gap-2">
                  <Label htmlFor={`pergunta-${q.chave}`} className="sr-only">
                    Pergunta {i + 1}
                  </Label>
                  <Input
                    id={`pergunta-${q.chave}`}
                    value={q.texto}
                    onChange={(e) => {
                      const texto = e.target.value;
                      alterar(
                        rascunho.map((x, j) =>
                          j === i
                            ? { ...x, texto, campo: x.dadoEditado ? x.campo : campoDaPergunta(texto) }
                            : x,
                        ),
                      );
                    }}
                    placeholder="O que o bot precisa descobrir? Ex.: Quantas pessoas entrariam no plano?"
                    disabled={!podeEditar}
                  />
                  {mostrarDados && (
                    <div className="flex flex-wrap items-center gap-2">
                      <Label htmlFor={`dado-${q.chave}`} className="text-xs text-muted-foreground">
                        Guardar em
                      </Label>
                      <Input
                        id={`dado-${q.chave}`}
                        value={q.campo}
                        onChange={(e) =>
                          alterar(
                            rascunho.map((x, j) =>
                              j === i ? { ...x, campo: e.target.value, dadoEditado: true } : x,
                            ),
                          )
                        }
                        className="h-8 w-56 font-mono text-xs"
                        placeholder={campoDaPergunta(q.texto) || "nome_do_dado"}
                        disabled={!podeEditar}
                      />
                    </div>
                  )}
                  <Textarea
                    value={q.orientacao ?? ""}
                    onChange={(e) =>
                      alterar(rascunho.map((x, j) => (j === i ? { ...x, orientacao: e.target.value } : x)))
                    }
                    rows={2}
                    placeholder="Orientação (opcional): o que fazer com certas respostas, o que não prometer…"
                    className="text-sm"
                    disabled={!podeEditar}
                  />
                </div>
                {podeEditar && (
                  <div className="flex shrink-0 flex-col gap-1">
                    <Button type="button" variant="ghost" size="sm" onClick={() => mover(i, -1)} disabled={i === 0}>
                      ↑
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => mover(i, 1)}
                      disabled={i === rascunho.length - 1}
                    >
                      ↓
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => alterar(rascunho.filter((_, j) => j !== i))}
                    >
                      ✕
                    </Button>
                  </div>
                )}
              </div>
              <div className="flex items-center gap-3 pl-9">
                <Switch
                  id={`obrigatoria-${q.chave}`}
                  checked={q.obrigatoria}
                  onCheckedChange={(v) =>
                    alterar(rascunho.map((x, j) => (j === i ? { ...x, obrigatoria: v } : x)))
                  }
                  disabled={!podeEditar}
                />
                <Label htmlFor={`obrigatoria-${q.chave}`} className="text-sm text-muted-foreground">
                  {q.obrigatoria
                    ? "Obrigatória: o atendimento não termina sem isso"
                    : "Opcional: se a pessoa não responder, o bot segue"}
                </Label>
              </div>
            </li>
          ))}
        </ol>
      )}

      {podeEditar && (
        <div className="flex flex-wrap items-center gap-3">
          <Button type="button" variant="outline" onClick={acrescentar}>
            Acrescentar pergunta
          </Button>
          <Button type="button" onClick={() => salvar.mutate()} disabled={!mexido || salvar.isPending}>
            {salvar.isPending ? "Salvando…" : "Salvar perguntas"}
          </Button>
          {mexido && !salvar.isPending && (
            <span className="text-sm text-aviso">Há perguntas não salvas.</span>
          )}
          <button
            type="button"
            onClick={() => setMostrarDados((v) => !v)}
            className="ml-auto text-xs text-muted-foreground underline"
          >
            {mostrarDados ? "esconder onde cada resposta é guardada" : "mostrar onde cada resposta é guardada"}
          </button>
        </div>
      )}
    </div>
  );
}
