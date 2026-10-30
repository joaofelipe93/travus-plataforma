"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { Secao } from "@/components/pagina";
import { Vazio } from "@/components/painel";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetBody, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import { slugSugerido } from "@/lib/atendimento";
import { dataHora, useSessao } from "@/lib/sessao";
import type { Bot, DetalheBot, ModeloBot, PontoDePartida } from "@/lib/tipos";

// Os bots do serviço: um por atendimento (um número de WhatsApp cada). A lista responde "quais
// existem e qual está realmente respondendo"; o fluxo de cada um fica na tela do bot.
//
// Bot nasce **desligado e sem envio**: cadastrar não é soltar na rua. Quem liga o `ativo` é
// admin ou operador na tela do bot; quem autoriza mandar mensagem é só admin.

export default function PaginaBots() {
  const sessao = useSessao();
  const perfil = sessao.data?.usuario.perfil;
  const podeEditar = perfil === "admin" || perfil === "operador";
  const [novo, setNovo] = useState(false);

  const bots = useQuery({
    queryKey: ["atendimento-bots"],
    queryFn: () => api<{ bots: Bot[] }>("/atendimento/bots"),
  });

  if (bots.isError) {
    return (
      <Alert variant="destructive" className="max-w-2xl">
        <AlertDescription>{bots.error.message}</AlertDescription>
      </Alert>
    );
  }

  const lista = bots.data?.bots ?? [];
  return (
    <>
      <Secao
        titulo="Bots de atendimento"
        descricao="Cada bot tem o próprio número de WhatsApp, o próprio fluxo e o próprio modelo. Um bot novo nasce desligado."
        acoes={podeEditar ? <Button onClick={() => setNovo(true)}>Novo bot</Button> : undefined}
      >
        {bots.isPending ? (
          <Skeleton className="h-40 w-full" />
        ) : lista.length === 0 ? (
          <Vazio>
            Nenhum bot ainda. Um bot é o nome, as instruções do que ele pode responder e a lista de
            perguntas que ele precisa fazer.
          </Vazio>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Bot</TableHead>
                <TableHead>Modelo</TableHead>
                <TableHead className="text-right">Perguntas</TableHead>
                <TableHead className="text-right">Conversas abertas</TableHead>
                <TableHead>Estado</TableHead>
                <TableHead>Criado</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {lista.map((b) => (
                <TableRow key={b.id}>
                  <TableCell>
                    <Link href={`/atendimento/bots/${b.id}`} className="font-medium hover:underline">
                      {b.nome}
                    </Link>
                    <p className="text-xs text-muted-foreground">{b.slug}</p>
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">{b.modelo}</TableCell>
                  <TableCell className="text-right tabular-nums">{b.perguntas ?? 0} pergunta(s)</TableCell>
                  <TableCell className="text-right tabular-nums">{b.conversas_abertas ?? 0}</TableCell>
                  <TableCell>
                    {b.ativo ? <Badge>ligado</Badge> : <Badge variant="secondary">desligado</Badge>}
                  </TableCell>
                  <TableCell className="text-xs whitespace-nowrap text-muted-foreground">
                    {dataHora(b.criado_em)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Secao>

      {podeEditar && <SheetNovoBot aberto={novo} aoFechar={() => setNovo(false)} />}
    </>
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
