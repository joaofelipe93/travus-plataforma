"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { formatarNumeroWhatsapp } from "@/components/conexao-whatsapp";
import { Colunas, Secao } from "@/components/pagina";
import { Bloco, Estados, Indicador, Indicadores, Vazio } from "@/components/painel";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { api, ErroApi } from "@/lib/api";
import { motivoLegivel } from "@/lib/atendimento";
import { dataHora } from "@/lib/sessao";
import type { ClienteResumo, Lead, RespostaLeads, StatusLead } from "@/lib/tipos";

// Leads: o que o atendimento produziu de aproveitável. Cada lead é uma conversa que colheu algo
// e espera **uma pessoa** decidir — o bot não cria cliente (decisão do usuário em 29/10/2026).
//
// A tela é lista à esquerda e revisão à direita, como o cadastro de Clientes: quem revisa compara
// o que o bot entendeu com a conversa, corrige o que estiver torto e então decide. Três decisões
// possíveis, e a do meio é a que evita cadastro duplicado:
//
//   - **criar cliente** com os dados revisados (nasce ativo em nenhum serviço);
//   - **ligar a um cliente que já existe** (alguém da carteira que escreveu no WhatsApp);
//   - **descartar com motivo** — "por que isto não virou cliente?" é pergunta de um mês depois.

const rotuloStatusLead: Record<StatusLead, string> = {
  novo: "esperando revisão",
  aproveitado: "aproveitado",
  descartado: "descartado",
};

type Situacao = "novos" | "aproveitados" | "descartados" | "todos";

export default function PaginaLeads() {
  const [situacao, setSituacao] = useState<Situacao>("novos");
  const [selecionado, setSelecionado] = useState<number | null>(null);

  const leads = useQuery({
    queryKey: ["atendimento-leads", situacao],
    queryFn: () => api<RespostaLeads>(`/atendimento/leads?situacao=${situacao}`),
    refetchInterval: 60_000,
  });

  if (leads.isError) {
    return (
      <Alert variant="destructive" className="max-w-2xl">
        <AlertDescription>{leads.error.message}</AlertDescription>
      </Alert>
    );
  }

  const lista = leads.data?.leads ?? [];
  const contagem = leads.data?.contagem;
  const aberto = lista.find((l) => l.id === selecionado) ?? lista[0];

  return (
    <div className="flex flex-col gap-10">
      {contagem && (
        <Indicadores>
          <Indicador
            rotulo="Esperando revisão"
            valor={contagem.novos}
            tom={contagem.novos > 0 ? "destaque" : "neutro"}
            detalhe="Cada um é uma conversa que colheu algo"
          />
          <Indicador rotulo="Aproveitados" valor={contagem.aproveitados} tom="ok" detalhe="Viraram cliente no cadastro" />
          <Indicador rotulo="Descartados" valor={contagem.descartados} detalhe="Com o motivo registrado" />
          <Indicador rotulo="No mês" valor={contagem.no_mes} detalhe="Leads criados neste mês" />
        </Indicadores>
      )}

      <Colunas
        lateralFixa
        lateral={
          <Bloco titulo="Como funciona">
            <p className="text-sm text-muted-foreground">
              Quando uma conversa encerra tendo colhido algum dado, ela vira um lead aqui. O bot
              nunca cadastra ninguém: quem decide é você, com a conversa ao lado.
            </p>
            <p className="text-sm text-muted-foreground">
              O cliente criado a partir de um lead nasce <strong>ativo em nenhum serviço</strong> —
              ele não entra em dry-run de lance nem em rodada de boletos até alguém ligar isso na
              tela dele.
            </p>
          </Bloco>
        }
      >
        <Secao
          titulo="Leads"
          descricao="Novos primeiro. A revisão abre ao lado."
          acoes={
            <Select value={situacao} onValueChange={(v) => setSituacao((v ?? "novos") as Situacao)}>
              <SelectTrigger className="w-48" aria-label="Filtrar por situação">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="novos">Esperando revisão</SelectItem>
                <SelectItem value="aproveitados">Aproveitados</SelectItem>
                <SelectItem value="descartados">Descartados</SelectItem>
                <SelectItem value="todos">Todos</SelectItem>
              </SelectContent>
            </Select>
          }
        >
          {leads.isPending ? (
            <Skeleton className="h-64 w-full" />
          ) : lista.length === 0 ? (
            <Vazio>
              {situacao === "novos"
                ? "Nenhum lead esperando. Eles aparecem quando uma conversa encerra tendo colhido algum dado."
                : "Nenhum lead neste recorte."}
            </Vazio>
          ) : (
            <div className="grid items-start gap-6 lg:grid-cols-[22rem_minmax(0,1fr)]">
              <ul className="flex max-h-[42rem] flex-col gap-2 overflow-y-auto pr-1">
                {lista.map((l) => (
                  <li key={l.id}>
                    <button
                      type="button"
                      onClick={() => setSelecionado(l.id)}
                      className={[
                        "w-full rounded-lg border p-3 text-left outline-none transition-colors hover:border-ouro/60 focus-visible:ring-2 focus-visible:ring-ring",
                        aberto?.id === l.id ? "border-ouro/60 bg-card/60" : "bg-card/30",
                      ].join(" ")}
                    >
                      <p className="truncate text-sm font-medium">
                        {l.nome || formatarNumeroWhatsapp(l.conversa_telefone)}
                      </p>
                      <p className="mt-0.5 truncate text-xs text-muted-foreground">
                        {l.bot_nome} · {Object.keys(l.dados).length} dado(s) · {dataHora(l.criado_em)}
                      </p>
                      <span className="mt-2 inline-block">
                        <Badge variant={l.status === "novo" ? "default" : "secondary"}>
                          {rotuloStatusLead[l.status]}
                        </Badge>
                      </span>
                    </button>
                  </li>
                ))}
              </ul>

              {aberto ? <Revisao key={aberto.id} lead={aberto} /> : <Vazio>Escolha um lead à esquerda.</Vazio>}
            </div>
          )}
        </Secao>
      </Colunas>
    </div>
  );
}

