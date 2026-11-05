"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { PerguntasDoBot } from "@/components/perguntas-bot";
import { Secao } from "@/components/pagina";
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
import type { Bot, DetalheBot, ModeloBot } from "@/lib/tipos";

// A tela de um bot, em três abas (decisão do usuário em 29/10/2026: antes era tudo — nome,
// modelo, instruções, perguntas, limites, ligar e excluir — numa rolagem só, e ele disse que
// estava "bagunçado").
//
// O que ficou onde, e por quê:
//
//   - **o cabeçalho** carrega a identidade e **o botão**. Ligar e desligar é a ação mais
//     frequente e a mais consequente: não pode estar no fim de um formulário;
//   - **Atendimento**: o que o bot responde e a primeira mensagem. É o que se reescreve com
//     frequência, e merece a tela inteira;
//   - **Perguntas**: a lista, sozinha, porque é onde se trabalha mais;
//   - **Ajustes**: nome, modelo, limites de gasto e a exclusão. Mexe-se uma vez e esquece.
//
// Cada aba salva só os campos dela; o resto vai com o valor que está no servidor (a rota é um
// PUT do formulário inteiro, e um PUT parcial poderia desligar o bot sem querer). Depois de
// salvar, a consulta recarrega e as outras abas já mostram o valor novo.

type Aba = "atendimento" | "perguntas" | "ajustes";

export default function PaginaBot() {
  const params = useParams<{ id: string }>();
  const id = Number(params.id);
  const sessao = useSessao();
  const perfil = sessao.data?.usuario.perfil;
  const podeEditar = perfil === "admin" || perfil === "operador";
  const [aba, setAba] = useState<Aba>("atendimento");

  const consulta = useQuery({
    queryKey: ["atendimento-bot", id],
    queryFn: () => api<DetalheBot>(`/atendimento/bots/${id}`),
    enabled: Number.isFinite(id),
  });

  if (consulta.isError) {
    return (
      <Alert variant="destructive" className="max-w-2xl">
        <AlertDescription>{consulta.error.message}</AlertDescription>
      </Alert>
    );
  }
  if (!consulta.data) {
    return <Skeleton className="h-96 w-full" />;
  }

  const { bot, perguntas, conversas } = consulta.data;
  const abas: { id: Aba; rotulo: string; detalhe?: string }[] = [
    { id: "atendimento", rotulo: "Atendimento" },
    { id: "perguntas", rotulo: "Perguntas", detalhe: String(perguntas.length) },
    { id: "ajustes", rotulo: "Ajustes" },
  ];

  return (
    <div className="flex flex-col gap-8">
      <Cabecalho dados={consulta.data} podeEditar={podeEditar} />

      <nav aria-label={`Partes do bot ${bot.nome}`} className="flex gap-7 border-b">
        {abas.map((a) => (
          <button
            key={a.id}
            type="button"
            onClick={() => setAba(a.id)}
            aria-current={aba === a.id ? "page" : undefined}
            className={cn(
              "-mb-px shrink-0 border-b-2 border-transparent pb-3 text-sm whitespace-nowrap text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:text-foreground focus-visible:underline",
              aba === a.id && "border-foreground text-foreground",
            )}
          >
            {a.rotulo}
            {a.detalhe && <span className="ml-1.5 text-xs text-muted-foreground tabular-nums">{a.detalhe}</span>}
          </button>
        ))}
      </nav>

      {aba === "atendimento" && (
        <AbaAtendimento key={`atendimento-${bot.atualizado_em}`} bot={bot} podeEditar={podeEditar} />
      )}

      {aba === "perguntas" && (
        <Secao
          titulo="As perguntas"
          descricao="O que este atendimento precisa descobrir, de cima para baixo. O bot faz uma por vez, com as palavras dele, conforme a conversa."
        >
          {conversas.abertas > 0 && (
            <Alert>
              <AlertTitle>Há {conversas.abertas} conversa(s) aberta(s)</AlertTitle>
              <AlertDescription>
                As perguntas não se alteram agora: mudaria o atendimento de quem já está
                conversando. Espere as conversas encerrarem.
              </AlertDescription>
            </Alert>
          )}
          <PerguntasDoBot
            key={perguntas.map((q) => q.id).join("-")}
            botId={bot.id}
            perguntas={perguntas}
            podeEditar={podeEditar && conversas.abertas === 0}
          />
        </Secao>
      )}

      {aba === "ajustes" && (
        <AbaAjustes key={`ajustes-${bot.atualizado_em}`} dados={consulta.data} podeEditar={podeEditar} />
      )}
    </div>
  );
}

