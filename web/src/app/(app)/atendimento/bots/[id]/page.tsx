"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { PerguntasDoBot } from "@/components/perguntas-bot";
import { Colunas, Secao } from "@/components/pagina";
import { Bloco, Estados } from "@/components/painel";
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
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
import { dataHora, useSessao } from "@/lib/sessao";
import type { DetalheBot, ModeloBot } from "@/lib/tipos";

// A tela de um bot: quem ele é (nome, modelo, instruções), o fluxo de etapas, os tetos de gasto
// e **o botão** — ligado ou desligado, o único interruptor que existe (decisão do usuário em
// 29/10/2026: "não quero tantas travas, só quero um botão"). Bot ligado responde quem escrever
// para o número dele; desligado, não abre conversa nem responde.
//
// O que continua separado: mudar o **fluxo** com conversa aberta é recusado pela API, porque a
// pessoa ficaria numa etapa que deixou de existir.

export default function PaginaBot() {
  const params = useParams<{ id: string }>();
  const id = Number(params.id);
  const sessao = useSessao();
  const perfil = sessao.data?.usuario.perfil;
  const podeEditar = perfil === "admin" || perfil === "operador";

  const consulta = useQuery({
    queryKey: ["atendimento-bot", id],
    queryFn: () => api<DetalheBot>(`/atendimento/bots/${id}`),
    enabled: Number.isFinite(id),
  });
  const modelos = useQuery({
    queryKey: ["atendimento-modelos"],
    queryFn: () => api<{ modelos: ModeloBot[]; padrao: string }>("/atendimento/modelos"),
  });

  if (consulta.isError) {
    return (
      <Alert variant="destructive" className="max-w-2xl">
        <AlertDescription>{consulta.error.message}</AlertDescription>
      </Alert>
    );
  }
  if (!consulta.data) {
    return <Skeleton className="h-96 w-full max-w-4xl" />;
  }

  const { bot, perguntas, conversas } = consulta.data;
  return (
    <Colunas
      lateral={
        <>
          <Bloco titulo="Como está" tom={bot.ativo ? "ok" : "neutro"}>
            <Estados
              itens={[
                {
                  rotulo: "Bot",
                  valor: bot.ativo ? "ligado: responde quem escrever" : "desligado",
                  tom: bot.ativo ? "ok" : "neutro",
                },
                { rotulo: "Conversas", valor: conversas.total, detalhe: `${conversas.abertas} aberta(s)` },
                { rotulo: "Identificador", valor: bot.slug },
                { rotulo: "Criado", valor: dataHora(bot.criado_em) },
              ]}
            />
          </Bloco>
          <Bloco
            titulo="Conversas deste bot"
            descricao="O que as pessoas escreveram e o que o bot respondeu."
            acoes={
              <Link
                href={`/atendimento/conversas?bot=${bot.id}`}
                className={buttonVariants({ variant: "outline", size: "sm" })}
              >
                Abrir
              </Link>
            }
          />
        </>
      }
    >
      <DadosDoBot
        key={JSON.stringify(bot)}
        dados={consulta.data}
        modelos={modelos.data?.modelos ?? []}
        podeEditar={podeEditar}
      />

      <Secao
        titulo="As perguntas"
        descricao="O que este atendimento precisa descobrir, de cima para baixo. O bot faz uma por vez, com as palavras dele, conforme a conversa."
      >
        {conversas.abertas > 0 && (
          <Alert>
            <AlertTitle>Há {conversas.abertas} conversa(s) aberta(s)</AlertTitle>
            <AlertDescription>
              As perguntas não se alteram agora: mudaria o atendimento de quem já está conversando.
              Espere as conversas encerrarem.
            </AlertDescription>
          </Alert>
        )}
        <PerguntasDoBot
          botId={bot.id}
          perguntas={perguntas}
          podeEditar={podeEditar && conversas.abertas === 0}
        />
      </Secao>

      {podeEditar && <SecaoExcluir dados={consulta.data} />}
    </Colunas>
  );
}

type Formulario = {
  nome: string;
  modelo: string;
  instrucoes: string;
  saudacao: string;
  ativo: boolean;
  maxMensagensHora: string;
  maxTokensConversa: string;
  pausa: string;
};

