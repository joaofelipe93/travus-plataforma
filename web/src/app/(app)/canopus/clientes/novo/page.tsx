"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { CamposCliente, CamposCota, clienteVazio, cotaPreenchida, cotaVazia } from "@/components/campos-cadastro";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { api } from "@/lib/api";
import type { Cliente, ClienteFormulario, Cota, CotaFormulario } from "@/lib/tipos";

export default function PaginaNovoCliente() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [cliente, setCliente] = useState<ClienteFormulario>(clienteVazio);
  const [cotas, setCotas] = useState<CotaFormulario[]>([cotaVazia()]);

  const salvar = useMutation({
    mutationFn: (corpo: ClienteFormulario & { cotas: CotaFormulario[] }) =>
      api<{ cliente: Cliente; cotas: Cota[] }>("/clientes", { metodo: "POST", json: corpo }),
    onSuccess: ({ cliente: novo, cotas: criadas }) => {
      queryClient.invalidateQueries({ queryKey: ["clientes"] });
      queryClient.invalidateQueries({ queryKey: ["cotas"] });
      toast.success(`${novo.nome} cadastrado com ${criadas.length} cota(s)`);
      router.push(`/canopus/clientes/${novo.id}`);
    },
  });

  // Cota em branco é só uma linha que a pessoa não usou: não vai para a API.
  const paraEnviar = cotas.filter(cotaPreenchida);

  return (
    <form
      className="flex flex-col gap-6"
      onSubmit={(e) => {
        e.preventDefault();
        salvar.mutate({ ...cliente, cotas: paraEnviar });
      }}
    >
      <Link href="/canopus/clientes" className="text-sm text-muted-foreground hover:text-foreground">
        ← Clientes
      </Link>

      <div>
        <h1 className="text-xl font-semibold tracking-tight">Novo cliente</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          O cliente é identificado pelo nome (a administradora não dá CPF): dois cadastros com o mesmo nome viram um só.
        </p>
      </div>

      <CamposCliente valor={cliente} aoMudar={setCliente} />

      <Separator />

      <div className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="font-medium">Cotas</h2>
            <p className="text-sm text-muted-foreground">Dá para cadastrar o cliente agora e incluir as cotas depois.</p>
          </div>
          <Button type="button" variant="outline" onClick={() => setCotas([...cotas, cotaVazia()])}>
            Adicionar cota
          </Button>
        </div>

        {cotas.map((c, i) => (
          <div key={i} className="flex flex-col gap-3 rounded-lg border p-4">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium text-muted-foreground">Cota {i + 1}</span>
              {cotas.length > 1 && (
                <Button type="button" variant="ghost" size="sm" onClick={() => setCotas(cotas.filter((_, j) => j !== i))}>
                  Remover
                </Button>
              )}
            </div>
            <CamposCota
              valor={c}
              prefixo={`cota-${i}`}
              aoMudar={(novo) => setCotas(cotas.map((antigo, j) => (j === i ? novo : antigo)))}
            />
          </div>
        ))}
      </div>

      {salvar.isError && (
        <Alert variant="destructive">
          <AlertDescription>{salvar.error.message}</AlertDescription>
        </Alert>
      )}

      <div className="flex items-center gap-3">
        <Button type="submit" disabled={salvar.isPending || cliente.nome.trim() === ""}>
          {salvar.isPending ? "Salvando…" : "Cadastrar cliente"}
        </Button>
        <Button type="button" variant="ghost" onClick={() => router.push("/canopus/clientes")}>
          Cancelar
        </Button>
        <span className="text-sm text-muted-foreground">
          {paraEnviar.length === 0 ? "Nenhuma cota preenchida" : `${paraEnviar.length} cota(s) a cadastrar`}
        </span>
      </div>
    </form>
  );
}