/** Identidade do bot e o botão: ligado, ele responde quem escrever. */
function Cabecalho({ dados, podeEditar }: { dados: DetalheBot; podeEditar: boolean }) {
  const { bot, conversas } = dados;
  const queryClient = useQueryClient();
  const [confirmar, setConfirmar] = useState(false);

  const ligar = useMutation({
    mutationFn: (ativo: boolean) => salvarBot(bot, { ativo }),
    onSuccess: (_, ativo) => {
      invalidarBot(queryClient, bot.id);
      toast.success(ativo ? `${bot.nome} ligado: ele responde quem escrever` : `${bot.nome} desligado`);
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <div className="flex flex-wrap items-start justify-between gap-x-8 gap-y-4">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-3">
          <h2 className="font-heading text-xl font-semibold">{bot.nome}</h2>
          {bot.ativo ? <Badge>ligado</Badge> : <Badge variant="secondary">desligado</Badge>}
        </div>
        <p className="mt-1 text-sm text-muted-foreground">
          <code>{bot.slug}</code> · {conversas.total} conversa(s), {conversas.abertas} aberta(s) ·
          criado em {dataHora(bot.criado_em)}
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-4">
        <Link
          href={`/atendimento/conversas?bot=${bot.id}`}
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          Ver conversas
        </Link>
        <div className={cn("flex items-center gap-3 rounded-lg border px-4 py-2", bot.ativo && "border-ouro/60")}>
          <Label htmlFor="ligado" className="text-sm">
            {bot.ativo ? "Respondendo" : "Desligado"}
          </Label>
          <Switch
            id="ligado"
            checked={bot.ativo}
            disabled={!podeEditar || ligar.isPending}
            onCheckedChange={(v) => {
              if (v) setConfirmar(true);
              else ligar.mutate(false);
            }}
          />
        </div>
      </div>

      <AlertDialog open={confirmar} onOpenChange={setConfirmar}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Ligar {bot.nome}?</AlertDialogTitle>
            <AlertDialogDescription>
              Quem escrever para o número deste bot passa a receber resposta, escrita pelo modelo na
              hora e sem revisão humana antes de sair. Antes de divulgar o número, escreva para ele
              você mesmo. Dá para desligar a qualquer momento, mas o que já saiu não volta.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmar(false);
                ligar.mutate(true);
              }}
            >
              Ligar
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/** Aba Atendimento: o que o bot responde e a primeira mensagem. */
function AbaAtendimento({ bot, podeEditar }: { bot: Bot; podeEditar: boolean }) {
  const queryClient = useQueryClient();
  const [instrucoes, setInstrucoes] = useState(bot.instrucoes);
  const [saudacao, setSaudacao] = useState(bot.saudacao ?? "");

  const salvar = useMutation({
    mutationFn: () => salvarBot(bot, { instrucoes, saudacao }),
    onSuccess: () => {
      invalidarBot(queryClient, bot.id);
      toast.success("Atendimento salvo");
    },
    onError: (e) => toast.error(e.message),
  });

  const mexido = instrucoes !== bot.instrucoes || saudacao !== (bot.saudacao ?? "");

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        salvar.mutate();
      }}
      className="flex max-w-3xl flex-col gap-8"
    >
      {!podeEditar && (
        <Alert>
          <AlertDescription>
            Seu perfil só vê esta tela. Editar bot é de administrador e operador.
          </AlertDescription>
        </Alert>
      )}

      <div className="flex flex-col gap-2">
        <Label htmlFor="instrucoes" className="font-heading text-base">
          O que este bot responde
        </Label>
        <p className="text-sm text-muted-foreground">
          Quem ele é, o que pode dizer e o que não pode. Ele só afirma o que está aqui — fora disso,
          diz que não sabe e que alguém da equipe responde.
        </p>
        <Textarea
          id="instrucoes"
          value={instrucoes}
          onChange={(e) => setInstrucoes(e.target.value)}
          rows={16}
          disabled={!podeEditar}
          required
          className="text-sm leading-relaxed"
        />
        <p className="text-xs text-muted-foreground">
          A plataforma já garante o resto, em todo bot: ele não finge ser pessoa, não inventa preço
          nem prazo, nunca pede senha, código ou cartão, e oferece alguém da equipe quando pedem.
        </p>
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="saudacao" className="font-heading text-base">
          Primeira mensagem (opcional)
        </Label>
        <p className="text-sm text-muted-foreground">
          Como ele abre a conversa, se você quiser padronizar. Em branco, o modelo cumprimenta com
          as palavras dele.
        </p>
        <Textarea
          id="saudacao"
          value={saudacao}
          onChange={(e) => setSaudacao(e.target.value)}
          rows={3}
          disabled={!podeEditar}
        />
      </div>

      {podeEditar && (
        <div className="flex items-center gap-3">
          <Button type="submit" disabled={!mexido || salvar.isPending}>
            {salvar.isPending ? "Salvando…" : "Salvar atendimento"}
          </Button>
          {mexido && !salvar.isPending && <span className="text-sm text-aviso">Há mudanças não salvas.</span>}
        </div>
      )}
    </form>
  );
}

