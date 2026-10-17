"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Faixa, Topo } from "@/components/pagina";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { abasVisiveis, servicoPorId } from "@/lib/servicos";
import { useSessao } from "@/lib/sessao";
import { cn } from "@/lib/utils";

// Cabeçalho e abas de um serviço (app/(app)/<id>/layout.tsx). Cada serviço mostra só as
// próprias telas: nada de um serviço aparece dentro de outro. O topo fica fixo ao rolar —
// de qualquer ponto da tela se vê em que serviço e em que aba a pessoa está.
export function EspacoServico({ id, children }: { id: string; children: React.ReactNode }) {
  const servico = servicoPorId(id);
  const caminho = usePathname();
  // O layout (app) só renderiza os filhos com a sessão carregada.
  const perfil = useSessao().data!.usuario.perfil;
  const abas = abasVisiveis(servico, perfil);
  // A aba atual é a de endereço mais longo que casa: com /x e /x/y, /x/y é a certa.
  const abaAtual = [...servico.abas]
    .sort((a, b) => b.href.length - a.href.length)
    .find((a) => caminho === a.href || caminho.startsWith(`${a.href}/`));
  const semPermissao = abas.length === 0 || (abaAtual !== undefined && !abas.includes(abaAtual));

  return (
    <main className="min-w-0 flex-1 pb-20">
      <Topo
        titulo={servico.nome}
        descricao={servico.descricao}
        abas={
          <nav aria-label={`Telas de ${servico.nome}`} className="flex gap-7 overflow-x-auto">
            {abas.map((a) => {
              const ativa = a === abaAtual;
              return (
                <Link
                  key={a.href}
                  href={a.href}
                  aria-current={ativa ? "page" : undefined}
                  className={cn(
                    "shrink-0 border-b-2 border-transparent pb-3 text-sm whitespace-nowrap text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:text-foreground focus-visible:underline",
                    ativa && "border-foreground text-foreground",
                  )}
                >
                  {a.rotulo}
                </Link>
              );
            })}
          </nav>
        }
      />
      <Faixa className="pt-8">
        {semPermissao ? (
          <Alert className="max-w-xl">
            <AlertTitle>Sem acesso a esta tela</AlertTitle>
            <AlertDescription>
              Seu perfil não permite abrir esta parte de {servico.nome}. Peça a um administrador.
            </AlertDescription>
          </Alert>
        ) : (
          children
        )}
      </Faixa>
    </main>
  );
}