function DadosDoBot({
  dados,
  modelos,
  podeEditar,
}: {
  dados: DetalheBot;
  modelos: ModeloBot[];
  podeEditar: boolean;
}) {
  const { bot } = dados;
  const queryClient = useQueryClient();
  const [confirmarLigar, setConfirmarLigar] = useState(false);
  const [f, setF] = useState<Formulario>({
    nome: bot.nome,
    modelo: bot.modelo,
    instrucoes: bot.instrucoes,
    saudacao: bot.saudacao ?? "",
    ativo: bot.ativo,
    maxMensagensHora: String(bot.max_mensagens_hora),
    maxTokensConversa: String(bot.max_tokens_conversa),
    pausa: String(bot.pausa_resposta_segundos),
  });

  const salvar = useMutation({
    mutationFn: () =>
      api<DetalheBot>(`/atendimento/bots/${bot.id}`, {
        metodo: "PUT",
        json: {
          nome: f.nome,
          slug: bot.slug, // o identificador não muda: a API recusa
          modelo: f.modelo,
          instrucoes: f.instrucoes,
          saudacao: f.saudacao,
          ativo: f.ativo,
          max_mensagens_hora: Number(f.maxMensagensHora),
          max_tokens_conversa: Number(f.maxTokensConversa),
          pausa_resposta_segundos: Number(f.pausa),
        },
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bot", bot.id] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bots"] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-resumo"] });
      toast.success("Bot salvo");
    },
    onError: (e) => toast.error(e.message),
  });

  const descricaoDoModelo = modelos.find((m) => m.id === f.modelo)?.descricao;

  return (
    <Secao titulo={bot.nome} descricao={`Identificador ${bot.slug} · não muda depois de criado.`}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          salvar.mutate();
        }}
        className="flex max-w-3xl flex-col gap-6"
      >
        {!podeEditar && (
          <Alert>
            <AlertDescription>
              Seu perfil só vê esta tela. Cadastrar e editar bot é de administrador e operador.
            </AlertDescription>
          </Alert>
        )}
        <div className="grid gap-5 sm:grid-cols-2">
          <div className="flex flex-col gap-2">
            <Label htmlFor="nome">Nome</Label>
            <Input
              id="nome"
              value={f.nome}
              onChange={(e) => setF({ ...f, nome: e.target.value })}
              disabled={!podeEditar}
              required
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="modelo">Modelo</Label>
            <Select value={f.modelo} onValueChange={(v) => setF({ ...f, modelo: v ?? f.modelo })} disabled={!podeEditar}>
              <SelectTrigger id="modelo">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {modelos.map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {m.nome}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {descricaoDoModelo && <p className="text-xs text-muted-foreground">{descricaoDoModelo}</p>}
          </div>
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="instrucoes">Instruções</Label>
          <Textarea
            id="instrucoes"
            value={f.instrucoes}
            onChange={(e) => setF({ ...f, instrucoes: e.target.value })}
            rows={10}
            disabled={!podeEditar}
            required
          />
          <p className="text-xs text-muted-foreground">
            O bot só afirma o que está aqui. Fora disso ele diz que não sabe e que alguém da equipe
            responde — não inventa preço, prazo nem disponibilidade.
          </p>
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="saudacao">Primeira mensagem (opcional)</Label>
          <Textarea
            id="saudacao"
            value={f.saudacao}
            onChange={(e) => setF({ ...f, saudacao: e.target.value })}
            rows={3}
            disabled={!podeEditar}
            placeholder="Oi! Aqui é o atendimento da Travus. Como posso ajudar?"
          />
        </div>

        <div className="flex flex-col gap-4 rounded-lg border p-4">
          <div>
            <p className="font-heading font-semibold">Travas de gasto</p>
            <p className="mt-1 text-sm text-muted-foreground">
              Cada mensagem respondida é uma chamada ao modelo. Estes limites existem para uma
              conversa — ou um laço entre dois automáticos — não virar conta sem fim.
            </p>
          </div>
          <div className="grid gap-5 sm:grid-cols-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="max-hora">Respostas por hora</Label>
              <Input
                id="max-hora"
                type="number"
                min={1}
                max={1000}
                value={f.maxMensagensHora}
                onChange={(e) => setF({ ...f, maxMensagensHora: e.target.value })}
                disabled={!podeEditar}
              />
              <p className="text-xs text-muted-foreground">Passando disso, a resposta é cancelada.</p>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="max-tokens">Tokens por conversa</Label>
              <Input
                id="max-tokens"
                type="number"
                min={1000}
                value={f.maxTokensConversa}
                onChange={(e) => setF({ ...f, maxTokensConversa: e.target.value })}
                disabled={!podeEditar}
              />
              <p className="text-xs text-muted-foreground">Alcançado o teto, a conversa encerra.</p>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="pausa">Pausa antes de responder</Label>
              <Input
                id="pausa"
                type="number"
                min={0}
                max={120}
                value={f.pausa}
                onChange={(e) => setF({ ...f, pausa: e.target.value })}
                disabled={!podeEditar}
              />
              <p className="text-xs text-muted-foreground">Em segundos, para não parecer um robô instantâneo.</p>
            </div>
          </div>
        </div>

        <div
          className={cn(
            "flex items-start justify-between gap-6 rounded-lg border p-4",
            f.ativo && "border-ouro/60",
          )}
        >
          <div className="min-w-0">
            <Label htmlFor="ativo" className="font-heading font-semibold">
              Bot ligado
            </Label>
            <p className="mt-1 text-sm text-muted-foreground">
              É o único interruptor: <strong>ligado, ele responde</strong> quem escrever para o
              número dele, com o texto que o modelo escrever na hora. Desligado, não abre conversa
              nem responde — e o que estiver na fila espera.
            </p>
            <p className="mt-2 text-xs text-muted-foreground">
              Antes de divulgar o número, mande uma mensagem para ele você mesmo: é o teste que não
              mente.
            </p>
          </div>
          <Switch
            id="ativo"
            checked={f.ativo}
            onCheckedChange={(v) => {
              if (v && !bot.ativo) setConfirmarLigar(true);
              else setF({ ...f, ativo: v });
            }}
            disabled={!podeEditar}
          />
        </div>

        {podeEditar && (
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={salvar.isPending}>
              {salvar.isPending ? "Salvando…" : "Salvar bot"}
            </Button>
            {f.ativo !== bot.ativo && (
              <span className="text-sm text-aviso">
                {f.ativo ? "Salve para ligar o bot." : "Salve para desligar o bot."}
              </span>
            )}
          </div>
        )}

        <AlertDialog open={confirmarLigar} onOpenChange={setConfirmarLigar}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Ligar {bot.nome}?</AlertDialogTitle>
              <AlertDialogDescription>
                A partir do momento em que você salvar, quem escrever para o número deste bot
                recebe resposta. O texto é escrito pelo modelo na hora e ninguém revisa antes de
                sair. Dá para desligar a qualquer momento, mas o que já saiu não volta.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancelar</AlertDialogCancel>
              <AlertDialogAction
                onClick={() => {
                  setConfirmarLigar(false);
                  setF({ ...f, ativo: true });
                }}
              >
                Ligar
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </form>
    </Secao>
  );
}

