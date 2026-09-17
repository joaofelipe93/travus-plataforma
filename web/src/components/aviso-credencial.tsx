"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";
import type { RespostaPerfil } from "@/lib/tipos";

// Um serviço que age em nome da pessoa (ex.: Trello) usa isto para pedir o token de quem está
// na tela, em vez de simplesmente falhar. A API decide de verdade: aqui é só o aviso.

// useCredencial diz se a pessoa já cadastrou a credencial: use para esconder o botão que
// dependeria dela. Enquanto carrega, `cadastrada` é undefined.
export function useCredencial(id: string) {
  const perfil = useQuery({ queryKey: ["perfil"], queryFn: () => api<RespostaPerfil>("/perfil") });
  const credencial = perfil.data?.credenciais.find((c) => c.id === id);
  return { cadastrada: credencial?.cadastrada, credencial, carregando: perfil.isPending };
}

// AvisoCredencial não mostra nada quando a credencial já está cadastrada (nem enquanto carrega).
export function AvisoCredencial({ id, children }: { id: string; children?: React.ReactNode }) {
  const { credencial } = useCredencial(id);
  if (!credencial || credencial.cadastrada) return null;

  return (
    <Alert>
      <AlertTitle>Falta o seu acesso ao {credencial.nome}</AlertTitle>
      <AlertDescription className="flex flex-col items-start gap-3">
        <span>
          {children ?? `Este serviço age no ${credencial.nome} em seu nome, então precisa do seu token.`} Cadastre uma vez
          em Meu perfil; ele fica guardado cifrado e vale só para você.
        </span>
        <Button variant="outline" render={<Link href="/perfil" />}>
          Cadastrar em Meu perfil
        </Button>
      </AlertDescription>
    </Alert>
  );
}
