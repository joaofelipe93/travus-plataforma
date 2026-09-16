"use client";

import { Button } from "@/components/ui/button";
import { Trilho } from "@/components/trilho";
import { ErroApi } from "@/lib/api";
import { useSessao } from "@/lib/sessao";

// Área logada. O gateway já barra quem não tem sessão; aqui buscamos o usuário
// (perfil e token CSRF) antes de mostrar as telas.
export default function LayoutApp({ children }: { children: React.ReactNode }) {
  const sessao = useSessao();

  if (sessao.isPending) {
    return <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">Carregando…</div>;
  }
  if (sessao.isError) {
    const expirou = sessao.error instanceof ErroApi && sessao.error.status === 401;
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
        <p>{expirou ? "Sessão expirada. Redirecionando para o login…" : sessao.error.message}</p>
        {!expirou && (
          <Button variant="outline" onClick={() => sessao.refetch()}>
            Tentar de novo
          </Button>
        )}
      </div>
    );
  }

  // Trilho de serviços à esquerda; cada serviço desenha o próprio cabeçalho e abas no layout dele.
  return (
    <div className="flex min-h-svh flex-1">
      <Trilho usuario={sessao.data.usuario} />
      {children}
    </div>
  );
}
