import type { Perfil } from "./tipos";

// Cada serviço da plataforma é um espaço separado: uma linha no trilho, um cabeçalho com o
// que ele faz e as abas só dele, tudo sob /<id>. Serviço novo = uma entrada aqui e uma pasta
// em app/(app)/<id> com layout.tsx. Os perfis só escondem o que a pessoa não pode usar: quem
// decide é a API.

export type Aba = {
  href: string;
  rotulo: string;
  // null: todos os perfis.
  perfis: readonly Perfil[] | null;
};

export type Servico = {
  id: string;
  nome: string;
  descricao: string;
  abas: readonly Aba[];
};

export const servicos: readonly Servico[] = [
  {
    id: "canopus",
    nome: "Canopus",
    descricao: "Credenciamento de lances no Newcon",
    abas: [
      { href: "/canopus/execucoes", rotulo: "Execuções", perfis: null },
      { href: "/canopus/cotas", rotulo: "Cotas", perfis: null },
      { href: "/canopus/clientes", rotulo: "Clientes", perfis: null },
    ],
  },
  {
    id: "reservas",
    nome: "Reservas",
    descricao: "Avisos de reservas e cancelamentos no grupo do WhatsApp",
    abas: [
      // Nome e telefone de hóspedes: sem o perfil leitura.
      { href: "/reservas/lista", rotulo: "Reservas", perfis: ["admin", "operador"] },
      { href: "/reservas/whatsapp", rotulo: "WhatsApp", perfis: ["admin"] },
    ],
  },
];

export function abasVisiveis(servico: Servico, perfil: Perfil): Aba[] {
  return servico.abas.filter((a) => a.perfis === null || a.perfis.includes(perfil));
}

// Serviços com ao menos uma aba que o perfil pode abrir.
export function servicosVisiveis(perfil: Perfil): Servico[] {
  return servicos.filter((s) => abasVisiveis(s, perfil).length > 0);
}

export function servicoPorId(id: string): Servico {
  const s = servicos.find((x) => x.id === id);
  if (!s) throw new Error(`serviço desconhecido: ${id}`);
  return s;
}
