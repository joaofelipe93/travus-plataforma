"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { api } from "@/lib/api";
import { dataHora, useSessao } from "@/lib/sessao";
import type { ConfiguracaoCanopus, RespostaConfiguracaoCanopus, ResultadoDisparo } from "@/lib/tipos";

// Configuração do serviço Canopus: dry-run automático, pasta do Google Drive e prazos. Admin e
// operador editam; todo perfil vê. Quem valida de verdade é a API — aqui só ajudamos a
// preencher e explicamos o que cada escolha faz.

// O formulário é só texto: o que o usuário digita vira número na hora de salvar.
type Formulario = {
  automatico: boolean;
  dia: string;
  hora: string;
  minuto: string;
  avisoEmail: boolean;
  pasta: string;
  validadeMinutos: string;
  retencaoDias: string;
};

function paraFormulario(c: ConfiguracaoCanopus): Formulario {
  return {
    automatico: c.dry_run_automatico,
    dia: String(c.dry_run_dia),
    hora: String(c.dry_run_hora).padStart(2, "0"),
    minuto: String(c.dry_run_minuto).padStart(2, "0"),
    avisoEmail: c.aviso_email,
    pasta: c.drive_pasta_id ?? "",
    validadeMinutos: String(c.validade_dry_run_minutos),
    retencaoDias: String(c.retencao_screenshots_dias),
  };
}

const dias = Array.from({ length: 31 }, (_, i) => String(i + 1));
const horas = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, "0"));
const minutos = ["00", "15", "30", "45"];

const resultadoDisparo: Record<ResultadoDisparo, string> = {
  criada: "o dry-run entrou na fila",
  sem_cotas: "nenhuma cota ativa no cadastro: não havia o que conferir",
  sem_responsavel: "a configuração estava sem a pessoa que a alterou",
  atrasada: "a plataforma estava fora do ar na hora e o dry-run não foi criado atrasado",
  antes_de_ligar: "horário anterior ao agendamento atual: nada a fazer",
};

export default function PaginaConfiguracao() {
  const sessao = useSessao();
  const consulta = useQuery({
    queryKey: ["configuracao-canopus"],
    queryFn: () => api<RespostaConfiguracaoCanopus>("/configuracao/canopus"),
  });

  if (consulta.isError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{consulta.error.message}</AlertDescription>
      </Alert>
    );
  }
  if (!consulta.data) {
    return <Skeleton className="h-96 w-full max-w-3xl" />;
  }
  return (
    <Configuracao
      // Configuração salva (por mim ou por outra pessoa) remonta o formulário com os valores
      // novos, em vez de um efeito sincronizando estado.
      key={JSON.stringify(paraFormulario(consulta.data.configuracao))}
      dados={consulta.data}
      // A API manda pode_editar; o perfil da sessão só evita o pisca-pisca de um botão que
      // apareceria e sumiria.
      podeEditar={consulta.data.pode_editar && sessao.data?.usuario.perfil !== "leitura"}
    />
  );
}

