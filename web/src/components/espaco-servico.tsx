"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { abasVisiveis, servicoPorId } from "@/lib/servicos";
import { useSessao } from "@/lib/sessao";
import { cn } from "@/lib/utils";

// Cabeçalho e abas de um serviço (app/(app)/<id>/layout.tsx). Cada serviço mostra só as
// próprias telas: nada de um serviço aparece dentro de outro.
export function EspacoServico({ id, children }: { id: string; children: React.ReactNode }) {
  const servico = servicoPorId(id);
  const caminho = usePathname();
  // O layout (app) só renderiza os filhos com a sessão carregada.
  const perfil = useSessao().data!.usuario.perfil;
  const abas = abasVisiveis(servico, perfil);
  const abaAtual = servico.abas.find((a) => caminho === a.href || caminho.startsWith(`${a.href}/`));
  const semPermissao = abas.length === 0 || (abaAtual !== undefined && !abas.includes(abaAtual));

  return (
    <div className="flex min-w-0 flex-1 flex-col">
      <header className="border-b px-8 pt-6">
        <h1 className="font-heading text-2xl font-semibold">{servico.nome}</h1>
        <p className="mt-0.5 text-sm text-muted-foreground">{servico.descricao}</p>
        <nav aria-label={`Telas de ${servico.nome}`} className="-mb-px mt-5 flex gap-6">
          {abas.map((a) => {
            const ativa = a === abaAtual;
            return (
              <Link
                key={a.href}
                href={a.href}
                aria-current={ativa ? "page" : undefined}
                className={cn(
                  "border-b-2 border-transparent pb-3 text-sm text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:text-foreground focus-visible:underline",
                  ativa && "border-foreground text-foreground",
                )}
              >
                {a.rotulo}
              </Link>
            );
          })}
        </nav>
      </header>
      <main className="w-full max-w-6xl flex-1 px-8 py-6">
        {semPermissao ? (
          <Alert className="max-w-xl">
            <AlertTitle>Sem acesso a esta tela</AlertTitle>
            <AlertDescription>Seu perfil não permite abrir esta parte de {servico.nome}. Peça a um administrador.</AlertDescription>
          </Alert>
        ) : (
          children
        )}
      </main>
    </div>
  );
}
