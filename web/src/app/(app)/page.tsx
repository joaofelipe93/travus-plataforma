"use client";

import { Assistente } from "@/components/assistente";
import { useSessao } from "@/lib/sessao";

function saudacao(nome: string) {
  const hora = new Date().getHours();
  const periodo = hora < 12 ? "Bom dia" : hora < 18 ? "Boa tarde" : "Boa noite";
  return `${periodo}, ${nome.split(" ")[0]}`;
}

const hoje = new Intl.DateTimeFormat("pt-BR", { weekday: "long", day: "numeric", month: "long", timeZone: "America/Sao_Paulo" });

// Início: a página depois do login, montada em blocos. O assistente é o primeiro; os próximos
// (serviços favoritos, atalhos, o que cada pessoa escolher para a sua home) entram embaixo dele,
// dentro do mesmo <div> de blocos. O layout (app) já carregou a sessão antes de chegar aqui.
export default function Inicio() {
  const sessao = useSessao();
  if (!sessao.data) return null;

  return (
    <main className="min-w-0 flex-1 overflow-y-auto">
      <div className="mx-auto flex w-full max-w-5xl flex-col gap-6 px-8 py-8">
        <header>
          <h1 className="text-2xl font-semibold">{saudacao(sessao.data.usuario.nome)}</h1>
          <p className="mt-1 text-sm text-muted-foreground first-letter:uppercase">{hoje.format(new Date())}</p>
        </header>

        <Assistente />
      </div>
    </main>
  );
}
