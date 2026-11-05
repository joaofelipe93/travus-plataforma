"use client";

// Os formulários do cadastro, cada um num painel lateral: o cliente novo (com as cotas de
// uma vez), a edição do cliente e a cota. Os campos em si estão em campos-cadastro.tsx.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import {
  CamposCliente,
  CamposCota,
  clienteParaFormulario,
  clienteVazio,
  cotaIniciada,
  cotaParaFormulario,
  cotaPreenchida,
  cotaVazia,
} from "@/components/campos-cadastro";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Sheet, SheetBody, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { api } from "@/lib/api";
import { tagCota } from "@/lib/sessao";
import type { Cliente, ClienteFormulario, Cota, CotaFormulario, ServicoCatalogo } from "@/lib/tipos";

/** Tudo que mostra cliente ou cota fica velho depois de uma mudança no cadastro. */
export function useInvalidarCadastro() {
  const queryClient = useQueryClient();
  return () => {
    for (const chave of ["cliente", "clientes", "cotas"]) {
      queryClient.invalidateQueries({ queryKey: [chave] });
    }
  };
}

type RespostaCliente = { cliente: Cliente; cotas: Cota[] };

export function SheetNovoCliente({
  aberto,
  aoFechar,
  aoCriar,
}: {
  aberto: boolean;
  aoFechar: () => void;
  aoCriar?: (cliente: Cliente) => void;
}) {
  const invalidar = useInvalidarCadastro();
  const [cliente, setCliente] = useState<ClienteFormulario>(clienteVazio);
  const [cotas, setCotas] = useState<CotaFormulario[]>([cotaVazia()]);
  // null: ninguém mexeu nos interruptores, então o cliente entra em todos os serviços (é o que
  // a API faz quando o campo não vem). Uma lista, mesmo vazia, é escolha de quem está cadastrando.
  const [escolhaServicos, setEscolhaServicos] = useState<string[] | null>(null);

  const catalogo = useQuery({
    queryKey: ["servicos"],
    queryFn: () => api<{ servicos: ServicoCatalogo[] }>("/servicos"),
    staleTime: Infinity,
  });
  const servicos = catalogo.data?.servicos ?? [];
  const ativos = escolhaServicos ?? servicos.map((s) => s.id);

  const salvar = useMutation({
    mutationFn: (corpo: ClienteFormulario & { cotas: CotaFormulario[]; servicos?: string[] }) =>
      api<RespostaCliente>("/clientes", { metodo: "POST", json: corpo }),
    onSuccess: ({ cliente: novo, cotas: criadas }) => {
      invalidar();
      toast.success(`${novo.nome} cadastrado${criadas.length ? ` com ${criadas.length} cota(s)` : ""}`);
      setCliente(clienteVazio());
      setCotas([cotaVazia()]);
      setEscolhaServicos(null);
      aoCriar?.(novo);
      aoFechar();
    },
  });

  const preenchidas = cotas.filter(cotaPreenchida);

  return (
    <Sheet open={aberto} onOpenChange={(a) => !a && !salvar.isPending && aoFechar()}>
      <SheetContent>
        <form
          className="flex h-full flex-col"
          onSubmit={(e) => {
            e.preventDefault();
            salvar.mutate({
              ...cliente,
              cotas: preenchidas,
              ...(escolhaServicos ? { servicos: escolhaServicos } : {}),
            });
          }}
        >
          <SheetHeader>
            <SheetTitle>Novo cliente</SheetTitle>
            <SheetDescription>
              O cliente é identificado pelo nome: dois cadastros com o mesmo nome viram um só.
            </SheetDescription>
          </SheetHeader>

          <SheetBody className="flex flex-col gap-6">
            <CamposCliente valor={cliente} aoMudar={setCliente} colunas={1} />

            {servicos.length > 0 && (
              <>
                <Separator />
                <fieldset className="flex flex-col gap-3">
                  <legend className="font-medium">Serviços</legend>
                  <p className="text-sm text-muted-foreground">
                    Em que este cliente entra desde já. Dá para mudar depois, no painel dele.
                  </p>
                  {servicos.map((s) => (
                    <Label key={s.id} className="flex items-start gap-3 font-normal">
                      <Switch
                        className="mt-0.5"
                        checked={ativos.includes(s.id)}
                        onCheckedChange={(ligar) =>
                          setEscolhaServicos(ligar ? [...ativos, s.id] : ativos.filter((id) => id !== s.id))
                        }
                      />
                      <span className="min-w-0">
                        <span className="block">{s.nome}</span>
                        <span className="block text-sm text-muted-foreground">{s.descricao}</span>
                      </span>
                    </Label>
                  ))}
                </fieldset>
              </>
            )}

            <Separator />
            <div className="flex flex-col gap-4">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="font-medium">Cotas</h3>
                  <p className="text-sm text-muted-foreground">Dá para cadastrar o cliente agora e incluir as cotas depois.</p>
                </div>
                <Button type="button" variant="outline" size="sm" onClick={() => setCotas([...cotas, cotaVazia()])}>
                  Adicionar cota
                </Button>
              </div>
              {cotas.map((c, i) => (
                <div key={i} className="flex flex-col gap-3 rounded-lg border p-4">
                  <div className="flex items-center justify-between">
                    <span className="text-sm text-muted-foreground">Cota {i + 1}</span>
                    {cotas.length > 1 && (
                      <Button type="button" variant="ghost" size="sm" onClick={() => setCotas(cotas.filter((_, j) => j !== i))}>
                        Remover
                      </Button>
                    )}
                  </div>
                  <CamposCota
                    valor={c}
                    prefixo={`nova-cota-${i}`}
                    // Bloco intocado não vai no envio, então o navegador também não o cobra.
                    exigirIdentidade={cotaIniciada(c)}
                    aoMudar={(novo) => setCotas(cotas.map((a, j) => (j === i ? novo : a)))}
                  />
                </div>
              ))}
            </div>
            {salvar.isError && (
              <Alert variant="destructive">
                <AlertDescription>{salvar.error.message}</AlertDescription>
              </Alert>
            )}
          </SheetBody>

          <SheetFooter>
            <Button type="submit" disabled={salvar.isPending || cliente.nome.trim() === ""}>
              {salvar.isPending ? "Salvando…" : "Cadastrar cliente"}
            </Button>
            <Button type="button" variant="ghost" onClick={aoFechar} disabled={salvar.isPending}>
              Cancelar
            </Button>
            <span className="ml-auto text-sm text-muted-foreground">
              {preenchidas.length === 0 ? "Nenhuma cota preenchida" : `${preenchidas.length} cota(s)`}
            </span>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}

export function SheetEditarCliente({ cliente, aberto, aoFechar }: { cliente: Cliente; aberto: boolean; aoFechar: () => void }) {
  const invalidar = useInvalidarCadastro();
  const [valor, setValor] = useState<ClienteFormulario>(() => clienteParaFormulario(cliente));

  const salvar = useMutation({
    mutationFn: (v: ClienteFormulario) => api<RespostaCliente>(`/clientes/${cliente.id}`, { metodo: "PATCH", json: v }),
    onSuccess: () => {
      invalidar();
      toast.success("Cliente atualizado");
      aoFechar();
    },
  });

  return (
    <Sheet open={aberto} onOpenChange={(a) => !a && !salvar.isPending && aoFechar()}>
      <SheetContent className="max-w-md">
        <form
          className="flex h-full flex-col"
          onSubmit={(e) => {
            e.preventDefault();
            salvar.mutate(valor);
          }}
        >
          <SheetHeader>
            <SheetTitle>Editar cliente</SheetTitle>
            <SheetDescription>Telefone, e-mail ou CPF/CNPJ em branco apagam o que estava guardado.</SheetDescription>
          </SheetHeader>
          <SheetBody className="flex flex-col gap-6">
            <CamposCliente valor={valor} aoMudar={setValor} prefixo="editar-cliente" colunas={1} />
            {salvar.isError && (
              <Alert variant="destructive">
                <AlertDescription>{salvar.error.message}</AlertDescription>
              </Alert>
            )}
          </SheetBody>
          <SheetFooter>
            <Button type="submit" disabled={salvar.isPending || valor.nome.trim() === ""}>
              {salvar.isPending ? "Salvando…" : "Salvar"}
            </Button>
            <Button type="button" variant="ghost" onClick={aoFechar} disabled={salvar.isPending}>
              Cancelar
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}

export function SheetCota({
  clienteId,
  clienteNome,
  cota,
  aberto,
  aoFechar,
}: {
  clienteId: number;
  clienteNome: string;
  /** null: cota nova. */
  cota: Cota | null;
  aberto: boolean;
  aoFechar: () => void;
}) {
  const invalidar = useInvalidarCadastro();
  const [valor, setValor] = useState<CotaFormulario>(() => (cota ? cotaParaFormulario(cota) : cotaVazia()));

  const salvar = useMutation({
    mutationFn: (v: CotaFormulario) =>
      cota
        ? api<{ cota: Cota }>(`/cotas/${cota.id}`, { metodo: "PUT", json: { ...v, cliente_id: clienteId } })
        : api<{ cota: Cota }>("/cotas", { metodo: "POST", json: { ...v, cliente_id: clienteId } }),
    onSuccess: ({ cota: salva }) => {
      invalidar();
      toast.success(`Cota ${tagCota(salva)} ${cota ? "atualizada" : "cadastrada"}`);
      aoFechar();
    },
  });

  return (
    <Sheet open={aberto} onOpenChange={(a) => !a && !salvar.isPending && aoFechar()}>
      <SheetContent>
        <form
          className="flex h-full flex-col"
          onSubmit={(e) => {
            e.preventDefault();
            salvar.mutate(valor);
          }}
        >
          <SheetHeader>
            <SheetTitle>{cota ? `Cota ${tagCota(cota)}` : "Nova cota"}</SheetTitle>
            <SheetDescription>{clienteNome}</SheetDescription>
          </SheetHeader>
          <SheetBody className="flex flex-col gap-6">
            <CamposCota valor={valor} aoMudar={setValor} prefixo={cota ? `cota-${cota.id}` : "cota-nova"} colunas={2} />
            {salvar.isError && (
              <Alert variant="destructive">
                <AlertDescription>{salvar.error.message}</AlertDescription>
              </Alert>
            )}
          </SheetBody>
          <SheetFooter>
            <Button type="submit" disabled={salvar.isPending || valor.grupo.trim() === "" || valor.cota.trim() === ""}>
              {salvar.isPending ? "Salvando…" : "Salvar"}
            </Button>
            <Button type="button" variant="ghost" onClick={aoFechar} disabled={salvar.isPending}>
              Cancelar
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}
