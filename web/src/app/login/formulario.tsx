"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, definirCsrf } from "@/lib/api";
import type { Sessao } from "@/lib/tipos";

// Só caminhos locais: evita usar o login para mandar alguém para outro site.
function destinoSeguro(proximo: string | null) {
  if (!proximo || !proximo.startsWith("/") || proximo.startsWith("//") || proximo.startsWith("/\\") || proximo.startsWith("/login")) {
    return "/canopus/execucoes";
  }
  return proximo;
}

export function FormularioLogin() {
  const parametros = useSearchParams();
  const router = useRouter();
  const queryClient = useQueryClient();
  const [email, setEmail] = useState("");
  const [senha, setSenha] = useState("");

  const entrar = useMutation({
    mutationFn: () => api<Sessao>("/auth/login", { metodo: "POST", json: { email, senha } }),
    onSuccess: (sessao) => {
      definirCsrf(sessao.csrf_token);
      queryClient.setQueryData(["sessao"], sessao);
      router.replace(destinoSeguro(parametros.get("proximo")));
    },
    onError: () => setSenha(""),
  });

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        entrar.mutate();
      }}
    >
      <div className="grid gap-2">
        <Label htmlFor="email">E-mail</Label>
        <Input id="email" type="email" autoComplete="username" required autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />
      </div>
      <div className="grid gap-2">
        <Label htmlFor="senha">Senha</Label>
        <Input id="senha" type="password" autoComplete="current-password" required value={senha} onChange={(e) => setSenha(e.target.value)} />
      </div>
      {entrar.isError && (
        <Alert variant="destructive">
          <AlertDescription>{entrar.error.message}</AlertDescription>
        </Alert>
      )}
      <Button type="submit" size="lg" className="mt-2" disabled={entrar.isPending || entrar.isSuccess}>
        {entrar.isPending || entrar.isSuccess ? "Entrando…" : "Entrar"}
      </Button>
    </form>
  );
}
