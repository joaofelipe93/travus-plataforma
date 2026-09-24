"use client";

import { Assistente } from "@/components/assistente";
import { useSessao } from "@/lib/sessao";

// Tela inicial: o assistente que responde sobre todos os serviços. O layout (app) já carregou a
// sessão antes de chegar aqui.
export default function Inicio() {
  const sessao = useSessao();
  if (!sessao.data) return null;
  return <Assistente usuario={sessao.data.usuario} />;
}
