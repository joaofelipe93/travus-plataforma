"use client";

import { useMutation } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ChevronsUpDownIcon } from "lucide-react";
import { Marca } from "@/components/marca";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { api, definirCsrf } from "@/lib/api";
import { abasVisiveis, servicosVisiveis } from "@/lib/servicos";
import { nomePerfil } from "@/lib/sessao";
import type { Usuario } from "@/lib/tipos";
import { cn } from "@/lib/utils";

// Embutidos no build (web/Dockerfile, a partir de version.txt e do commit).
const versaoDoApp = process.env.NEXT_PUBLIC_VERSAO ? `v${process.env.NEXT_PUBLIC_VERSAO}` : "dev";
const commitDoApp = (process.env.NEXT_PUBLIC_COMMIT ?? "desconhecido").slice(0, 7);

// Trilho à esquerda: um serviço por linha. O serviço aberto é o único ponto em ouro da tela
// (a barra à esquerda do nome); o resto do trilho fica quieto.
export function Trilho({ usuario }: { usuario: Usuario }) {
  const caminho = usePathname();
  const sair = useMutation({
    mutationFn: () => api("/auth/logout", { metodo: "POST" }),
    // Mesmo se a sessão já tinha expirado, volta para o login com a memória limpa.
    onSettled: () => {
      definirCsrf(null);
      // Recarga completa de propósito: descarta o cache das consultas do usuário anterior.
      // eslint-disable-next-line @next/next/no-location-assign-relative-destination
      window.location.assign("/login");
    },
  });

  return (
    <aside className="sticky top-0 flex h-svh w-56 shrink-0 flex-col border-r bg-sidebar">
      <Link href="/" className="flex h-16 items-center px-5 outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <Marca />
      </Link>

      <nav aria-label="Serviços" className="flex flex-col gap-0.5 px-2 pt-2">
        {servicosVisiveis(usuario.perfil).map((s) => {
          const ativo = caminho === `/${s.id}` || caminho.startsWith(`/${s.id}/`);
          return (
            <Link
              key={s.id}
              href={abasVisiveis(s, usuario.perfil)[0].href}
              aria-current={ativo ? "page" : undefined}
              className={cn(
                "relative rounded-md py-2 pr-3 pl-4 font-heading text-[0.95rem] text-muted-foreground transition-colors outline-none hover:bg-sidebar-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring",
                ativo && "bg-sidebar-accent text-foreground",
              )}
            >
              {ativo && <span aria-hidden className="absolute inset-y-1.5 left-0 w-[3px] rounded-full bg-ouro" />}
              {s.nome}
            </Link>
          );
        })}
      </nav>

      <div className="mt-auto flex flex-col gap-0.5 border-t px-2 py-3">
        <Link
          href="/status"
          aria-current={caminho === "/status" ? "page" : undefined}
          className={cn(
            "rounded-md px-4 py-2 text-sm text-muted-foreground transition-colors outline-none hover:bg-sidebar-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring",
            caminho === "/status" && "bg-sidebar-accent text-foreground",
          )}
        >
          Status da plataforma
        </Link>
        <DropdownMenu>
          <DropdownMenuTrigger className="flex w-full items-center gap-2 rounded-md px-4 py-2 text-left outline-none hover:bg-sidebar-accent focus-visible:ring-2 focus-visible:ring-ring">
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm">{usuario.nome}</span>
              <span className="block text-xs text-muted-foreground">{nomePerfil[usuario.perfil]}</span>
            </span>
            <ChevronsUpDownIcon className="size-4 text-muted-foreground" />
          </DropdownMenuTrigger>
          <DropdownMenuContent side="top" align="start" className="w-52">
            <DropdownMenuGroup>
              <DropdownMenuLabel className="truncate">{usuario.email}</DropdownMenuLabel>
              <DropdownMenuLabel className="tabular-nums">
                Versão {versaoDoApp} <span className="text-muted-foreground">({commitDoApp})</span>
              </DropdownMenuLabel>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem render={<Link href="/perfil" />}>Meu perfil</DropdownMenuItem>
            <DropdownMenuItem onClick={() => sair.mutate()} disabled={sair.isPending}>
              Sair
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </aside>
  );
}
