"use client";

// Campos dos formulários do CRM do Canopus (cliente e cota). Ficam aqui porque as telas de
// novo cliente, detalhe do cliente e cotas usam os mesmos. A validação de verdade é da API:
// aqui só ajudamos a preencher (tipo do campo, opções, obrigatórios).

import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { nomeModalidade } from "@/lib/sessao";
import type { ClienteFormulario, Cota, CotaFormulario } from "@/lib/tipos";

export function clienteVazio(): ClienteFormulario {
  return { nome: "", telefone: "", email: "" };
}

export function cotaVazia(): CotaFormulario {
  return {
    administradora: "CANOPUS",
    grupo: "",
    cota: "",
    versao: "",
    tipo_consorcio: "",
    modalidade_padrao: "segundo_fixo",
    ativa: true,
    vendedor: "",
    forma_pagamento: "",
    vencimento_parcela: null,
    dia_assembleia: null,
    contratacao: "",
  };
}

export function cotaParaFormulario(c: Cota): CotaFormulario {
  return {
    administradora: c.administradora,
    grupo: c.grupo,
    cota: c.cota,
    versao: c.versao,
    tipo_consorcio: c.tipo_consorcio ?? "",
    modalidade_padrao: c.modalidade_padrao,
    ativa: c.ativa,
    vendedor: c.vendedor ?? "",
    forma_pagamento: c.forma_pagamento ?? "",
    vencimento_parcela: c.vencimento_parcela,
    dia_assembleia: c.dia_assembleia,
    contratacao: c.contratacao ?? "",
  };
}

/** Grupo e cota são o mínimo para a cota existir no Newcon. */
export function cotaPreenchida(c: CotaFormulario) {
  return c.grupo.trim() !== "" && c.cota.trim() !== "";
}

type Campo = { id: string; rotulo: string; dica?: string };

function Campo({ id, rotulo, dica, children }: Campo & { children: React.ReactNode }) {
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{rotulo}</Label>
      {children}
      {dica && <p className="text-xs text-muted-foreground">{dica}</p>}
    </div>
  );
}

export function CamposCliente({
  valor,
  aoMudar,
  prefixo = "cliente",
}: {
  valor: ClienteFormulario;
  aoMudar: (v: ClienteFormulario) => void;
  prefixo?: string;
}) {
  return (
    <div className="grid gap-4 sm:grid-cols-3">
      <Campo id={`${prefixo}-nome`} rotulo="Nome">
        <Input
          id={`${prefixo}-nome`}
          required
          autoComplete="off"
          value={valor.nome}
          onChange={(e) => aoMudar({ ...valor, nome: e.target.value })}
        />
      </Campo>
      <Campo id={`${prefixo}-telefone`} rotulo="Telefone">
        <Input
          id={`${prefixo}-telefone`}
          autoComplete="off"
          placeholder="(11) 90000-0000"
          value={valor.telefone}
          onChange={(e) => aoMudar({ ...valor, telefone: e.target.value })}
        />
      </Campo>
      <Campo id={`${prefixo}-email`} rotulo="E-mail">
        <Input
          id={`${prefixo}-email`}
          type="email"
          autoComplete="off"
          value={valor.email}
          onChange={(e) => aoMudar({ ...valor, email: e.target.value })}
        />
      </Campo>
    </div>
  );
}

const modalidades = Object.entries(nomeModalidade).map(([value, label]) => ({ value, label }));