function Revisao({ lead }: { lead: Lead }) {
  const queryClient = useQueryClient();
  const [nome, setNome] = useState(lead.nome ?? "");
  const [telefone, setTelefone] = useState(lead.telefone ?? "");
  const [email, setEmail] = useState(lead.email ?? "");
  const [observacoes, setObservacoes] = useState(lead.observacoes ?? "");
  const [motivo, setMotivo] = useState("");
  const [confirmarCriar, setConfirmarCriar] = useState(false);
  const [confirmarDescartar, setConfirmarDescartar] = useState(false);
  const [ligarA, setLigarA] = useState<number | null>(null);
  const [buscaCliente, setBuscaCliente] = useState("");
  const [nomeRepetido, setNomeRepetido] = useState(false);

  const decidido = lead.status !== "novo";

  const invalidar = () => {
    void queryClient.invalidateQueries({ queryKey: ["atendimento-leads"] });
    void queryClient.invalidateQueries({ queryKey: ["atendimento-resumo"] });
  };

  const salvar = useMutation({
    mutationFn: () =>
      api<{ lead: Lead }>(`/atendimento/leads/${lead.id}`, {
        metodo: "PATCH",
        json: { nome, telefone, email, observacoes },
      }),
    onSuccess: () => {
      invalidar();
      toast.success("Correções salvas");
    },
    onError: (e) => toast.error(e.message),
  });

  const aproveitar = useMutation({
    mutationFn: (clienteID: number | null) =>
      api<{ lead: Lead }>(`/atendimento/leads/${lead.id}/aproveitar`, {
        metodo: "POST",
        json: clienteID ? { cliente_id: clienteID } : {},
      }),
    onSuccess: (res) => {
      invalidar();
      void queryClient.invalidateQueries({ queryKey: ["clientes"] });
      toast.success(`Lead aproveitado: ${res.lead.cliente_nome ?? "cliente ligado"}`);
    },
    onError: (e) => {
      // Nome repetido no cadastro: a saída é ligar ao cliente que já existe, não insistir.
      if (e instanceof ErroApi && e.status === 409) {
        setNomeRepetido(true);
        toast.error(`${e.message} — ligue este lead ao cliente que já existe.`);
      } else {
        toast.error(e.message);
      }
    },
  });

  const descartar = useMutation({
    mutationFn: () =>
      api<{ lead: Lead }>(`/atendimento/leads/${lead.id}/descartar`, {
        metodo: "POST",
        json: { motivo },
      }),
    onSuccess: () => {
      invalidar();
      toast.success("Lead descartado");
    },
    onError: (e) => toast.error(e.message),
  });

  // A lista de clientes só é buscada quando alguém vai ligar o lead a um existente.
  const clientes = useQuery({
    queryKey: ["clientes"],
    queryFn: () => api<{ clientes: ClienteResumo[] }>("/clientes"),
    enabled: nomeRepetido || buscaCliente.length > 1,
  });
  const candidatos = (clientes.data?.clientes ?? [])
    .filter((c) => c.nome.toLowerCase().includes(buscaCliente.toLowerCase()))
    .slice(0, 8);

  const outros = Object.entries(lead.dados);

  return (
    <div className="flex min-w-0 flex-col gap-6">
      <Bloco
        titulo={lead.nome || formatarNumeroWhatsapp(lead.conversa_telefone)}
        tom={lead.status === "aproveitado" ? "ok" : lead.status === "descartado" ? "neutro" : "destaque"}
        descricao={`Veio de ${lead.bot_nome}, em ${dataHora(lead.criado_em)}.`}
        acoes={
          <Link
            href={`/atendimento/conversas/${lead.conversa_id}`}
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            Ver a conversa
          </Link>
        }
      >
        <Estados
          itens={[
            {
              rotulo: "Situação",
              valor: <Badge variant={lead.status === "novo" ? "default" : "secondary"}>{rotuloStatusLead[lead.status]}</Badge>,
              detalhe: lead.revisado_por_nome ? `por ${lead.revisado_por_nome}` : undefined,
            },
            {
              rotulo: "WhatsApp de origem",
              valor: formatarNumeroWhatsapp(lead.conversa_telefone),
              detalhe: lead.contato_nome ? `no nome de ${lead.contato_nome}` : undefined,
            },
            {
              rotulo: "A conversa",
              valor: `${lead.mensagens} mensagem(ns)`,
              detalhe: motivoLegivel(lead.conversa_motivo) ?? undefined,
            },
            ...(lead.cliente_id
              ? [
                  {
                    rotulo: "Virou o cliente",
                    valor: (
                      <Link href={`/clientes/${lead.cliente_id}`} className="underline">
                        {lead.cliente_nome ?? "ver cliente"}
                      </Link>
                    ),
                    tom: "ok" as const,
                  },
                ]
              : []),
            ...(lead.motivo_descarte
              ? [{ rotulo: "Motivo do descarte", valor: lead.motivo_descarte }]
              : []),
          ]}
        />
      </Bloco>

      {outros.length > 0 && (
        <Bloco titulo="O que o bot colheu" descricao="O registro cru da conversa.">
          <Estados itens={outros.map(([campo, valor]) => ({ rotulo: campo, valor }))} />
        </Bloco>
      )}

      <Secao titulo="Revisão" descricao={decidido ? "Lead já decidido: os campos ficam como registro." : "Confira com a conversa ao lado e corrija o que o modelo entendeu torto."}>
        <div className="flex flex-col gap-5">
          <div className="grid gap-5 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="lead-nome">Nome</Label>
              <Input id="lead-nome" value={nome} onChange={(e) => setNome(e.target.value)} disabled={decidido} />
              <p className="text-xs text-muted-foreground">O cadastro recusa nome repetido: nome igual é o mesmo cliente.</p>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="lead-telefone">Telefone</Label>
              <Input
                id="lead-telefone"
                value={telefone}
                onChange={(e) => setTelefone(e.target.value)}
                disabled={decidido}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="lead-email">E-mail</Label>
              <Input
                id="lead-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                disabled={decidido}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="lead-observacoes">Observações</Label>
              <Textarea
                id="lead-observacoes"
                value={observacoes}
                onChange={(e) => setObservacoes(e.target.value)}
                rows={3}
                disabled={decidido}
              />
            </div>
          </div>

          {!decidido && (
            <>
              <div className="flex flex-wrap items-center gap-3">
                <Button variant="outline" onClick={() => salvar.mutate()} disabled={salvar.isPending}>
                  {salvar.isPending ? "Salvando…" : "Salvar correções"}
                </Button>
                <Button onClick={() => setConfirmarCriar(true)} disabled={nome.trim() === "" || aproveitar.isPending}>
                  Criar cliente
                </Button>
                <Button variant="ghost" onClick={() => setConfirmarDescartar(true)} disabled={descartar.isPending}>
                  Descartar
                </Button>
              </div>
              {nome.trim() === "" && (
                <p className="text-xs text-aviso">
                  Sem nome não dá para criar cliente: pegue o nome na conversa ou descarte o lead.
                </p>
              )}

              <div className="flex flex-col gap-3 rounded-lg border p-4">
                <div>
                  <p className="font-heading font-semibold">Já é cliente?</p>
                  <p className="mt-1 text-sm text-muted-foreground">
                    Alguém da carteira que escreveu no WhatsApp não precisa de cadastro novo — e o
                    cadastro recusa nome repetido. Ligue o lead ao cliente que já existe.
                  </p>
                </div>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="busca-cliente">Procurar por nome</Label>
                  <Input
                    id="busca-cliente"
                    value={buscaCliente}
                    onChange={(e) => setBuscaCliente(e.target.value)}
                    placeholder="comece a digitar…"
                  />
                </div>
                {buscaCliente.length > 1 && (
                  <ul className="flex flex-col divide-y divide-border/70">
                    {candidatos.length === 0 ? (
                      <Vazio>Nenhum cliente com esse nome.</Vazio>
                    ) : (
                      candidatos.map((c) => (
                        <li key={c.id} className="flex items-center justify-between gap-4 py-2">
                          <span className="min-w-0 truncate text-sm">{c.nome}</span>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => setLigarA(c.id)}
                            disabled={aproveitar.isPending}
                          >
                            Ligar a este
                          </Button>
                        </li>
                      ))
                    )}
                  </ul>
                )}
              </div>
            </>
          )}
        </div>
      </Secao>

      <AlertDialog open={confirmarCriar} onOpenChange={setConfirmarCriar}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Criar o cliente {nome.trim()}?</AlertDialogTitle>
            <AlertDialogDescription>
              Ele entra no cadastro da plataforma com os dados desta revisão e{" "}
              <strong>ativo em nenhum serviço</strong>: não entra em dry-run de lance nem em rodada
              de boletos até alguém ligar isso na tela dele.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmarCriar(false);
                // Salva as correções antes, para o cliente nascer com o que está na tela.
                salvar.mutate(undefined, { onSuccess: () => aproveitar.mutate(null) });
              }}
            >
              Criar cliente
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={ligarA !== null} onOpenChange={(a) => !a && setLigarA(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Ligar este lead ao cliente existente?</AlertDialogTitle>
            <AlertDialogDescription>
              Nenhum cliente novo é criado. O lead fica marcado como aproveitado e aponta para{" "}
              {candidatos.find((c) => c.id === ligarA)?.nome ?? "o cliente escolhido"}.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const id = ligarA;
                setLigarA(null);
                if (id) aproveitar.mutate(id);
              }}
            >
              Ligar
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={confirmarDescartar} onOpenChange={setConfirmarDescartar}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Descartar este lead?</AlertDialogTitle>
            <AlertDialogDescription>
              Diga por quê: é a resposta para quem perguntar, um mês depois, por que esta conversa
              não virou cliente. A conversa e os dados continuam guardados.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex flex-col gap-2">
            <Label htmlFor="motivo-descarte">Motivo</Label>
            <Input
              id="motivo-descarte"
              value={motivo}
              onChange={(e) => setMotivo(e.target.value)}
              placeholder="não era do nosso ramo"
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              disabled={motivo.trim() === ""}
              onClick={() => {
                setConfirmarDescartar(false);
                descartar.mutate();
              }}
            >
              Descartar
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
