"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRef, useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import { nomePerfil, podeEditar, useSessao } from "@/lib/sessao";
import type { Importacao, Totais } from "@/lib/tipos";
import { Historico } from "./historico";
import { VisaoPrevia, resumirTotais } from "./previa";

export default function PaginaImportar() {
  const sessao = useSessao();
  const queryClient = useQueryClient();
  const campoArquivo = useRef<HTMLInputElement>(null);
  const [arquivo, setArquivo] = useState<File | null>(null);
  const [importacao, setImportacao] = useState<Importacao | null>(null);
  const [aplicada, setAplicada] = useState<{ arquivo: string; totais: Totais } | null>(null);
  const [confirmando, setConfirmando] = useState(false);

  const atualizarListas = () => {
    for (const chave of ["importacoes", "cotas", "clientes", "cliente"]) {
      queryClient.invalidateQueries({ queryKey: [chave] });
    }
  };

  const gerarPrevia = useMutation({
    mutationFn: (f: File) => {
      const formulario = new FormData();
      formulario.append("arquivo", f);
      return api<Importacao>("/importacoes", { metodo: "POST", formulario });
    },
    onSuccess: (imp) => {
      setImportacao(imp);
      setAplicada(null);
      queryClient.invalidateQueries({ queryKey: ["importacoes"] });
    },
  });

  const limparArquivo = () => {
    setArquivo(null);
    if (campoArquivo.current) campoArquivo.current.value = "";
  };

  const aplicar = useMutation({
    mutationFn: (imp: Importacao) => api<{ totais: Totais }>(`/importacoes/${imp.id}/aplicar`, { metodo: "POST" }),
    onSuccess: (r, imp) => {
      setConfirmando(false);
      setImportacao(null);
      setAplicada({ arquivo: imp.arquivo_nome, totais: r.totais });
      limparArquivo();
      atualizarListas();
      toast.success("Importação aplicada");
    },
    onError: () => setConfirmando(false),
  });

  const descartar = useMutation({
    mutationFn: (imp: Importacao) => api(`/importacoes/${imp.id}/descartar`, { metodo: "POST" }),
    onSuccess: () => {
      setImportacao(null);
      aplicar.reset();
      limparArquivo();
      queryClient.invalidateQueries({ queryKey: ["importacoes"] });
      toast("Prévia descartada: nada foi gravado");
    },
    onError: (e) => toast.error(e.message),
  });

  const perfil = sessao.data?.usuario.perfil;
  if (!podeEditar(perfil)) {
    return (
      <div className="flex flex-col gap-4">
        <h1 className="text-xl font-semibold tracking-tight">Importar planilha</h1>
        <Alert>
          <AlertDescription>
            Seu perfil ({perfil ? nomePerfil[perfil] : "—"}) permite só consultar. Peça a um operador ou administrador para importar a planilha.
          </AlertDescription>
        </Alert>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Importar planilha</h1>
        <p className="text-sm text-muted-foreground">
          Envie a planilha de clientes em CSV. Nada é gravado antes de você revisar a prévia e aplicar.
        </p>
      </div>

      {aplicada && (
        <Alert>
          <AlertTitle>Importação de {aplicada.arquivo} aplicada</AlertTitle>
          <AlertDescription>
            {resumirTotais(aplicada.totais)}.{" "}
            <Link href="/cotas" className="underline">
              Ver cotas
            </Link>
          </AlertDescription>
        </Alert>
      )}

      {!importacao && (
        <Card>
          <CardContent>
            <form
              className="flex flex-col gap-3 sm:flex-row sm:items-end"
              onSubmit={(e) => {
                e.preventDefault();
                if (arquivo) gerarPrevia.mutate(arquivo);
              }}
            >
              <div className="grid flex-1 gap-2">
                <Label htmlFor="arquivo">Planilha (CSV, até 2 MB)</Label>
                <Input
                  id="arquivo"
                  ref={campoArquivo}
                  type="file"
                  accept=".csv,text/csv"
                  onChange={(e) => {
                    setArquivo(e.target.files?.[0] ?? null);
                    gerarPrevia.reset();
                  }}
                />
              </div>
              <Button type="submit" disabled={!arquivo || gerarPrevia.isPending}>
                {gerarPrevia.isPending ? "Lendo planilha…" : "Gerar prévia"}
              </Button>
            </form>
            {gerarPrevia.isError && (
              <Alert variant="destructive" className="mt-4">
                <AlertTitle>Não foi possível ler a planilha</AlertTitle>
                <AlertDescription>{gerarPrevia.error.message}</AlertDescription>
              </Alert>
            )}
          </CardContent>
        </Card>
      )}

      {importacao && (
        <VisaoPrevia
          importacao={importacao}
          erroAoAplicar={aplicar.error?.message}
          ocupado={aplicar.isPending || descartar.isPending}
          onAplicar={() => setConfirmando(true)}
          onDescartar={() => descartar.mutate(importacao)}
        />
      )}

      <AlertDialog
        open={confirmando}
        onOpenChange={(aberto) => {
          if (!aplicar.isPending) setConfirmando(aberto);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Aplicar a importação?</AlertDialogTitle>
            <AlertDialogDescription>
              {importacao && `${importacao.arquivo_nome}: ${resumirTotais(importacao.previa.totais)}.`} Cotas fora da planilha não são desativadas.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={aplicar.isPending}>Cancelar</AlertDialogCancel>
            <AlertDialogAction disabled={aplicar.isPending} onClick={() => importacao && aplicar.mutate(importacao)}>
              {aplicar.isPending ? "Aplicando…" : "Aplicar"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Historico />
    </div>
  );
}
