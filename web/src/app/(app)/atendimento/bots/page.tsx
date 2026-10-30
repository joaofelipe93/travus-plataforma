"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
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
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetBody, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import { slugSugerido } from "@/lib/atendimento";
import { formatarNumeroWhatsapp } from "@/components/conexao-whatsapp";
import { useSessao } from "@/lib/sessao";
import { cn } from "@/lib/utils";
import type { Bot, DetalheBot, ModeloBot, PontoDePartida, SituacaoWhatsappAtendimento } from "@/lib/tipos";

// Os bots do serviço, um cartão cada (antes era uma tabela com "modelo" e "criado em", que lia
// como despejo de banco). O cartão responde o que se pergunta olhando a lista:
//
//   - **está respondendo?** — o estado em palavras e o interruptor ali mesmo, sem abrir o bot;
//   - **tem número?** — para admin, a conexão do WhatsApp daquele bot. "Ligado sem número
//     pareado" é a causa mais comum de "o bot não respondeu", e precisa aparecer aqui;
//   - **está montado?** — quantas perguntas tem (bot sem pergunta só tira dúvida);
//   - **está em uso?** — conversas abertas.
//
// Bot nasce **desligado**: cadastrar não é soltar na rua.

export default function PaginaBots() {
  const sessao = useSessao();
  const perfil = sessao.data?.usuario.perfil;
  const podeEditar = perfil === "admin" || perfil === "operador";
  const admin = perfil === "admin";
  const [novo, setNovo] = useState(false);

  const bots = useQuery({
    queryKey: ["atendimento-bots"],
    queryFn: () => api<{ bots: Bot[] }>("/atendimento/bots"),
  });
  // A conexão de cada número é rota de admin (traz QR): para os outros perfis o cartão
  // simplesmente não mostra essa linha.
  const whatsapp = useQuery({
    queryKey: ["atendimento-whatsapp"],
    queryFn: () => api<SituacaoWhatsappAtendimento>("/atendimento/whatsapp"),
    enabled: admin,
  });

  if (bots.isError) {
    return (
      <Alert variant="destructive" className="max-w-2xl">
        <AlertDescription>{bots.error.message}</AlertDescription>
      </Alert>
    );
  }

  const lista = bots.data?.bots ?? [];
  const ligados = lista.filter((b) => b.ativo).length;

  return (
    <>
      <Secao
        titulo="Bots de atendimento"
        descricao={
          lista.length === 0
            ? "Cada bot tem o próprio número de WhatsApp, as próprias perguntas e o próprio modelo."
            : `${lista.length} bot(s), ${ligados} respondendo. Cada um tem o próprio número de WhatsApp.`
        }
        acoes={podeEditar ? <Button onClick={() => setNovo(true)}>Novo bot</Button> : undefined}
      >
        {bots.isPending ? (
          <Skeleton className="h-40 w-full" />
        ) : lista.length === 0 ? (
          <div className="flex max-w-2xl flex-col items-start gap-4 rounded-xl border border-dashed p-8">
            <div>
              <p className="font-heading text-base font-semibold">Nenhum bot ainda</p>
              <p className="mt-2 text-sm text-muted-foreground">
                Um bot é o nome, as instruções do que ele pode responder e a lista de perguntas que
                ele precisa fazer. Para não começar de uma tela em branco, o &quot;Novo bot&quot; oferece
                pontos de partida — plano de saúde empresarial, consórcio ou uma qualificação
                genérica —, e você edita o que quiser.
              </p>
              <p className="mt-2 text-sm text-muted-foreground">
                Ele nasce <strong>desligado</strong>: dá para escrever com calma antes de qualquer
                pessoa falar com ele.
              </p>
            </div>
            {podeEditar && <Button onClick={() => setNovo(true)}>Criar o primeiro bot</Button>}
          </div>
        ) : (
          <ul className="grid items-start gap-4 xl:grid-cols-2">
            {lista.map((b) => (
              <CartaoBot
                key={b.id}
                bot={b}
                conexao={admin ? whatsapp.data?.bots.find((x) => x.id === b.id) : undefined}
                podeEditar={podeEditar}
              />
            ))}
          </ul>
        )}
      </Secao>

      {podeEditar && <SheetNovoBot aberto={novo} aoFechar={() => setNovo(false)} />}
    </>
  );
}

