"use client";

import { useMutation } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ChevronDownIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { nomePerfil } from "@/lib/sessao";
import type { Perfil, Usuario } from "@/lib/tipos";
import { cn } from "@/lib/utils";

const links = [
  { href: "/execucoes", rotulo: "Execuções", perfis: null },
  { href: "/cotas", rotulo: "Cotas", perfis: null },
  { href: "/clientes", rotulo: "Clientes", perfis: null },
  { href: "/importar", rotulo: "Importar planilha", perfis: ["admin", "operador"] },
  { href: "/whatsapp", rotulo: "WhatsApp", perfis: ["admin"] },
] as const satisfies readonly { href: string; rotulo: string; perfis: readonly Perfil[] | null }[];

export function Cabecalho({ usuario }: { usuario: Usuario }) {
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
    <header className="border-b bg-background">
      <div className="mx-auto flex min-h-14 max-w-6xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-2">
        <Link href="/cotas" className="font-semibold tracking-tight">
          Travus
        </Link>
        <nav className="flex flex-wrap items-center gap-1 text-sm">
          {links
            .filter((l) => l.perfis === null || (l.perfis as readonly Perfil[]).includes(usuario.perfil))
            .map((l) => (
              <Link
                key={l.href}
                href={l.href}
                className={cn(
                  "rounded-md px-3 py-1.5 text-muted-foreground transition-colors hover:text-foreground",
                  caminho.startsWith(l.href) && "bg-muted font-medium text-foreground",
                )}
              >
                {l.rotulo}
              </Link>
            ))}
        </nav>
        <div className="ml-auto">
          <DropdownMenu>
            <DropdownMenuTrigger render={<Button variant="ghost" size="sm" />}>
              {usuario.nome}
              <Badge variant="secondary">{nomePerfil[usuario.perfil]}</Badge>
              <ChevronDownIcon />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuGroup>
                <DropdownMenuLabel>{usuario.email}</DropdownMenuLabel>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem render={<Link href="/status" />}>Status da plataforma</DropdownMenuItem>
              <DropdownMenuItem onClick={() => sair.mutate()} disabled={sair.isPending}>
                Sair
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    </header>
  );
}
