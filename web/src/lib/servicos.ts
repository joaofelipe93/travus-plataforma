import type { Perfil } from "./tipos";

// Cada serviço da plataforma é um espaço separado: uma linha no trilho, um cabeçalho com o
// que ele faz e as abas só dele, tudo sob /<id>. Serviço novo = uma entrada aqui e uma pasta
// em app/(app)/<id> com layout.tsx. Os perfis só escondem o que a pessoa não pode usar: quem
// decide é a API.
//
// **Todos os serviços têm a mesma forma**, nesta ordem (decisão do usuário em 20/10/2026, que
// achou os serviços desparelhos): **Painel** (como está o serviço agora e o que ele vai fazer
// sozinho), as telas de operação dele, **Configurações** (o que a plataforma faz sem ninguém
// pedir) e **Conexões** (a conta de terceiros que o serviço usa: o WhatsApp dos boletos, o
// WhatsApp do notificador, o Google Drive dos comprovantes). O Painel é a primeira aba, e é
// nele que o trilho leva. Serviço novo nasce com essas três partes.
//
// O cadastro de **Clientes** não está aqui de propósito: ele não é um serviço, é o cadastro que
// todos usam (/clientes, com um item próprio no trilho). Os serviços que agem sobre um cliente
// — hoje lances e boletos — são ligados cliente a cliente pela API (`internal/servicos`), e os
// ids daqui são os mesmos de lá.

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
    id: "lances",
    nome: "Lances",
    descricao: "Credenciamento de lances no Newcon (Canopus Consórcios)",
    abas: [
      { href: "/lances/painel", rotulo: "Painel", perfis: null },
      { href: "/lances/execucoes", rotulo: "Execuções", perfis: null },
      { href: "/lances/cotas", rotulo: "Cotas", perfis: null },
      // Todo perfil vê o agendamento e os prazos; admin e operador editam (a API decide).
      { href: "/lances/configuracao", rotulo: "Configurações", perfis: null },
      // Lances não manda WhatsApp: a conta de terceiros dele é o Google Drive dos
      // comprovantes, e a situação do token é de admin (GET /integracoes/google-drive).
      { href: "/lances/conexoes", rotulo: "Conexões", perfis: ["admin"] },
    ],
  },
  {
    id: "boletos",
    nome: "Boletos",
    descricao: "Boletos do mês no WhatsApp dos clientes (Boleto Fácil da Canopus)",
    abas: [
      { href: "/boletos/painel", rotulo: "Painel", perfis: null },
      { href: "/boletos/rodadas", rotulo: "Rodadas", perfis: null },
      { href: "/boletos/configuracao", rotulo: "Configurações", perfis: null },
      // O QR dá acesso à conta do WhatsApp que fala com os clientes: só admin.
      { href: "/boletos/conexoes", rotulo: "Conexões", perfis: ["admin"] },
    ],
  },
  {
    id: "atendimento",
    nome: "Atendimento",
    descricao: "Bots que atendem no WhatsApp e coletam o que interessa",
    abas: [
      { href: "/atendimento/painel", rotulo: "Painel", perfis: null },
      { href: "/atendimento/bots", rotulo: "Bots", perfis: null },
      // Uma conversa traz o telefone de quem escreveu e o que a pessoa disse: a mesma régua da
      // tela de Reservas, que também mostra dado de gente de verdade.
      { href: "/atendimento/conversas", rotulo: "Conversas", perfis: ["admin", "operador"] },
      { href: "/atendimento/configuracao", rotulo: "Configurações", perfis: null },
      // O QR dá acesso à conta de WhatsApp que fala com clientes: só admin.
      { href: "/atendimento/conexoes", rotulo: "Conexões", perfis: ["admin"] },
    ],
  },
  {
    id: "reservas",
    nome: "Reservas",
    descricao: "Avisos de reservas e cancelamentos no grupo do WhatsApp",
    abas: [
      // Nome e telefone de hóspedes: sem o perfil leitura.
      { href: "/reservas/painel", rotulo: "Painel", perfis: ["admin", "operador"] },
      { href: "/reservas/lista", rotulo: "Reservas", perfis: ["admin", "operador"] },
      // Escolher o grupo que recebe os avisos é só do admin (PUT /integracoes/whatsapp/grupo).
      { href: "/reservas/configuracao", rotulo: "Configurações", perfis: ["admin"] },
      { href: "/reservas/conexoes", rotulo: "Conexões", perfis: ["admin"] },
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
