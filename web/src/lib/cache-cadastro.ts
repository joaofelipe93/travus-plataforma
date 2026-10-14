"use client";

// Resposta imediata: a tela já mostra o resultado e só depois a API confirma. Se a API
// recusar, o que estava antes volta. As cotas aparecem em duas consultas (a lista de /cotas e
// o detalhe do cliente), então as duas são corrigidas juntas.

import type { QueryClient } from "@tanstack/react-query";
import type { Cota } from "@/lib/tipos";

type ComCotas = { cotas: Cota[] };

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

// O prefixo ["cotas"] também alcança consultas que não trazem lista de cotas (a de grupos do
// filtro da tela de cotas, por exemplo): nessas, o cache fica como está.
function temCotas(dados: unknown): dados is ComCotas {
  return !!dados && Array.isArray((dados as Partial<ComCotas>).cotas);
}

/** Aplica a mudança em toda consulta cujo resultado traz uma lista de cotas. */
function mexerNasListas(queryClient: QueryClient, mudar: (cotas: Cota[]) => Cota[]) {
  const aplicar = (dados: unknown) => (temCotas(dados) ? { ...dados, cotas: mudar(dados.cotas) } : dados);
  for (const chave of ["cotas", "cliente"]) {
    queryClient.setQueriesData({ queryKey: [chave] }, aplicar);
  }
}

/** Troca uma cota em todas as consultas que a mostram. */
export function trocarCotaNoCache(queryClient: QueryClient, id: number, mudar: (c: Cota) => Cota) {
  mexerNasListas(queryClient, (cotas) => cotas.map((c) => (c.id === id ? mudar(c) : c)));
}

/** Tira uma cota de todas as consultas que a mostram. */
export function tirarCotaDoCache(queryClient: QueryClient, id: number) {
  mexerNasListas(queryClient, (cotas) => cotas.filter((c) => c.id !== id));
}
