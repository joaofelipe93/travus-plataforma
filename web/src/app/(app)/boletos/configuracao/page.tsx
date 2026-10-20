"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { AvisoEnsaio } from "@/components/aviso-ensaio";
import { Colunas, Secao } from "@/components/pagina";
import { Bloco, Estados } from "@/components/painel";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import { dataHora, useSessao } from "@/lib/sessao";
import type { CampoMensagem, RespostaConfiguracaoBoletos } from "@/lib/tipos";

// Configuração do serviço de boletos: o agendamento das rodadas do mês (dias 8 e 14) e o
// **envio no WhatsApp dos clientes**, que é a única parte da plataforma que manda mensagem para
// o telefone de um cliente de verdade. Por isso ela fica cercada de vermelho e só admin liga.
//
// Com o envio desligado, a rodada busca os boletos e MONTA as mensagens sem mandar nada (ensaio):
// dá para abrir a rodada e ler exatamente o que sairia. É a configuração que explica esse estado,
// então o aviso de ensaio mora aqui, ao lado do interruptor.

export default function PaginaConfiguracaoBoletos() {
  const sessao = useSessao();
  const config = useQuery({
    queryKey: ["boletos-configuracao"],
    queryFn: () => api<RespostaConfiguracaoBoletos>("/boletos/configuracao"),
  });

  if (config.isError) {
    return (
      <Alert variant="destructive" className="max-w-2xl">
        <AlertDescription>{config.error.message}</AlertDescription>
      </Alert>
    );
  }
  if (!config.data) {
    return <Skeleton className="h-96 w-full max-w-3xl" />;
  }

  const perfil = sessao.data?.usuario.perfil;
  return (
    <Colunas lateral={<Lateral dados={config.data} />}>
      <Configuracao
        // Configuração salva remonta o formulário com os valores novos, sem efeito.
        key={JSON.stringify(config.data.configuracao)}
        dados={config.data}
        podeEditar={config.data.pode_editar && perfil !== "leitura"}
        podeLigarEnvio={config.data.pode_ligar_envio && perfil === "admin"}
      />
    </Colunas>
  );
}

function Lateral({ dados }: { dados: RespostaConfiguracaoBoletos }) {
  const c = dados.configuracao;
  return (
    <>
      <AvisoEnsaio dados={dados} />
      <Bloco
        titulo="Próximas rodadas"
        descricao={c.agendamento_ativo ? "Horário de Brasília." : "O agendamento está desligado: as rodadas só saem à mão."}
      >
        <Estados
          itens={[
            {
              rotulo: "Envio",
              valor: c.agendamento_ativo ? dataHora(c.proximo_envio) : "—",
              detalhe: c.agendamento_ativo ? c.motivo_envio || undefined : undefined,
            },
            {
              rotulo: "Cobrança",
              valor: c.agendamento_ativo ? dataHora(c.proxima_cobranca) : "—",
              detalhe: c.agendamento_ativo ? c.motivo_cobranca || undefined : undefined,
            },
          ]}
        />
      </Bloco>
      <Bloco
        titulo="Número dos clientes"
        descricao="O WhatsApp que fala no privado de cada cliente — não é o das reservas."
        acoes={
          <Link href="/boletos/conexoes" className={buttonVariants({ variant: "outline", size: "sm" })}>
            Conexões
          </Link>
        }
      >
        <Estados
          itens={[
            {
              rotulo: "Fila de mensagens",
              valor: `${dados.fila.pendentes} esperando`,
              tom: dados.fila.falharam > 0 ? "erro" : "neutro",
              detalhe: dados.fila.falharam > 0 ? `${dados.fila.falharam} sem entregar` : undefined,
            },
          ]}
        />
      </Bloco>
    </>
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
      <Select
        items={opcoes.map((o) => ({ value: o, label: o }))}
        value={valor}
        onValueChange={(v) => aoMudar(v ?? valor)}
        disabled={desabilitado}
      >
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
