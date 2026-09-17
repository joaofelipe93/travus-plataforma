"use client";

// Resposta imediata: a tela já mostra o resultado e só depois a API confirma. Se a API
// recusar, o que estava antes volta. As cotas aparecem em duas consultas (a lista de /cotas e
// o detalhe do cliente), então as duas são corrigidas juntas.

import type { QueryClient } from "@tanstack/react-query";
import type { Cota } from "@/lib/tipos";

type ListaCotas = { cotas: Cota[] };
type DetalheCliente = { cotas: Cota[] };

/** Guarda o estado atual das consultas de cota para poder desfazer. */
export type CacheCotas = [readonly unknown[], unknown][];

export async function pausarConsultasDeCota(queryClient: QueryClient): Promise<CacheCotas> {
  await Promise.all([
    queryClient.cancelQueries({ queryKey: ["cotas"] }),
    queryClient.cancelQueries({ queryKey: ["cliente"] }),
  ]);
  return [...queryClient.getQueriesData({ queryKey: ["cotas"] }), ...queryClient.getQueriesData({ queryKey: ["cliente"] })];
}

export function desfazer(queryClient: QueryClient, anterior: CacheCotas | undefined) {
  for (const [chave, dados] of anterior ?? []) {
    queryClient.setQueryData(chave, dados);
  }
}

/** Troca uma cota em todas as consultas que a mostram. */
export function trocarCotaNoCache(queryClient: QueryClient, id: number, mudar: (c: Cota) => Cota) {
  const trocar = (cotas: Cota[]) => cotas.map((c) => (c.id === id ? mudar(c) : c));
  queryClient.setQueriesData<ListaCotas>({ queryKey: ["cotas"] }, (d) => (d ? { ...d, cotas: trocar(d.cotas) } : d));
  queryClient.setQueriesData<DetalheCliente>({ queryKey: ["cliente"] }, (d) => (d ? { ...d, cotas: trocar(d.cotas) } : d));
}

/** Tira uma cota de todas as consultas que a mostram. */
export function tirarCotaDoCache(queryClient: QueryClient, id: number) {
  const tirar = (cotas: Cota[]) => cotas.filter((c) => c.id !== id);
  queryClient.setQueriesData<ListaCotas>({ queryKey: ["cotas"] }, (d) => (d ? { ...d, cotas: tirar(d.cotas) } : d));
  queryClient.setQueriesData<DetalheCliente>({ queryKey: ["cliente"] }, (d) => (d ? { ...d, cotas: tirar(d.cotas) } : d));
}
