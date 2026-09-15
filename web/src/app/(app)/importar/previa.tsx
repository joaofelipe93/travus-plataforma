"use client";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { dataHora, tagCota } from "@/lib/sessao";
import type { Importacao, Mudanca, Totais } from "@/lib/tipos";
import { cn } from "@/lib/utils";

export function resumirTotais(t: Totais) {
  const partes = [
    `${t.cotas_novas} cota(s) nova(s)`,
    `${t.cotas_alteradas} alterada(s)`,
    `${t.clientes_novos} cliente(s) novo(s)`,
  ];
  if (t.clientes_alterados) partes.push(`${t.clientes_alterados} com contato atualizado`);
  return partes.join(", ");
}

const nomesCampos: Record<string, string> = {
  cliente: "Cliente",
  tipo_consorcio: "Tipo de consórcio",
  telefone: "Telefone",
  email: "E-mail",
};

function nomeCampo(campo: string) {
  return nomesCampos[campo] ?? campo.replace(/^planilha: /, "Planilha: ");
}

type Props = {
  importacao: Importacao;
  erroAoAplicar?: string;
  ocupado: boolean;
  onAplicar: () => void;
  onDescartar: () => void;
};

export function VisaoPrevia({ importacao, erroAoAplicar, ocupado, onAplicar, onDescartar }: Props) {
  const p = importacao.previa;
  const t = p.totais;
  const temErros = t.erros > 0;
  const nadaAMudar = !temErros && t.cotas_novas + t.cotas_alteradas + t.clientes_novos + t.clientes_alterados === 0;

  return (
    <section className="flex flex-col gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="font-semibold">Prévia de {importacao.arquivo_nome}</h2>
          <p className="text-sm text-muted-foreground">
            {t.linhas} linha(s) lida(s), {t.clientes} cliente(s) na planilha. Gerada em {dataHora(importacao.criada_em)}.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={onDescartar} disabled={ocupado}>
            Descartar
          </Button>
          <Button onClick={onAplicar} disabled={ocupado || temErros || nadaAMudar}>
            Aplicar importação
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
        <Numero rotulo="Cotas novas" valor={t.cotas_novas} />
        <Numero rotulo="Cotas alteradas" valor={t.cotas_alteradas} />
        <Numero rotulo="Sem mudança" valor={t.cotas_sem_mudanca} />
        <Numero rotulo="Clientes novos" valor={t.clientes_novos} />
        <Numero rotulo="Fora da planilha" valor={t.fora_da_planilha} tom={t.fora_da_planilha > 0 ? "atencao" : undefined} />
        <Numero rotulo="Erros" valor={t.erros} tom={temErros ? "erro" : undefined} />
      </div>

      {erroAoAplicar && (
        <Alert variant="destructive">
          <AlertTitle>A importação não foi aplicada</AlertTitle>
          <AlertDescription>{erroAoAplicar}</AlertDescription>
        </Alert>
      )}
      {temErros && (
        <Alert variant="destructive">
          <AlertTitle>A planilha tem {t.erros} erro(s)</AlertTitle>
          <AlertDescription>Corrija a planilha e envie de novo. Nada foi gravado.</AlertDescription>
        </Alert>
      )}
      {nadaAMudar && (
        <Alert>
          <AlertDescription>O cadastro já está igual à planilha: não há o que aplicar.</AlertDescription>
        </Alert>
      )}

      <Secao titulo="Erros" quantidade={p.erros.length}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-20">Linha</TableHead>
              <TableHead>Problema</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {p.erros.map((e, i) => (
              <TableRow key={i}>
                <TableCell className="tabular-nums">{e.linha}</TableCell>
                <TableCell>{e.mensagem}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Secao>

      <Secao titulo="Cotas novas" quantidade={p.cotas_novas.length}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-20">Linha</TableHead>
              <TableHead>Cliente</TableHead>
              <TableHead>Cota</TableHead>
              <TableHead>Tipo</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {p.cotas_novas.map((c) => (
              <TableRow key={tagCota(c)}>
                <TableCell className="tabular-nums">{c.linha}</TableCell>
                <TableCell>{c.cliente}</TableCell>
                <TableCell className="tabular-nums">{tagCota(c)}</TableCell>
                <TableCell>{c.tipo_consorcio || "—"}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Secao>

      <Secao titulo="Cotas alteradas" quantidade={p.cotas_alteradas.length}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-20">Linha</TableHead>
              <TableHead>Cota</TableHead>
              <TableHead>O que muda</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {p.cotas_alteradas.map((c) => (
              <TableRow key={tagCota(c)}>
                <TableCell className="tabular-nums">{c.linha}</TableCell>
                <TableCell className="tabular-nums">{tagCota(c)}</TableCell>
                <TableCell>
                  <ListaMudancas mudancas={c.mudancas} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Secao>

      <Secao titulo="Clientes novos" quantidade={p.clientes_novos.length}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Nome</TableHead>
              <TableHead>Telefone</TableHead>
              <TableHead>E-mail</TableHead>
              <TableHead className="text-right">Cotas</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {p.clientes_novos.map((c) => (
              <TableRow key={c.nome}>
                <TableCell>{c.nome}</TableCell>
                <TableCell>{c.telefone || "—"}</TableCell>
                <TableCell>{c.email || "—"}</TableCell>
                <TableCell className="text-right tabular-nums">{c.cotas}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Secao>

      <Secao titulo="Clientes com contato atualizado" quantidade={p.clientes_alterados.length}>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Nome</TableHead>
              <TableHead>O que muda</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {p.clientes_alterados.map((c) => (
              <TableRow key={c.nome}>
                <TableCell>{c.nome}</TableCell>
                <TableCell>
                  <ListaMudancas mudancas={c.mudancas} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Secao>

      <Secao
        titulo="No cadastro, mas fora da planilha"
        quantidade={p.fora_da_planilha.length}
        descricao="Não serão desativadas. Se alguma não deve mais receber lance, desative na tela de Cotas."
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Cota</TableHead>
              <TableHead>Cliente</TableHead>
              <TableHead>Situação</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {p.fora_da_planilha.map((c) => (
              <TableRow key={tagCota(c)}>
                <TableCell className="tabular-nums">{tagCota(c)}</TableCell>
                <TableCell>{c.cliente}</TableCell>
                <TableCell>{c.ativa ? <Badge variant="secondary">Ativa</Badge> : <Badge variant="outline">Inativa</Badge>}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Secao>
    </section>
  );
}

function Numero({ rotulo, valor, tom }: { rotulo: string; valor: number; tom?: "atencao" | "erro" }) {
  return (
    <div
      className={cn(
        "rounded-lg border p-3",
        tom === "atencao" && "border-amber-300 bg-amber-50 dark:border-amber-900 dark:bg-amber-950/40",
        tom === "erro" && "border-destructive/40 bg-destructive/5",
      )}
    >
      <div className="text-2xl font-semibold tabular-nums">{valor}</div>
      <div className="text-xs text-muted-foreground">{rotulo}</div>
    </div>
  );
}

function Secao({ titulo, quantidade, descricao, children }: { titulo: string; quantidade: number; descricao?: string; children: React.ReactNode }) {
  if (quantidade === 0) return null;
  return (
    <div className="flex flex-col gap-2">
      <div>
        <h3 className="text-sm font-medium">
          {titulo} <span className="text-muted-foreground">({quantidade})</span>
        </h3>
        {descricao && <p className="text-sm text-muted-foreground">{descricao}</p>}
      </div>
      <div className="rounded-lg border">{children}</div>
    </div>
  );
}

function ListaMudancas({ mudancas }: { mudancas?: Mudanca[] }) {
  return (
    <ul className="flex flex-col gap-0.5">
      {mudancas?.map((m) => (
        <li key={m.campo}>
          <span className="text-muted-foreground">{nomeCampo(m.campo)}:</span> {m.antes || "(vazio)"} → {m.depois || "(vazio)"}
        </li>
      ))}
    </ul>
  );
}