function Configuracao({ dados, podeEditar }: { dados: RespostaConfiguracaoCanopus; podeEditar: boolean }) {
  const queryClient = useQueryClient();
  const c = dados.configuracao;
  const [form, setForm] = useState<Formulario>(() => paraFormulario(c));

  const salvo = JSON.stringify(paraFormulario(c));
  const mudou = JSON.stringify(form) !== salvo;
  const mudar = (parte: Partial<Formulario>) => setForm((f) => ({ ...f, ...parte }));

  const salvar = useMutation({
    mutationFn: () =>
      api<RespostaConfiguracaoCanopus>("/configuracao/canopus", {
        metodo: "PUT",
        json: {
          dry_run_automatico: form.automatico,
          dry_run_dia: Number(form.dia),
          dry_run_hora: Number(form.hora),
          dry_run_minuto: Number(form.minuto),
          aviso_email: form.avisoEmail,
          drive_pasta_id: form.pasta.trim(),
          validade_dry_run_minutos: Number(form.validadeMinutos),
          retencao_screenshots_dias: Number(form.retencaoDias),
        },
      }),
    onSuccess: (resposta) => {
      queryClient.setQueryData(["configuracao-canopus"], resposta);
      toast.success("Configuração salva");
    },
    onError: (e) => toast.error(e.message),
  });

  const numeroValido = (texto: string, min: number, max: number) => {
    const n = Number(texto);
    return Number.isInteger(n) && n >= min && n <= max;
  };
  const prazosValidos = numeroValido(form.validadeMinutos, 30, 1440) && numeroValido(form.retencaoDias, 1, 365);

  return (
    <form
      className="flex max-w-3xl flex-col gap-6"
      onSubmit={(e) => {
        e.preventDefault();
        salvar.mutate();
      }}
    >
      <p className="text-sm text-muted-foreground">
        O que o serviço Canopus faz sozinho e onde ele guarda os comprovantes. Vale para todos: quem usa a plataforma vê o
        mesmo agendamento.
      </p>

      <Secao
        titulo="Dry-run automático"
        acao={
          <Badge variant={form.automatico ? "secondary" : "outline"}>{form.automatico ? "ligado" : "desligado"}</Badge>
        }
      >
        <div className="flex flex-col gap-4">
          <Label className="flex items-center gap-3">
            <Switch
              checked={form.automatico}
              disabled={!podeEditar}
              onCheckedChange={(v) => mudar({ automatico: v })}
              aria-label="Ligar o dry-run automático"
            />
            Rodar um dry-run todo mês, sem ninguém pedir
          </Label>

          <div className="grid gap-4 sm:grid-cols-3">
            <Campo id="dia" rotulo="Dia do mês" dica="Mês sem esse dia (31 em fevereiro) usa o último dia.">
              <Select items={dias.map((d) => ({ value: d, label: d }))} value={form.dia} onValueChange={(v) => mudar({ dia: v ?? "10" })} disabled={!podeEditar}>
                <SelectTrigger id="dia">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {dias.map((d) => (
                    <SelectItem key={d} value={d}>
                      {d}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Campo>
            <Campo id="hora" rotulo="Hora" dica="Horário de Brasília.">
              <Select items={horas.map((h) => ({ value: h, label: `${h} h` }))} value={form.hora} onValueChange={(v) => mudar({ hora: v ?? "08" })} disabled={!podeEditar}>
                <SelectTrigger id="hora">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {horas.map((h) => (
                    <SelectItem key={h} value={h}>
                      {h} h
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Campo>
            <Campo id="minuto" rotulo="Minuto">
              <Select items={minutos.map((m) => ({ value: m, label: m }))} value={form.minuto} onValueChange={(v) => mudar({ minuto: v ?? "00" })} disabled={!podeEditar}>
                <SelectTrigger id="minuto">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {minutos.map((m) => (
                    <SelectItem key={m} value={m}>
                      {m}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Campo>
          </div>

          {form.automatico ? (
            <Alert>
              <AlertTitle>No dia marcado, o robô entra no Newcon sozinho</AlertTitle>
              <AlertDescription>
                Ele abre a tela de credenciamento de <strong>todas as cotas ativas</strong>, marca 2º Fixo, tira os
                screenshots e <strong>não confirma nada</strong>. O lance real continua passando pela revisão e por um
                administrador.
                {!mudou && (
                  <>
                    <br />
                    Próximo: <strong>{dataHora(c.proximo_dry_run)}</strong>.
                  </>
                )}
              </AlertDescription>
            </Alert>
          ) : (
            <p className="text-sm text-muted-foreground">
              Desligado, nada roda sem alguém pedir em Execuções → Novo dry-run. Ligando com estes valores, o primeiro
              seria em {dataHora(c.proximo_dry_run)}.
            </p>
          )}

          <Label className="flex items-center gap-3">
            <Switch
              checked={form.avisoEmail}
              disabled={!podeEditar}
              onCheckedChange={(v) => mudar({ avisoEmail: v })}
              aria-label="Avisar por e-mail quando o dry-run automático terminar"
            />
            Avisar por e-mail quando ele terminar
          </Label>
          {form.avisoEmail && !dados.email_configurado && (
            <p className="text-sm text-muted-foreground">
              O servidor ainda não tem SMTP configurado (SMTP_*): até lá, o resumo fica só no log da plataforma.
            </p>
          )}

          {c.ultimo_disparo_em && (
            <p className="text-sm text-muted-foreground">
              Última vez: {dataHora(c.ultimo_disparo_em)} —{" "}
              {c.ultimo_disparo_resultado ? resultadoDisparo[c.ultimo_disparo_resultado] : "sem registro"}.
            </p>
          )}
        </div>
      </Secao>

      <Secao titulo="Google Drive">
        <div className="flex flex-col gap-4">
          <Campo
            id="pasta"
            rotulo="Pasta dos comprovantes"
            dica='Cole o endereço da pasta ("https://drive.google.com/drive/folders/…") ou só o id. Em branco, vale a pasta definida no servidor.'
          >
            <Input
              id="pasta"
              autoComplete="off"
              spellCheck={false}
              placeholder="1AbCdEfGhIjKlMnOpQr"
              disabled={!podeEditar}
              value={form.pasta}
              onChange={(e) => mudar({ pasta: e.target.value })}
            />
          </Campo>
          <p className="text-sm text-muted-foreground">
            {c.drive_pasta_origem === "nenhuma" ? (
              "Nenhuma pasta escolhida: os comprovantes de lance real ficam no banco e não sobem para o Drive."
            ) : (
              <>
                Em uso: <span className="font-mono text-xs">{c.drive_pasta_em_vigor}</span>
                {c.drive_pasta_origem === "ambiente" && " (definida no servidor, em GOOGLE_DRIVE_PASTA_ID)"}
              </>
            )}
          </p>
        </div>
      </Secao>

      <Secao titulo="Prazos">
        <div className="grid gap-4 sm:grid-cols-2">
          <Campo
            id="validade"
            rotulo="Prazo para aprovar o lance real (minutos)"
            dica="Contado do fim do dry-run. Passado o prazo, a revisão pede um dry-run novo (de 30 a 1440)."
          >
            <Input
              id="validade"
              inputMode="numeric"
              disabled={!podeEditar}
              aria-invalid={!numeroValido(form.validadeMinutos, 30, 1440)}
              value={form.validadeMinutos}
              onChange={(e) => mudar({ validadeMinutos: e.target.value })}
            />
          </Campo>
          <Campo
            id="retencao"
            rotulo="Guardar screenshots por (dias)"
            dica="Screenshots têm dados de clientes e são apagados depois do prazo (de 1 a 365). Comprovantes em PDF não expiram."
          >
            <Input
              id="retencao"
              inputMode="numeric"
              disabled={!podeEditar}
              aria-invalid={!numeroValido(form.retencaoDias, 1, 365)}
              value={form.retencaoDias}
              onChange={(e) => mudar({ retencaoDias: e.target.value })}
            />
          </Campo>
        </div>
      </Secao>

      {podeEditar ? (
        <div className="flex flex-wrap items-center gap-3">
          <Button type="submit" disabled={!mudou || !prazosValidos || salvar.isPending}>
            {salvar.isPending ? "Salvando…" : "Salvar configuração"}
          </Button>
          {mudou && (
            <Button type="button" variant="outline" disabled={salvar.isPending} onClick={() => setForm(paraFormulario(c))}>
              Desfazer
            </Button>
          )}
          <p className="text-sm text-muted-foreground">
            {c.atualizado_por_nome
              ? `Última alteração: ${c.atualizado_por_nome}, em ${dataHora(c.atualizado_em)}.`
              : "Ainda com os valores padrão."}
          </p>
        </div>
      ) : (
        <Alert>
          <AlertTitle>Só leitura</AlertTitle>
          <AlertDescription>
            Administradores e operadores alteram esta configuração. {c.atualizado_por_nome && `Última alteração: ${c.atualizado_por_nome}, em ${dataHora(c.atualizado_em)}.`}
          </AlertDescription>
        </Alert>
      )}
    </form>
  );
}

function Secao({ titulo, acao, children }: { titulo: string; acao?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="rounded-lg border p-5">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-medium">{titulo}</h2>
        {acao}
      </div>
      {children}
    </section>
  );
}

function Campo({ id, rotulo, dica, children }: { id: string; rotulo: string; dica?: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{rotulo}</Label>
      {children}
      {dica && <p className="text-xs text-muted-foreground">{dica}</p>}
    </div>
  );
}
