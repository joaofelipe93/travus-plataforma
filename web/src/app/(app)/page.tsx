"use client";

import Link from "next/link";
import { Assistente } from "@/components/assistente";
import { Colunas, Pagina, Secao } from "@/components/pagina";
import { abasVisiveis, servicosVisiveis } from "@/lib/servicos";
import { useSessao } from "@/lib/sessao";
import type { Perfil } from "@/lib/tipos";

function saudacao(nome: string) {
  const hora = new Date().getHours();
  const periodo = hora < 12 ? "Bom dia" : hora < 18 ? "Boa tarde" : "Boa noite";
  return `${periodo}, ${nome.split(" ")[0]}`;
}

const hoje = new Intl.DateTimeFormat("pt-BR", { weekday: "long", day: "numeric", month: "long", timeZone: "America/Sao_Paulo" });

// Início: a página depois do login. O assistente ocupa a coluna larga e as telas de cada
// serviço ficam ao lado, para a pessoa já sair daqui para onde vai trabalhar. Os próximos
// blocos (serviços favoritos, o que cada pessoa escolher) entram embaixo do assistente.
export default function Inicio() {
  const sessao = useSessao();
  if (!sessao.data) return null;
  const { usuario } = sessao.data;

  return (
    <Pagina titulo={saudacao(usuario.nome)} descricao={<span className="first-letter:uppercase">{hoje.format(new Date())}</span>}>
      <Colunas lateral={<Atalhos perfil={usuario.perfil} />}>
        <Assistente />
      </Colunas>
    </Pagina>
  );
}

function Atalhos({ perfil }: { perfil: Perfil }) {
  return (
    <Secao titulo="Ir para">
      <ul className="flex flex-col divide-y">
        {servicosVisiveis(perfil).map((s) => (
          <li key={s.id} className="py-3 first:pt-0">
            <Link
              href={abasVisiveis(s, perfil)[0].href}
              className="font-heading text-[0.95rem] outline-none hover:underline focus-visible:underline"
            >
              {s.nome}
            </Link>
            <p className="mt-0.5 text-sm text-muted-foreground">{s.descricao}</p>
            <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1">
              {abasVisiveis(s, perfil).map((a) => (
                <li key={a.href}>
                  <Link
                    href={a.href}
                    className="text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:text-foreground focus-visible:underline"
                  >
                    {a.rotulo}
                  </Link>
                </li>
              ))}
            </ul>
          </li>
        ))}
        <li className="py-3">
          <Link
            href="/perfil"
            className="font-heading text-[0.95rem] outline-none hover:underline focus-visible:underline"
          >
            Meu perfil
          </Link>
          <p className="mt-0.5 text-sm text-muted-foreground">Seus dados de contato e os acessos que você cadastrou</p>
        </li>
      </ul>
    </Secao>
  );
}