/** Aba Ajustes: nome, modelo, limites de gasto e a exclusão. Mexe-se uma vez e esquece. */
function AbaAjustes({ dados, podeEditar }: { dados: DetalheBot; podeEditar: boolean }) {
  const { bot, conversas } = dados;
  const queryClient = useQueryClient();
  const router = useRouter();
  const [nome, setNome] = useState(bot.nome);
  const [modelo, setModelo] = useState(bot.modelo);
  const [maxHora, setMaxHora] = useState(String(bot.max_mensagens_hora));
  const [maxTokens, setMaxTokens] = useState(String(bot.max_tokens_conversa));
  const [pausa, setPausa] = useState(String(bot.pausa_resposta_segundos));
  const [excluir, setExcluir] = useState(false);

  const modelos = useQuery({
    queryKey: ["atendimento-modelos"],
    queryFn: () => api<{ modelos: ModeloBot[]; padrao: string }>("/atendimento/modelos"),
  });

  const salvar = useMutation({
    mutationFn: () =>
      salvarBot(bot, {
        nome,
        modelo,
        max_mensagens_hora: Number(maxHora),
        max_tokens_conversa: Number(maxTokens),
        pausa_resposta_segundos: Number(pausa),
      }),
    onSuccess: () => {
      invalidarBot(queryClient, bot.id);
      toast.success("Ajustes salvos");
    },
    onError: (e) => toast.error(e.message),
  });

  const apagar = useMutation({
    mutationFn: () => api<void>(`/atendimento/bots/${bot.id}`, { metodo: "DELETE" }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bots"] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-resumo"] });
      toast.success("Bot excluído");
      router.push("/atendimento/bots");
    },
    onError: (e) => toast.error(e.message),
  });

  const mexido =
    nome !== bot.nome ||
    modelo !== bot.modelo ||
    maxHora !== String(bot.max_mensagens_hora) ||
    maxTokens !== String(bot.max_tokens_conversa) ||
    pausa !== String(bot.pausa_resposta_segundos);
  const descricaoDoModelo = modelos.data?.modelos.find((m) => m.id === modelo)?.descricao;

  return (
    <div className="flex max-w-3xl flex-col gap-10">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          salvar.mutate();
        }}
        className="flex flex-col gap-8"
      >
        <Secao titulo="Identidade" descricao="O identificador não muda: é a pasta da sessão do WhatsApp deste bot.">
          <div className="grid gap-5 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="nome">Nome</Label>
              <Input id="nome" value={nome} onChange={(e) => setNome(e.target.value)} disabled={!podeEditar} required />
              <p className="text-xs text-muted-foreground">Como a equipe chama o bot aqui dentro.</p>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="slug">Identificador</Label>
              <Input id="slug" value={bot.slug} disabled readOnly className="font-mono" />
              <p className="text-xs text-muted-foreground">Trocar perderia o pareamento do WhatsApp.</p>
            </div>
          </div>
        </Secao>

        <Secao titulo="Modelo" descricao="Quem escreve as respostas. Cada mensagem respondida é uma chamada a ele.">
          <div className="flex max-w-md flex-col gap-2">
            <Label htmlFor="modelo" className="sr-only">
              Modelo
            </Label>
            <Select
              items={(modelos.data?.modelos ?? []).map((m) => ({ value: m.id, label: m.nome }))}
              value={modelo}
              onValueChange={(v) => setModelo(v ?? bot.modelo)}
              disabled={!podeEditar}
            >
              <SelectTrigger id="modelo">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(modelos.data?.modelos ?? []).map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {m.nome}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {descricaoDoModelo && <p className="text-xs text-muted-foreground">{descricaoDoModelo}</p>}
          </div>
        </Secao>

        <Secao
          titulo="Limites de gasto"
          descricao="Não travam o bot: existem para uma conversa — ou um laço entre dois automáticos — não virar conta sem fim."
        >
          <div className="grid gap-5 sm:grid-cols-3">
            <div className="flex flex-col gap-2">
              <Label htmlFor="max-hora">Respostas por hora</Label>
              <Input
                id="max-hora"
                type="number"
                min={1}
                max={1000}
                value={maxHora}
                onChange={(e) => setMaxHora(e.target.value)}
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
                value={maxTokens}
                onChange={(e) => setMaxTokens(e.target.value)}
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
                value={pausa}
                onChange={(e) => setPausa(e.target.value)}
                disabled={!podeEditar}
              />
              <p className="text-xs text-muted-foreground">Em segundos, para não parecer robô instantâneo.</p>
            </div>
          </div>
        </Secao>

        {podeEditar && (
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={!mexido || salvar.isPending}>
              {salvar.isPending ? "Salvando…" : "Salvar ajustes"}
            </Button>
            {mexido && !salvar.isPending && <span className="text-sm text-aviso">Há mudanças não salvas.</span>}
          </div>
        )}
      </form>

      {podeEditar && (
        <Secao titulo="Excluir" rotulo="Excluir bot">
          <div className="flex flex-wrap items-center justify-between gap-4 rounded-lg border p-4">
            <p className="min-w-0 text-sm text-muted-foreground">
              {conversas.total > 0
                ? `Este bot já tem ${conversas.total} conversa(s): ele não se exclui, porque isso apagaria o histórico. Para pará-lo, desligue no alto da tela.`
                : "Bot sem conversa nenhuma pode ser excluído."}
            </p>
            <Button variant="destructive" onClick={() => setExcluir(true)} disabled={conversas.total > 0}>
              Excluir bot
            </Button>
          </div>

          <AlertDialog open={excluir} onOpenChange={setExcluir}>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Excluir {bot.nome}?</AlertDialogTitle>
                <AlertDialogDescription>
                  O bot e as perguntas dele são apagados. Isto não se desfaz.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Cancelar</AlertDialogCancel>
                <AlertDialogAction
                  onClick={() => {
                    setExcluir(false);
                    apagar.mutate();
                  }}
                >
                  Excluir
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </Secao>
      )}
    </div>
  );
}