function SecaoExcluir({ dados }: { dados: DetalheBot }) {
  const { bot, conversas } = dados;
  const router = useRouter();
  const queryClient = useQueryClient();
  const [aberto, setAberto] = useState(false);

  const excluir = useMutation({
    mutationFn: () => api<void>(`/atendimento/bots/${bot.id}`, { metodo: "DELETE" }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bots"] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-resumo"] });
      toast.success("Bot excluído");
      router.push("/atendimento/bots");
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <Secao titulo="Excluir" rotulo="Excluir bot">
      <div className="flex max-w-3xl flex-wrap items-center justify-between gap-4 rounded-lg border p-4">
        <p className="min-w-0 text-sm text-muted-foreground">
          {conversas.total > 0
            ? `Este bot já tem ${conversas.total} conversa(s): ele não se exclui, porque isso apagaria o histórico. Para pará-lo, desligue o bot acima.`
            : "Bot sem conversa nenhuma pode ser excluído."}
        </p>
        <Button variant="destructive" onClick={() => setAberto(true)} disabled={conversas.total > 0}>
          Excluir bot
        </Button>
      </div>

      <AlertDialog open={aberto} onOpenChange={setAberto}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Excluir {bot.nome}?</AlertDialogTitle>
            <AlertDialogDescription>
              O bot e o fluxo dele são apagados. Isto não se desfaz.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setAberto(false);
                excluir.mutate();
              }}
            >
              Excluir
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Secao>
  );
}