export function CamposCota({
  valor,
  aoMudar,
  prefixo,
  travarIdentidade = false,
}: {
  valor: CotaFormulario;
  aoMudar: (v: CotaFormulario) => void;
  prefixo: string;
  /** Cota com lance ou execução: a API recusa mudar administradora, grupo, cota e versão. */
  travarIdentidade?: boolean;
}) {
  const numero = (texto: string) => (texto.trim() === "" ? null : Number(texto));
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <Campo id={`${prefixo}-grupo`} rotulo="Grupo" dica="Só números; os zeros à esquerda entram sozinhos.">
        <Input
          id={`${prefixo}-grupo`}
          required
          inputMode="numeric"
          disabled={travarIdentidade}
          value={valor.grupo}
          onChange={(e) => aoMudar({ ...valor, grupo: e.target.value })}
        />
      </Campo>
      <Campo id={`${prefixo}-cota`} rotulo="Cota">
        <Input
          id={`${prefixo}-cota`}
          required
          inputMode="numeric"
          disabled={travarIdentidade}
          value={valor.cota}
          onChange={(e) => aoMudar({ ...valor, cota: e.target.value })}
        />
      </Campo>
      <Campo id={`${prefixo}-versao`} rotulo="Versão" dica="Em branco: 00.">
        <Input
          id={`${prefixo}-versao`}
          inputMode="numeric"
          disabled={travarIdentidade}
          value={valor.versao}
          onChange={(e) => aoMudar({ ...valor, versao: e.target.value })}
        />
      </Campo>
      <Campo id={`${prefixo}-tipo`} rotulo="Tipo de consórcio" dica="Imóvel, automóvel…">
        <Input
          id={`${prefixo}-tipo`}
          value={valor.tipo_consorcio}
          onChange={(e) => aoMudar({ ...valor, tipo_consorcio: e.target.value })}
        />
      </Campo>

      <Campo id={`${prefixo}-modalidade`} rotulo="Modalidade do lance">
        <Select
          items={modalidades}
          value={valor.modalidade_padrao}
          onValueChange={(v) => aoMudar({ ...valor, modalidade_padrao: v ?? "segundo_fixo" })}
        >
          <SelectTrigger id={`${prefixo}-modalidade`}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {modalidades.map((m) => (
              <SelectItem key={m.value} value={m.value}>
                {m.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Campo>
      <Campo id={`${prefixo}-vendedor`} rotulo="Vendedor">
        <Input id={`${prefixo}-vendedor`} value={valor.vendedor} onChange={(e) => aoMudar({ ...valor, vendedor: e.target.value })} />
      </Campo>
      <Campo id={`${prefixo}-pagamento`} rotulo="Forma de pagamento" dica="Boleto, PIX…">
        <Input
          id={`${prefixo}-pagamento`}
          value={valor.forma_pagamento}
          onChange={(e) => aoMudar({ ...valor, forma_pagamento: e.target.value })}
        />
      </Campo>
      <Campo id={`${prefixo}-contratacao`} rotulo="Contratação">
        <Input
          id={`${prefixo}-contratacao`}
          type="date"
          value={valor.contratacao}
          onChange={(e) => aoMudar({ ...valor, contratacao: e.target.value })}
        />
      </Campo>

      <Campo id={`${prefixo}-vencimento`} rotulo="Vencimento da parcela" dica="Dia do mês, de 1 a 31.">
        <Input
          id={`${prefixo}-vencimento`}
          type="number"
          min={1}
          max={31}
          value={valor.vencimento_parcela ?? ""}
          onChange={(e) => aoMudar({ ...valor, vencimento_parcela: numero(e.target.value) })}
        />
      </Campo>
      <Campo id={`${prefixo}-assembleia`} rotulo="Dia da assembleia" dica="Dia do mês, de 1 a 31.">
        <Input
          id={`${prefixo}-assembleia`}
          type="number"
          min={1}
          max={31}
          value={valor.dia_assembleia ?? ""}
          onChange={(e) => aoMudar({ ...valor, dia_assembleia: numero(e.target.value) })}
        />
      </Campo>
      <Campo id={`${prefixo}-administradora`} rotulo="Administradora">
        <Input
          id={`${prefixo}-administradora`}
          disabled={travarIdentidade}
          value={valor.administradora}
          onChange={(e) => aoMudar({ ...valor, administradora: e.target.value })}
        />
      </Campo>
      <div className="flex items-end">
        <Label className="flex h-9 items-center gap-2 font-normal">
          <Switch checked={valor.ativa} onCheckedChange={(v) => aoMudar({ ...valor, ativa: v })} />
          Entra nas execuções de lance
        </Label>
      </div>
    </div>
  );
}