/**
 * salvarBot manda o bot inteiro com as mudanças aplicadas: a rota é um PUT do formulário todo
 * (um PUT parcial poderia desligar o bot sem querer), e cada aba só mexe nos campos dela.
 */
function salvarBot(bot: Bot, mudancas: Partial<Bot>) {
  return api<DetalheBot>(`/atendimento/bots/${bot.id}`, {
    metodo: "PUT",
    json: {
      nome: bot.nome,
      slug: bot.slug,
      modelo: bot.modelo,
      instrucoes: bot.instrucoes,
      saudacao: bot.saudacao ?? "",
      ativo: bot.ativo,
      max_mensagens_hora: bot.max_mensagens_hora,
      max_tokens_conversa: bot.max_tokens_conversa,
      pausa_resposta_segundos: bot.pausa_resposta_segundos,
      ...mudancas,
    },
  });
}

function invalidarBot(queryClient: ReturnType<typeof useQueryClient>, id: number) {
  void queryClient.invalidateQueries({ queryKey: ["atendimento-bot", id] });
  void queryClient.invalidateQueries({ queryKey: ["atendimento-bots"] });
  void queryClient.invalidateQueries({ queryKey: ["atendimento-resumo"] });
  void queryClient.invalidateQueries({ queryKey: ["atendimento-whatsapp"] });
}