function CartaoBot({
  bot,
  conexao,
  podeEditar,
}: {
  bot: Bot;
  conexao: SituacaoWhatsappAtendimento["bots"][number] | undefined;
  podeEditar: boolean;
}) {
  const queryClient = useQueryClient();
  const [confirmar, setConfirmar] = useState(false);

  const ligar = useMutation({
    mutationFn: (ativo: boolean) =>
      api<DetalheBot>(`/atendimento/bots/${bot.id}`, {
        metodo: "PUT",
        json: {
          nome: bot.nome,
          slug: bot.slug,
          modelo: bot.modelo,
          instrucoes: bot.instrucoes,
          saudacao: bot.saudacao ?? "",
          ativo,
          max_mensagens_hora: bot.max_mensagens_hora,
          max_tokens_conversa: bot.max_tokens_conversa,
          pausa_resposta_segundos: bot.pausa_resposta_segundos,
        },
      }),
    onSuccess: (_, ativo) => {
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bots"] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bot", bot.id] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-resumo"] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-whatsapp"] });
      toast.success(ativo ? `${bot.nome} ligado` : `${bot.nome} desligado`);
    },
    onError: (e) => toast.error(e.message),
  });

  const perguntas = bot.perguntas ?? 0;
  const abertas = bot.conversas_abertas ?? 0;
  // "Ligado sem número" é o que explica o bot que não respondeu: merece aviso, não rodapé.
  const faltaNumero = bot.ativo && conexao !== undefined && !conexao.conectado;

  return (
    <li
      className={cn(
        "flex min-w-0 flex-col gap-4 rounded-xl border bg-card/50 p-5",
        bot.ativo && "border-ouro/40",
        faltaNumero && "border-aviso/50",
      )}
    >
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <div className="min-w-0">
          <Link href={`/atendimento/bots/${bot.id}`} className="font-heading font-semibold hover:underline">
            {bot.nome}
          </Link>
          <p className="mt-0.5 text-xs text-muted-foreground">
            <code>{bot.slug}</code>
          </p>
        </div>
        <div className="flex items-center gap-2">
          {bot.ativo ? <Badge>ligado</Badge> : <Badge variant="secondary">desligado</Badge>}
          {podeEditar && (
            <Switch
              aria-label={bot.ativo ? `Desligar ${bot.nome}` : `Ligar ${bot.nome}`}
              checked={bot.ativo}
              disabled={ligar.isPending}
              onCheckedChange={(v) => {
                if (v) setConfirmar(true);
                else ligar.mutate(false);
              }}
            />
          )}
        </div>
      </div>

      <dl className="flex flex-wrap items-baseline gap-x-6 gap-y-1 text-sm">
        <div className="flex items-baseline gap-1.5">
          <dt className="text-muted-foreground">Perguntas</dt>
          <dd className={cn("font-medium tabular-nums", perguntas === 0 && "text-aviso")}>{perguntas}</dd>
        </div>
        <div className="flex items-baseline gap-1.5">
          <dt className="text-muted-foreground">Conversas abertas</dt>
          <dd className="font-medium tabular-nums">{abertas}</dd>
        </div>
        {conexao && (
          <div className="flex min-w-0 items-baseline gap-1.5">
            <dt className="text-muted-foreground">Número</dt>
            <dd className={cn("truncate font-medium", !conexao.conectado && "text-aviso")}>
              {conexao.numero ? formatarNumeroWhatsapp(conexao.numero) : "não pareado"}
            </dd>
          </div>
        )}
      </dl>

      {faltaNumero && (
        <p className="text-xs text-aviso">
          Este bot está ligado, mas sem número conectado: as respostas dele ficam na fila.{" "}
          <Link href="/atendimento/conexoes" className="underline">
            Parear em Conexões
          </Link>
          .
        </p>
      )}
      {perguntas === 0 && (
        <p className="text-xs text-muted-foreground">
          Sem perguntas, ele só tira dúvida: não descobre nada de quem escreve.
        </p>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <Link
          href={`/atendimento/bots/${bot.id}`}
          className={buttonVariants({ variant: "outline", size: "sm" })}
        >
          Abrir
        </Link>
        {abertas > 0 && (
          <Link
            href={`/atendimento/conversas?bot=${bot.id}&situacao=abertas`}
            className={buttonVariants({ variant: "ghost", size: "sm" })}
          >
            Ver conversas
          </Link>
        )}
      </div>

      <AlertDialog open={confirmar} onOpenChange={setConfirmar}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Ligar {bot.nome}?</AlertDialogTitle>
            <AlertDialogDescription>
              Quem escrever para o número deste bot passa a receber resposta, escrita pelo modelo na
              hora e sem revisão humana antes de sair. Antes de divulgar o número, escreva para ele
              você mesmo.
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
    </li>
  );
}

function SheetNovoBot({ aberto, aoFechar }: { aberto: boolean; aoFechar: () => void }) {
  const queryClient = useQueryClient();
  const router = useRouter();
  const [nome, setNome] = useState("");
  const [slug, setSlug] = useState("");
  const [slugEditado, setSlugEditado] = useState(false);
  const [modelo, setModelo] = useState("");
  const [instrucoes, setInstrucoes] = useState("");
  // Ponto de partida escolhido: copia instruções, saudação e perguntas para o bot novo. A Travus
  // tem vários segmentos, então nenhum é padrão — "em branco" é o primeiro da lista.
  const [partida, setPartida] = useState("em-branco");

  const pontos = useQuery({
    queryKey: ["atendimento-pontos-de-partida"],
    queryFn: () => api<{ pontos_de_partida: PontoDePartida[] }>("/atendimento/pontos-de-partida"),
    enabled: aberto,
  });
  const pontoEscolhido = pontos.data?.pontos_de_partida.find((p) => p.id === partida);

  const modelos = useQuery({
    queryKey: ["atendimento-modelos"],
    queryFn: () => api<{ modelos: ModeloBot[]; padrao: string }>("/atendimento/modelos"),
    enabled: aberto,
  });
  const escolhido = modelo || modelos.data?.padrao || "";

  const criar = useMutation({
    mutationFn: async () => {
      const res = await api<DetalheBot>("/atendimento/bots", {
        metodo: "POST",
        json: {
          nome,
          slug: slug || slugSugerido(nome),
          modelo: escolhido,
          instrucoes,
          saudacao: pontoEscolhido?.saudacao ?? "",
        },
      });
      // As perguntas do ponto de partida entram na sequência, pela rota delas.
      if (pontoEscolhido && pontoEscolhido.perguntas.length > 0) {
        await api(`/atendimento/bots/${res.bot.id}/perguntas`, {
          metodo: "PUT",
          json: { perguntas: pontoEscolhido.perguntas },
        });
      }
      return res;
    },
    onSuccess: (res) => {
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bots"] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-resumo"] });
      toast.success("Bot criado. Agora confira as perguntas.");
      limpar();
      aoFechar();
      router.push(`/atendimento/bots/${res.bot.id}`);
    },
    onError: (e) => toast.error(e.message),
  });

  function limpar() {
    setNome("");
    setSlug("");
    setSlugEditado(false);
    setModelo("");
    setInstrucoes("");
  }

  const descricaoDoModelo = modelos.data?.modelos.find((m) => m.id === escolhido)?.descricao;

  return (
    <Sheet open={aberto} onOpenChange={(a) => !a && !criar.isPending && aoFechar()}>
      <SheetContent className="max-w-lg">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            criar.mutate();
          }}
          className="flex min-h-0 flex-1 flex-col"
        >
          <SheetHeader>
            <SheetTitle>Novo bot</SheetTitle>
            <SheetDescription>
              Ele nasce desligado: dá para escrever as perguntas com calma antes de qualquer pessoa
              falar com ele.
            </SheetDescription>
          </SheetHeader>
          <SheetBody className="flex flex-col gap-5">
            <div className="flex flex-col gap-2">
              <Label htmlFor="bot-partida">Ponto de partida</Label>
              <Select
                value={partida}
                onValueChange={(v) => {
                  const id = v ?? "em-branco";
                  setPartida(id);
                  const p = pontos.data?.pontos_de_partida.find((x) => x.id === id);
                  // Só preenche o que ainda está vazio ou veio de outro ponto de partida: quem
                  // já escreveu algo não perde o texto por trocar a opção.
                  setInstrucoes(p?.instrucoes ?? "");
                }}
              >
                <SelectTrigger id="bot-partida">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(pontos.data?.pontos_de_partida ?? []).map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.nome}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {pontoEscolhido && (
                <p className="text-xs text-muted-foreground">
                  {pontoEscolhido.descricao}
                  {pontoEscolhido.perguntas.length > 0 &&
                    ` Vem com ${pontoEscolhido.perguntas.length} pergunta(s), que você edita depois.`}
                </p>
              )}
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="bot-nome">Nome</Label>
              <Input
                id="bot-nome"
                value={nome}
                onChange={(e) => {
                  setNome(e.target.value);
                  if (!slugEditado) setSlug(slugSugerido(e.target.value));
                }}
                placeholder="Atendimento Comercial"
                required
              />
              <p className="text-xs text-muted-foreground">
                É como a equipe chama o bot aqui dentro. O cliente não vê este nome.
              </p>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="bot-slug">Identificador</Label>
              <Input
                id="bot-slug"
                value={slug}
                onChange={(e) => {
                  setSlug(e.target.value);
                  setSlugEditado(true);
                }}
                placeholder="comercial"
                required
              />
              <p className="text-xs text-muted-foreground">
                Só minúsculas, números e hífen. Ele identifica a sessão do WhatsApp deste bot e{" "}
                <strong>não muda depois</strong>.
              </p>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="bot-modelo">Modelo</Label>
              <Select value={escolhido} onValueChange={(v) => setModelo(v ?? "")}>
                <SelectTrigger id="bot-modelo">
                  <SelectValue placeholder="Escolha o modelo" />
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
            <div className="flex flex-col gap-2">
              <Label htmlFor="bot-instrucoes">Instruções</Label>
              <Textarea
                id="bot-instrucoes"
                value={instrucoes}
                onChange={(e) => setInstrucoes(e.target.value)}
                rows={8}
                placeholder={
                  "Você é o primeiro atendimento da Travus no WhatsApp.\n" +
                  "Diga o que a equipe atende e o que você pode responder.\n" +
                  "Não fale de preço nem prometa prazo: isso é da equipe."
                }
                required
              />
              <p className="text-xs text-muted-foreground">
                Quem o bot é, o que ele pode responder e o que não pode. O bot só afirma o que está
                aqui — e já sabe, por conta da plataforma, que não finge ser pessoa e nunca pede
                senha, código ou cartão.
              </p>
            </div>
          </SheetBody>
          <SheetFooter>
            <Button type="button" variant="ghost" onClick={aoFechar} disabled={criar.isPending}>
              Cancelar
            </Button>
            <Button type="submit" disabled={criar.isPending}>
              {criar.isPending ? "Criando…" : "Criar bot"}
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}
