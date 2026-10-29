"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import { idDeCampo } from "@/lib/atendimento";
import type { CampoEtapa, DetalheBot, EtapaBot } from "@/lib/tipos";

// O editor de fluxo: a parte do serviço que o usuário pediu que fosse simples de montar.
//
// A ideia do fluxo é **etapa com objetivo**, não árvore de menu (decisão do usuário em
// 29/10/2026): em cada etapa o bot persegue um objetivo e coleta alguns dados; com os
// obrigatórios coletados, ele mesmo passa para a próxima. Quem escreve a pergunta é o modelo, na
// hora, conforme o que a pessoa disse — por isso aqui não se escreve texto de pergunta, e sim
// **o que se quer descobrir**.
//
// A lista inteira é salva de uma vez (PUT .../etapas): a ordem na tela é a ordem do fluxo, e o
// que estava lá é substituído. Fluxo vazio é permitido (bot só de tirar dúvida).

type CampoRascunho = CampoEtapa & { chave: string };
type EtapaRascunho = {
  chave: string;
  nome: string;
  objetivo: string;
  instrucoes: string;
  campos: CampoRascunho[];
};

let sequencia = 0;
function novaChave(): string {
  sequencia += 1;
  return `r${sequencia}`;
}

function paraRascunho(etapas: EtapaBot[]): EtapaRascunho[] {
  return etapas.map((e) => ({
    chave: novaChave(),
    nome: e.nome,
    objetivo: e.objetivo,
    instrucoes: e.instrucoes ?? "",
    campos: e.campos.map((c) => ({ ...c, chave: novaChave() })),
  }));
}

