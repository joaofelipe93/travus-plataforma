"use client";

import { useQuery } from "@tanstack/react-query";
import { api, definirCsrf } from "@/lib/api";
import type { Perfil, Sessao } from "@/lib/tipos";

export function useSessao() {
  return useQuery({
    queryKey: ["sessao"],
    queryFn: async () => {
      const sessao = await api<Sessao>("/auth/sessao");
      definirCsrf(sessao.csrf_token);
      return sessao;
    },
    staleTime: 5 * 60_000,
    retry: false,
  });
}

// Só esconde botões: quem decide a permissão é a API.
export function podeEditar(perfil: Perfil | undefined) {
  return perfil === "admin" || perfil === "operador";
}

export const nomePerfil: Record<Perfil, string> = {
  admin: "Administrador",
  operador: "Operador",
  leitura: "Leitura",
};

export const nomeModalidade: Record<string, string> = {
  segundo_fixo: "2º Fixo",
  fixo: "Fixo",
  livre: "Livre",
  limitado: "Limitado",
  fidelidade: "Fidelidade",
};

export function tagCota(c: { grupo: string; cota: string; versao: string }) {
  return `${c.grupo}-${c.cota}-${c.versao}`;
}

export function dataHora(iso: string | null | undefined) {
  return iso ? new Date(iso).toLocaleString("pt-BR", { dateStyle: "short", timeStyle: "short" }) : "—";
}
