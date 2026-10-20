"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { Colunas, Secao } from "@/components/pagina";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import { dataHora, useSessao } from "@/lib/sessao";
import {
  corDoStatusRodada,
  mesLegivel,
  rotuloStatusRodada,
  rotuloTipoRodada,
} from "@/lib/boletos";
import type {
  CampoMensagem,
  DetalheRodada,
  RespostaConfiguracaoBoletos,
  RodadaResumo,
  TipoRodada,
} from "@/lib/tipos";

// Serviço de boletos da Canopus (issue #48): as rodadas do mês e a configuração.
//
// Duas ideias que a tela precisa deixar claras, porque mandar mensagem para cliente não se
// desfaz: (1) com o envio desligado a rodada só MONTA as mensagens, e dá para ler o que sairia;
// (2) a cobrança só avisa quem recebeu o boleto e continua com a parcela em aberto.

export default function PaginaBoletos() {
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
    <div className="flex flex-col gap-12">
      {config.isError && (
        <Alert variant="destructive" className="max-w-2xl">
          <AlertDescription>{config.error.message}</AlertDescription>
        </Alert>
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

      {/* O aviso de ensaio fica ao lado da configuração: é ela que o explica — e é lá que se
          liga ou desliga o envio. */}
      {config.data && (
        <Colunas lateral={<AvisoEnsaio dados={config.data} />}>
          <Configuracao
            // Configuração salva remonta o formulário com os valores novos, sem efeito.
            key={JSON.stringify(config.data.configuracao)}
            dados={config.data}
            podeEditar={podeCriar}
            podeLigarEnvio={config.data.pode_ligar_envio && sessao.data?.usuario.perfil === "admin"}
          />
        </Colunas>
      )}
    </div>
  );
}

/** O aviso mais importante da tela: se nada será enviado, e por quê. */
function AvisoEnsaio({ dados }: { dados: RespostaConfiguracaoBoletos }) {
  const { envio_habilitado_servidor: servidor, configuracao } = dados;
  if (servidor && configuracao.envio_habilitado) {
    return (
      <Alert>
        <AlertTitle>O envio está ligado</AlertTitle>
        <AlertDescription>
          As rodadas mandam o boleto no WhatsApp dos clientes, uma mensagem a cada{" "}
          {configuracao.pausa_envio_segundos}s, no máximo {configuracao.max_mensagens_rodada} por rodada.
          {configuracao.envio_habilitado_por_nome && ` Ligado por ${configuracao.envio_habilitado_por_nome}.`}
        </AlertDescription>
      </Alert>
    );
  }
  return (
    <Alert>
      <AlertTitle>Modo ensaio: nenhuma mensagem é enviada</AlertTitle>
      <AlertDescription>
        {!servidor
          ? "A chave BOLETOS_ENVIO_HABILITADO do servidor está desligada."
          : "O envio está desligado na configuração abaixo."}{" "}
        As rodadas continuam buscando os boletos e <strong>montam</strong> as mensagens: abra uma rodada
        para ler exatamente o que seria enviado a cada cliente.
      </AlertDescription>
    </Alert>
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
        <AlertDescription>
          Crie uma rodada de envio para a plataforma buscar os boletos em aberto dos clientes.
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

type Formulario = {
  agendamento: boolean;
  diaEnvio: string;
  horaEnvio: string;
  diaCobranca: string;
  horaCobranca: string;
  anteciparDiaUtil: boolean;
  envio: boolean;
  pausa: string;
  teto: string;
  textoEnvio: string;
  textoCobranca: string;
};

function paraFormulario(c: RespostaConfiguracaoBoletos["configuracao"]): Formulario {
  return {
    agendamento: c.agendamento_ativo,
    diaEnvio: String(c.dia_envio),
    horaEnvio: String(c.hora_envio).padStart(2, "0"),
    diaCobranca: String(c.dia_cobranca),
    horaCobranca: String(c.hora_cobranca).padStart(2, "0"),
    anteciparDiaUtil: c.antecipar_dia_util,
    envio: c.envio_habilitado,
    pausa: String(c.pausa_envio_segundos),
    teto: String(c.max_mensagens_rodada),
    textoEnvio: c.texto_envio,
    textoCobranca: c.texto_cobranca,
  };
}

const dias = Array.from({ length: 31 }, (_, i) => String(i + 1));
const horas = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, "0"));

function Configuracao({
  dados,
  podeEditar,
  podeLigarEnvio,
}: {
  dados: RespostaConfiguracaoBoletos;
  podeEditar: boolean;
  podeLigarEnvio: boolean;
}) {
  const queryClient = useQueryClient();
  const c = dados.configuracao;
  const [form, setForm] = useState<Formulario>(() => paraFormulario(c));
  const mudou = JSON.stringify(form) !== JSON.stringify(paraFormulario(c));
  const mudar = (parte: Partial<Formulario>) => setForm((f) => ({ ...f, ...parte }));

  const salvar = useMutation({
    mutationFn: () =>
      api<RespostaConfiguracaoBoletos>("/boletos/configuracao", {
        metodo: "PUT",
        json: {
          agendamento_ativo: form.agendamento,
          dia_envio: Number(form.diaEnvio),
          hora_envio: Number(form.horaEnvio),
          dia_cobranca: Number(form.diaCobranca),
          hora_cobranca: Number(form.horaCobranca),
          antecipar_dia_util: form.anteciparDiaUtil,
          envio_habilitado: form.envio,
          pausa_envio_segundos: Number(form.pausa),
          max_mensagens_rodada: Number(form.teto),
          texto_envio: form.textoEnvio,
          texto_cobranca: form.textoCobranca,
        },
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["boletos-configuracao"] });
      toast.success("Configuração salva");
    },
    onError: (e) => toast.error(e.message),
  });

  const seletor = (
    id: string,
    rotulo: string,
    valor: string,
    opcoes: string[],
    aoMudar: (v: string) => void,
    desabilitado: boolean,
  ) => (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{rotulo}</Label>
      <Select items={opcoes.map((o) => ({ value: o, label: o }))} value={valor} onValueChange={(v) => aoMudar(v ?? valor)} disabled={desabilitado}>
        <SelectTrigger id={id} className="w-24">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {opcoes.map((o) => (
            <SelectItem key={o} value={o}>
              {o}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );

  return (
    <Secao
      titulo="Configuração"
      descricao={
        podeEditar
          ? "Admin e operador ajustam o agendamento; ligar o envio no WhatsApp é só do admin."
          : "Você pode ver a configuração, mas não alterá-la."
      }
      className="max-w-[46rem]"
    >
      <div className="flex flex-col gap-4 rounded-lg border p-4">
        <Label className="flex items-center gap-3 font-normal">
          <Switch
            checked={form.agendamento}
            onCheckedChange={(v) => mudar({ agendamento: v })}
            disabled={!podeEditar}
          />
          Rodar as rodadas automaticamente todo mês
        </Label>
        <div className="flex flex-wrap gap-4">
          {seletor("dia-envio", "Dia do envio", form.diaEnvio, dias, (v) => mudar({ diaEnvio: v }), !podeEditar)}
          {seletor("hora-envio", "Hora", form.horaEnvio, horas, (v) => mudar({ horaEnvio: v }), !podeEditar)}
          {seletor("dia-cobranca", "Dia da cobrança", form.diaCobranca, dias, (v) => mudar({ diaCobranca: v }), !podeEditar)}
          {seletor("hora-cobranca", "Hora", form.horaCobranca, horas, (v) => mudar({ horaCobranca: v }), !podeEditar)}
        </div>
        <Label className="flex items-center gap-3 font-normal">
          <Switch
            checked={form.anteciparDiaUtil}
            onCheckedChange={(v) => mudar({ anteciparDiaUtil: v })}
            disabled={!podeEditar}
          />
          Antecipar para o dia útil anterior quando cair em sábado, domingo ou feriado
        </Label>
        <p className="text-sm text-muted-foreground">
          Próximo envio: <strong>{dataHora(c.proximo_envio)}</strong>
          {c.motivo_envio && ` (${c.motivo_envio})`}. Próxima cobrança:{" "}
          <strong>{dataHora(c.proxima_cobranca)}</strong>
          {c.motivo_cobranca && ` (${c.motivo_cobranca})`}.
        </p>
      </div>

      <div className="flex flex-col gap-4 rounded-lg border border-destructive/50 p-4">
        <div>
          <h3 className="font-medium">Envio no WhatsApp dos clientes</h3>
          <p className="text-sm text-muted-foreground">
            Ligado, a plataforma manda o boleto e a cobrança <strong>para os clientes</strong>, sem ninguém
            conferir mensagem por mensagem. Mensagem enviada não se desfaz.
          </p>
        </div>
        {!dados.envio_habilitado_servidor && (
          <Alert>
            <AlertDescription>
              A chave <code>BOLETOS_ENVIO_HABILITADO</code> do servidor está desligada: enquanto ela estiver
              assim, esta opção não pode ser ligada.
            </AlertDescription>
          </Alert>
        )}
        <Label className="flex items-center gap-3 font-normal">
          <Switch
            checked={form.envio}
            onCheckedChange={(v) => mudar({ envio: v })}
            disabled={!podeLigarEnvio || !dados.envio_habilitado_servidor}
          />
          Enviar de verdade (desligado: as mensagens são montadas e não saem)
        </Label>
        <div className="flex flex-wrap gap-4">
          <div className="grid gap-1.5">
            <Label htmlFor="pausa">Pausa entre mensagens (s)</Label>
            <Input
              id="pausa"
              type="number"
              min={5}
              max={600}
              className="w-28"
              value={form.pausa}
              onChange={(e) => mudar({ pausa: e.target.value })}
              disabled={!podeEditar}
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="teto">Máximo por rodada</Label>
            <Input
              id="teto"
              type="number"
              min={1}
              max={5000}
              className="w-28"
              value={form.teto}
              onChange={(e) => mudar({ teto: e.target.value })}
              disabled={!podeEditar}
            />
          </div>
        </div>
        <p className="text-xs text-muted-foreground">
          A pausa protege o número de ser bloqueado pelo WhatsApp: quanto menor, maior o risco.
        </p>
      </div>

      {/* O texto que chega ao cliente. É uma mensagem só para todos: o que muda são os campos
          entre chaves, que a plataforma preenche na hora do envio. */}
      <div className="flex flex-col gap-4">
        <div>
          <h3 className="font-medium">Mensagens</h3>
          <p className="text-sm text-muted-foreground">
            A mesma mensagem vai para todos os clientes — ninguém edita nada cliente a cliente. Os campos
            entre chaves são preenchidos no envio. Vale da próxima rodada em diante: mensagem que já está
            na fila não muda.
          </p>
        </div>
        <ModeloMensagem
          id="texto-envio"
          titulo="Boleto (dia do envio)"
          descricao="Vai junto com o PDF do boleto, uma por cota."
          valor={form.textoEnvio}
          campos={dados.mensagens.campos_envio}
          padrao={dados.mensagens.padrao_envio}
          max={dados.mensagens.max_caracteres}
          desabilitado={!podeEditar}
          aoMudar={(texto) => mudar({ textoEnvio: texto })}
        />
        <ModeloMensagem
          id="texto-cobranca"
          titulo="Cobrança (dias depois)"
          descricao="Vai para quem continua com a parcela em aberto, uma por cliente, sem anexo."
          valor={form.textoCobranca}
          campos={dados.mensagens.campos_cobranca}
          padrao={dados.mensagens.padrao_cobranca}
          max={dados.mensagens.max_caracteres}
          desabilitado={!podeEditar}
          aoMudar={(texto) => mudar({ textoCobranca: texto })}
        />
      </div>

      {dados.fila.pendentes + dados.fila.falharam > 0 && (
        <p className="text-sm text-muted-foreground">
          Fila do WhatsApp: {dados.fila.pendentes} esperando, {dados.fila.falharam} sem entregar.
        </p>
      )}

      {podeEditar && (
        <div className="flex items-center gap-3">
          <Button onClick={() => salvar.mutate()} disabled={!mudou || salvar.isPending}>
            {salvar.isPending ? "Salvando…" : "Salvar configuração"}
          </Button>
          {c.atualizado_por_nome && (
            <span className="text-xs text-muted-foreground">
              Última alteração por {c.atualizado_por_nome} em {dataHora(c.atualizado_em)}
            </span>
          )}
        </div>
      )}
    </Secao>
  );
}

/**
 * Uma caixa de texto com a lista dos campos e a prévia do que o cliente vai receber.
 *
 * A prévia usa os exemplos que a API manda e segue a mesma regra do servidor: um pedaço da
 * linha que depende de um campo vazio some junto com o separador " · ". Quem valida de verdade
 * é a API — aqui é só para ninguém salvar às cegas.
 */
function ModeloMensagem({
  id,
  titulo,
  descricao,
  valor,
  campos,
  padrao,
  max,
  desabilitado,
  aoMudar,
}: {
  id: string;
  titulo: string;
  descricao: string;
  valor: string;
  campos: CampoMensagem[];
  padrao: string;
  max: number;
  desabilitado: boolean;
  aoMudar: (texto: string) => void;
}) {
  const previa = renderizarPrevia(valor, campos);
  return (
    <div className="flex flex-col gap-3 rounded-lg border p-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <Label htmlFor={id} className="text-base font-medium">
            {titulo}
          </Label>
          <p className="mt-0.5 text-sm text-muted-foreground">{descricao}</p>
        </div>
        {!desabilitado && (
          <Button type="button" variant="ghost" size="sm" disabled={valor === padrao} onClick={() => aoMudar(padrao)}>
            Voltar ao texto padrão
          </Button>
        )}
      </div>

      <Textarea
        id={id}
        rows={9}
        maxLength={max}
        value={valor}
        disabled={desabilitado}
        onChange={(e) => aoMudar(e.target.value)}
      />

      <div className="flex flex-col gap-2">
        <p className="text-xs text-muted-foreground">
          {valor.length}/{max} caracteres · campos desta mensagem:
        </p>
        <ul className="flex flex-wrap gap-x-4 gap-y-1">
          {campos.map((c) => (
            <li key={c.nome} className="text-xs text-muted-foreground">
              <code className="font-mono text-foreground">{`{${c.nome}}`}</code> {c.descricao}
            </li>
          ))}
        </ul>
      </div>

      <div>
        <p className="text-xs text-muted-foreground">Prévia, com dados de exemplo</p>
        <p className="mt-1 rounded-lg bg-muted/40 p-3 text-sm whitespace-pre-wrap">{previa}</p>
      </div>
    </div>
  );
}

/** Mesma regra do servidor (api/internal/mensagem): campo vazio leva o pedaço e o separador. */
function renderizarPrevia(modelo: string, campos: CampoMensagem[]): string {
  const exemplos = new Map(campos.map((c) => [c.nome, c.exemplo]));
  const linhas: string[] = [];
  for (const linha of modelo.split("\n")) {
    const pedacos = linha.split("·");
    const mantidos: string[] = [];
    for (const pedaco of pedacos) {
      let vazio = false;
      const texto = pedaco.replace(/\{([^{}]*)\}/g, (achado, nome: string) => {
        const exemplo = exemplos.get(nome.trim());
        if (exemplo === undefined) return achado;
        if (exemplo.trim() === "") vazio = true;
        return exemplo;
      });
      if (!vazio) mantidos.push(texto.trim());
    }
    if (pedacos.length > 0 && mantidos.length === 0) continue;
    linhas.push(mantidos.join(" · "));
  }
  return linhas.join("\n").replace(/\n{3,}/g, "\n\n").trim();
}
