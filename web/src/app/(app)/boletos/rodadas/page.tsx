"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { AvisoEnsaio } from "@/components/aviso-ensaio";
import { Secao } from "@/components/pagina";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api } from "@/lib/api";
import { dataHora, useSessao } from "@/lib/sessao";
import { corDoStatusRodada, mesLegivel, rotuloStatusRodada, rotuloTipoRodada } from "@/lib/boletos";
import type { DetalheRodada, RespostaConfiguracaoBoletos, RodadaResumo, TipoRodada } from "@/lib/tipos";

// As rodadas do serviço de boletos: a operação do dia a dia. Uma rodada é uma passada pelo
// cadastro — 'envio' busca os boletos em aberto do mês e guarda o PDF, 'cobranca' pergunta de
// novo depois para saber o que continua aberto.
//
// Duas ideias que a tela precisa deixar claras, porque mandar mensagem para cliente não se
// desfaz: (1) com o envio desligado a rodada só MONTA as mensagens, e dá para ler o que sairia;
// (2) a cobrança só avisa quem recebeu o boleto e continua com a parcela em aberto. O que liga e
// desliga esse envio está em Configurações; aqui fica o aviso de como a plataforma está agora.

export default function PaginaRodadas() {
  const sessao = useSessao();
  const config = useQuery({
    queryKey: ["boletos-configuracao"],
    queryFn: () => api<RespostaConfiguracaoBoletos>("/boletos/configuracao"),
  });
  const rodadas = useQuery({
    queryKey: ["boletos-rodadas"],
    queryFn: () => api<{ rodadas: RodadaResumo[] }>("/boletos/rodadas"),
    // Rodada em andamento muda sozinha: o worker vai passando pelos clientes.
    refetchInterval: (q) =>
      q.state.data?.rodadas.some((r) => r.status === "na_fila" || r.status === "em_andamento") ? 5000 : 30000,
  });

  const podeCriar = (config.data?.pode_editar ?? false) && sessao.data?.usuario.perfil !== "leitura";

  return (
    <div className="flex flex-col gap-8">
      {config.isError && (
        <Alert variant="destructive" className="max-w-2xl">
          <AlertDescription>{config.error.message}</AlertDescription>
        </Alert>
      )}

      {config.data && (
        <div className="flex flex-col gap-2 xl:max-w-4xl">
          <AvisoEnsaio dados={config.data} />
          <p className="text-sm text-muted-foreground">
            O interruptor do envio, o agendamento e a pausa entre mensagens ficam em{" "}
            <Link href="/boletos/configuracao" className="underline underline-offset-4">
              Configurações
            </Link>
            .
          </p>
        </div>
      )}

      <Secao
        titulo="Rodadas"
        descricao="Uma rodada percorre os clientes ativos em Boletos que têm CPF/CNPJ cadastrado e consulta o Boleto Fácil."
        acoes={podeCriar && <NovaRodada />}
      >
        {rodadas.isError ? (
          <Alert variant="destructive">
            <AlertDescription>{rodadas.error.message}</AlertDescription>
          </Alert>
        ) : rodadas.data ? (
          <TabelaRodadas rodadas={rodadas.data.rodadas} />
        ) : (
          <Skeleton className="h-40 w-full" />
        )}
      </Secao>
    </div>
  );
}

function NovaRodada() {
  const queryClient = useQueryClient();
  const [tipo, setTipo] = useState<TipoRodada>("envio");

  const criar = useMutation({
    mutationFn: () => api<DetalheRodada>("/boletos/rodadas", { metodo: "POST", json: { tipo } }),
    onSuccess: (d) => {
      void queryClient.invalidateQueries({ queryKey: ["boletos-rodadas"] });
      toast.success(`Rodada de ${rotuloTipoRodada[d.rodada.tipo]} na fila`);
    },
    onError: (e) => toast.error(e.message),
  });

  const tipos: { value: TipoRodada; label: string }[] = [
    { value: "envio", label: "Envio: buscar e mandar os boletos" },
    { value: "cobranca", label: "Cobrança: avisar quem não pagou" },
  ];

  return (
    <div className="flex flex-wrap items-end gap-2">
      <div className="grid gap-1.5">
        <Label htmlFor="tipo-rodada">Nova rodada</Label>
        <Select items={tipos} value={tipo} onValueChange={(v) => setTipo((v as TipoRodada) ?? "envio")}>
          <SelectTrigger id="tipo-rodada" className="min-w-72">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {tipos.map((t) => (
              <SelectItem key={t.value} value={t.value}>
                {t.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <Button onClick={() => criar.mutate()} disabled={criar.isPending}>
        {criar.isPending ? "Criando…" : "Criar rodada"}
      </Button>
    </div>
  );
}

function TabelaRodadas({ rodadas }: { rodadas: RodadaResumo[] }) {
  if (rodadas.length === 0) {
    return (
      <Alert>
        <AlertTitle>Nenhuma rodada ainda</AlertTitle>
        <AlertDescription className="flex flex-col items-start gap-3">
          <span>Crie uma rodada de envio para a plataforma buscar os boletos em aberto dos clientes.</span>
          <Link href="/boletos/painel" className={buttonVariants({ variant: "outline", size: "sm" })}>
            Ver o painel do serviço
          </Link>
        </AlertDescription>
      </Alert>
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Rodada</TableHead>
          <TableHead>Mês</TableHead>
          <TableHead>Situação</TableHead>
          <TableHead className="text-right">Clientes</TableHead>
          <TableHead className="text-right">Boletos</TableHead>
          <TableHead>Quando</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rodadas.map((r) => (
          <TableRow key={r.id}>
            <TableCell>
              <Link href={`/boletos/rodadas/${r.id}`} className="font-medium underline-offset-4 hover:underline">
                nº {r.id} · {rotuloTipoRodada[r.tipo]}
              </Link>
              {r.origem === "agendada" && <span className="ml-2 text-xs text-muted-foreground">automática</span>}
            </TableCell>
            <TableCell>{mesLegivel(r.referencia)}</TableCell>
            <TableCell>
              <Badge variant={corDoStatusRodada(r.status)}>{rotuloStatusRodada[r.status]}</Badge>
              {r.com_erro > 0 && <span className="ml-2 text-xs text-destructive">{r.com_erro} com erro</span>}
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {r.consultados + r.sem_boleto}/{r.total}
            </TableCell>
            <TableCell className="text-right tabular-nums">{r.parcelas}</TableCell>
            <TableCell className="text-sm text-muted-foreground">
              {dataHora(r.finalizada_em ?? r.criada_em)}
              <span className="block text-xs">por {r.criada_por_nome}</span>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