export function FluxoDoBot({
  botId,
  etapas,
  podeEditar,
}: {
  botId: number;
  etapas: EtapaBot[];
  podeEditar: boolean;
}) {
  const queryClient = useQueryClient();
  const [rascunho, setRascunho] = useState<EtapaRascunho[]>(() => paraRascunho(etapas));
  const [mexido, setMexido] = useState(false);

  function alterar(proximo: EtapaRascunho[]) {
    setRascunho(proximo);
    setMexido(true);
  }

  const salvar = useMutation({
    mutationFn: () =>
      api<DetalheBot>(`/atendimento/bots/${botId}/etapas`, {
        metodo: "PUT",
        json: {
          etapas: rascunho.map((e) => ({
            nome: e.nome,
            objetivo: e.objetivo,
            instrucoes: e.instrucoes.trim() === "" ? null : e.instrucoes,
            campos: e.campos.map((c) => ({
              id: c.id.trim() === "" ? idDeCampo(c.rotulo) : c.id,
              rotulo: c.rotulo,
              obrigatorio: c.obrigatorio,
            })),
          })),
        },
      }),
    onSuccess: (res) => {
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bot", botId] });
      void queryClient.invalidateQueries({ queryKey: ["atendimento-bots"] });
      setRascunho(paraRascunho(res.etapas));
      setMexido(false);
      toast.success(res.etapas.length === 0 ? "Fluxo apagado" : "Fluxo salvo");
    },
    onError: (e) => toast.error(e.message),
  });

  function etapaNova() {
    alterar([
      ...rascunho,
      { chave: novaChave(), nome: "", objetivo: "", instrucoes: "", campos: [] },
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
    <div className="flex flex-col gap-5">
      {rascunho.length === 0 ? (
        <Alert>
          <AlertDescription>
            Sem etapas, o bot só conversa: responde o que as instruções permitem e encerra quando o
            assunto acaba. Para ele <strong>descobrir</strong> algo de quem escreve — o que a pessoa
            procura, o telefone, o melhor horário —, acrescente uma etapa.
          </AlertDescription>
        </Alert>
      ) : (
        rascunho.map((etapa, i) => (
          <CartaoEtapa
            key={etapa.chave}
            etapa={etapa}
            posicao={i + 1}
            total={rascunho.length}
            podeEditar={podeEditar}
            aoMudar={(nova) => alterar(rascunho.map((e, j) => (j === i ? nova : e)))}
            aoRemover={() => alterar(rascunho.filter((_, j) => j !== i))}
            aoMover={(d) => mover(i, d)}
          />
        ))
      )}

      {podeEditar && (
        <div className="flex flex-wrap items-center gap-3">
          <Button type="button" variant="outline" onClick={etapaNova}>
            Acrescentar etapa
          </Button>
          <Button type="button" onClick={() => salvar.mutate()} disabled={!mexido || salvar.isPending}>
            {salvar.isPending ? "Salvando…" : "Salvar fluxo"}
          </Button>
          {mexido && !salvar.isPending && (
            <span className="text-sm text-aviso">Há mudanças não salvas no fluxo.</span>
          )}
        </div>
      )}
    </div>
  );
}

function CartaoEtapa({
  etapa,
  posicao,
  total,
  podeEditar,
  aoMudar,
  aoRemover,
  aoMover,
}: {
  etapa: EtapaRascunho;
  posicao: number;
  total: number;
  podeEditar: boolean;
  aoMudar: (e: EtapaRascunho) => void;
  aoRemover: () => void;
  aoMover: (direcao: -1 | 1) => void;
}) {
  const idBase = `etapa-${etapa.chave}`;
  return (
    <div className="flex flex-col gap-4 rounded-xl border bg-card/50 p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Badge variant="outline">
          Etapa {posicao} de {total}
        </Badge>
        {podeEditar && (
          <div className="flex items-center gap-2">
            <Button type="button" variant="ghost" size="sm" onClick={() => aoMover(-1)} disabled={posicao === 1}>
              Subir
            </Button>
            <Button type="button" variant="ghost" size="sm" onClick={() => aoMover(1)} disabled={posicao === total}>
              Descer
            </Button>
            <Button type="button" variant="ghost" size="sm" onClick={aoRemover}>
              Remover
            </Button>
          </div>
        )}
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-2">
          <Label htmlFor={`${idBase}-nome`}>Nome da etapa</Label>
          <Input
            id={`${idBase}-nome`}
            value={etapa.nome}
            onChange={(e) => aoMudar({ ...etapa, nome: e.target.value })}
            placeholder="Interesse"
            disabled={!podeEditar}
          />
          <p className="text-xs text-muted-foreground">Só para a equipe se entender; o cliente não vê.</p>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor={`${idBase}-objetivo`}>Objetivo</Label>
          <Input
            id={`${idBase}-objetivo`}
            value={etapa.objetivo}
            onChange={(e) => aoMudar({ ...etapa, objetivo: e.target.value })}
            placeholder="Descobrir que tipo de imóvel a pessoa procura"
            disabled={!podeEditar}
          />
          <p className="text-xs text-muted-foreground">
            O que o bot persegue aqui. Ele escolhe as palavras da pergunta.
          </p>
        </div>
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor={`${idBase}-instrucoes`}>Orientação desta etapa (opcional)</Label>
        <Textarea
          id={`${idBase}-instrucoes`}
          value={etapa.instrucoes}
          onChange={(e) => aoMudar({ ...etapa, instrucoes: e.target.value })}
          rows={2}
          placeholder="Se a pessoa falar de compra, explique que aqui só tratamos de aluguel."
          disabled={!podeEditar}
        />
      </div>

      <Campos
        idBase={idBase}
        campos={etapa.campos}
        podeEditar={podeEditar}
        aoMudar={(campos) => aoMudar({ ...etapa, campos })}
      />
    </div>
  );
}

function Campos({
  idBase,
  campos,
  podeEditar,
  aoMudar,
}: {
  idBase: string;
  campos: CampoRascunho[];
  podeEditar: boolean;
  aoMudar: (campos: CampoRascunho[]) => void;
}) {
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border/70 p-4">
      <div>
        <p className="text-sm font-medium">Dados a coletar</p>
        <p className="mt-1 text-xs text-muted-foreground">
          O que o bot precisa obter antes de passar à etapa seguinte. Os marcados como obrigatórios
          seguram a etapa — a plataforma confere, não o modelo.
        </p>
      </div>

      {campos.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          Nenhum dado nesta etapa: ela serve só para orientar a conversa.
        </p>
      ) : (
        <ul className="flex flex-col gap-3">
          {campos.map((campo, i) => (
            <li key={campo.chave} className="grid items-end gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto_auto]">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor={`${idBase}-rotulo-${campo.chave}`} className="text-xs">
                  Como a equipe lê
                </Label>
                <Input
                  id={`${idBase}-rotulo-${campo.chave}`}
                  value={campo.rotulo}
                  onChange={(e) => {
                    const rotulo = e.target.value;
                    // Enquanto ninguém mexer no id, ele acompanha o rótulo.
                    const idAtual = campo.id === idDeCampo(campo.rotulo) || campo.id === "" ? idDeCampo(rotulo) : campo.id;
                    aoMudar(campos.map((c, j) => (j === i ? { ...c, rotulo, id: idAtual } : c)));
                  }}
                  placeholder="Telefone"
                  disabled={!podeEditar}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor={`${idBase}-id-${campo.chave}`} className="text-xs">
                  Nome do dado
                </Label>
                <Input
                  id={`${idBase}-id-${campo.chave}`}
                  value={campo.id}
                  onChange={(e) => aoMudar(campos.map((c, j) => (j === i ? { ...c, id: e.target.value } : c)))}
                  placeholder="telefone"
                  disabled={!podeEditar}
                />
              </div>
              <div className="flex flex-col items-center gap-1.5">
                <Label htmlFor={`${idBase}-obrig-${campo.chave}`} className="text-xs">
                  Obrigatório
                </Label>
                <Switch
                  id={`${idBase}-obrig-${campo.chave}`}
                  checked={campo.obrigatorio}
                  onCheckedChange={(v) =>
                    aoMudar(campos.map((c, j) => (j === i ? { ...c, obrigatorio: v } : c)))
                  }
                  disabled={!podeEditar}
                />
              </div>
              {podeEditar && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => aoMudar(campos.filter((_, j) => j !== i))}
                >
                  Tirar
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}

      {podeEditar && (
        <div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() =>
              aoMudar([...campos, { chave: novaChave(), id: "", rotulo: "", obrigatorio: true }])
            }
          >
            Acrescentar dado
          </Button>
        </div>
      )}
    </div>
  );
}
