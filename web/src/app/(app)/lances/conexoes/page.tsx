"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { Bloco, Estados, type ItemEstado } from "@/components/painel";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { buttonVariants } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { api } from "@/lib/api";
import { dataHora, useSessao } from "@/lib/sessao";
import type { RespostaConfiguracaoCanopus, SituacaoGoogleDrive } from "@/lib/tipos";

// Conexões do serviço de Lances. Ele não manda WhatsApp: as contas de terceiros que ele usa são
// o **Newcon** (credenciais só no servidor, nunca na tela) e o **Google Drive**, que recebe o
// comprovante de cada lance registrado. Esta tela é a resposta para "por que o comprovante não
// chegou na pasta?" — antes dela isso só se via por `make google-status`, no terminal.
//
// Nada aqui é editável: o token do Google é importado no servidor (make google-token) e a pasta
// é escolhida em Configurações, que admin e operador editam.

export default function PaginaConexoesLances() {
  const sessao = useSessao();
  if (sessao.data?.usuario.perfil !== "admin") {
    return (
      <Alert>
        <AlertTitle>Só administradores</AlertTitle>
        <AlertDescription>
          A situação da conta do Google é de administrador. A pasta dos comprovantes aparece em{" "}
          <Link href="/lances/configuracao" className="underline underline-offset-4">
            Configurações
          </Link>
          .
        </AlertDescription>
      </Alert>
    );
  }
  return <Conexoes />;
}

function Conexoes() {
  const drive = useQuery({
    queryKey: ["integracao-google-drive"],
    queryFn: () => api<SituacaoGoogleDrive>("/integracoes/google-drive"),
  });
  const config = useQuery({
    queryKey: ["configuracao-canopus"],
    queryFn: () => api<RespostaConfiguracaoCanopus>("/configuracao/canopus"),
  });

  const erro = drive.error ?? config.error;
  if (erro) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{erro.message}</AlertDescription>
      </Alert>
    );
  }
  if (!drive.data || !config.data) {
    return <Skeleton className="h-72 w-full max-w-3xl" />;
  }

  const d = drive.data;
  const c = config.data.configuracao;
  const pronto = d.token_importado && d.client_configurado && d.cofre_configurado && d.pasta_origem !== "nenhuma";

  const google: ItemEstado[] = [
    {
      rotulo: "Token da conta",
      valor: d.token_importado ? "importado" : "não importado",
      tom: d.token_importado ? "ok" : "erro",
      detalhe: d.token_importado ? dataHora(d.token_atualizado_em) : undefined,
    },
    {
      rotulo: "Aplicativo OAuth",
      valor: d.client_configurado ? "configurado" : "faltando",
      tom: d.client_configurado ? "ok" : "erro",
      detalhe: "GOOGLE_CLIENT_ID e GOOGLE_CLIENT_SECRET",
    },
    {
      rotulo: "Cofre dos segredos",
      valor: d.cofre_configurado ? "configurado" : "faltando",
      tom: d.cofre_configurado ? "ok" : "erro",
      detalhe: "CHAVE_CRIPTOGRAFIA",
    },
    {
      rotulo: "Pasta dos comprovantes",
      valor: d.pasta_origem === "nenhuma" ? "nenhuma" : <span className="font-mono text-xs">{c.drive_pasta_em_vigor}</span>,
      tom: d.pasta_origem === "nenhuma" ? "aviso" : "neutro",
      detalhe:
        d.pasta_origem === "ambiente" ? "definida no servidor" : d.pasta_origem === "configuracao" ? "escolhida na tela" : undefined,
    },
  ];

  return (
    <div className="flex max-w-3xl flex-col gap-8">
      <Bloco
        titulo="Google Drive"
        descricao="Para onde sobe o PDF de cada lance registrado, com o mesmo nome de arquivo do script antigo."
        tom={pronto ? "ok" : "aviso"}
        acoes={
          <Link href="/lances/configuracao" className={buttonVariants({ variant: "outline", size: "sm" })}>
            Trocar a pasta
          </Link>
        }
      >
        <Estados itens={google} />
        {!pronto && (
          <Alert>
            <AlertTitle>O comprovante não vai subir</AlertTitle>
            <AlertDescription>
              {!d.token_importado && "Falta importar o token da conta do Google no servidor (make google-token). "}
              {!d.client_configurado && "Falta o aplicativo OAuth no deploy/.env. "}
              {!d.cofre_configurado && "Sem CHAVE_CRIPTOGRAFIA a API não decifra o token guardado. "}
              {d.pasta_origem === "nenhuma" && "Falta escolher a pasta em Configurações. "}
              O lance continua sendo registrado e o PDF fica guardado na plataforma: dá para enviá-lo ao Drive
              depois, pela ficha do cliente.
            </AlertDescription>
          </Alert>
        )}
      </Bloco>

      <Bloco
        titulo="Newcon (Canopus)"
        descricao="O site onde o worker credencia os lances."
        tom={drive.data.lance_real_habilitado ? "aviso" : "neutro"}
      >
        <Estados
          itens={[
            {
              rotulo: "Credenciais",
              valor: "só no servidor",
              detalhe: "NEWCON_USER e NEWCON_PASS, no worker",
            },
            {
              rotulo: "Lance real",
              valor: d.lance_real_habilitado ? "liberado" : "bloqueado",
              tom: d.lance_real_habilitado ? "aviso" : "neutro",
              detalhe: "LANCE_REAL_HABILITADO",
            },
          ]}
        />
        <p className="text-sm text-muted-foreground">
          A sessão do Newcon não se conecta por aqui: o worker entra no site a cada execução, com as credenciais
          do servidor, e só uma execução roda por vez. Uma execução que não sai da fila costuma ser o worker
          fora do ar — veja o{" "}
          <Link href="/status" className="underline underline-offset-4">
            status da plataforma
          </Link>
          .
        </p>
      </Bloco>
    </div>
  );
}
