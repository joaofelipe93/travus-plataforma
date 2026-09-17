"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { api } from "@/lib/api";
import { dataHora, nomePerfil } from "@/lib/sessao";
import type { Credencial, MeuPerfil, RespostaPerfil } from "@/lib/tipos";

// Meu perfil: dados de contato (para o suporte saber a quem recorrer) e os acessos pessoais a
// serviços de terceiros. O valor de um token nunca vem da API: a tela mostra só a dica.

export default function PaginaPerfil() {
  const perfil = useQuery({ queryKey: ["perfil"], queryFn: () => api<RespostaPerfil>("/perfil") });

  return (
    <main className="flex w-full max-w-3xl flex-1 flex-col gap-6 px-8 py-8">
      <div>
        <h1 className="font-heading text-2xl font-semibold">Meu perfil</h1>
        <p className="mt-0.5 text-sm text-muted-foreground">Seus dados de contato e os acessos que só você cadastra.</p>
      </div>

      {perfil.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{perfil.error.message}</AlertDescription>
        </Alert>
      ) : !perfil.data ? (
        <Skeleton className="h-96 w-full" />
      ) : (
        <>
          <Dados key={perfil.data.usuario.atualizado_em} usuario={perfil.data.usuario} />
          <Acessos credenciais={perfil.data.credenciais} cofreConfigurado={perfil.data.cofre_configurado} />
        </>
      )}
    </main>
  );
}

function Secao({ titulo, descricao, children }: { titulo: string; descricao?: string; children: React.ReactNode }) {
  return (
    <section className="rounded-lg border p-5">
      <div className="mb-4">
        <h2 className="font-medium">{titulo}</h2>
        {descricao && <p className="mt-0.5 text-sm text-muted-foreground">{descricao}</p>}
      </div>
      {children}
    </section>
  );
}

function Campo({ id, rotulo, ajuda, children }: { id: string; rotulo: string; ajuda?: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={id}>{rotulo}</Label>
      {children}
      {ajuda && <p className="text-xs text-muted-foreground">{ajuda}</p>}
    </div>
  );
}

function Dados({ usuario }: { usuario: MeuPerfil }) {
  const queryClient = useQueryClient();
  const [nome, setNome] = useState(usuario.nome);
  const [telefone, setTelefone] = useState(usuario.telefone ?? "");
  const [cargo, setCargo] = useState(usuario.cargo ?? "");
  const [observacoes, setObservacoes] = useState(usuario.observacoes ?? "");

  const mudou =
    nome !== usuario.nome ||
    telefone !== (usuario.telefone ?? "") ||
    cargo !== (usuario.cargo ?? "") ||
    observacoes !== (usuario.observacoes ?? "");

  const salvar = useMutation({
    mutationFn: () => api<MeuPerfil>("/perfil", { metodo: "PATCH", json: { nome, telefone, cargo, observacoes } }),
    onSuccess: () => {
      toast.success("Perfil salvo");
      queryClient.invalidateQueries({ queryKey: ["perfil"] });
      // O nome aparece no trilho: recarrega a sessão para não ficar o antigo.
      queryClient.invalidateQueries({ queryKey: ["sessao"] });
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <Secao titulo="Dados">
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          salvar.mutate();
        }}
      >
        <Campo id="nome" rotulo="Nome">
          <Input id="nome" required maxLength={120} value={nome} onChange={(e) => setNome(e.target.value)} />
        </Campo>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="email" rotulo="E-mail" ajuda="É o seu login: quem troca é um administrador.">
            <Input id="email" value={usuario.email} readOnly disabled />
          </Campo>
          <Campo id="perfil" rotulo="Perfil" ajuda="Define o que você pode fazer; quem muda é um administrador.">
            <div className="flex h-8 items-center">
              <Badge variant="secondary">{nomePerfil[usuario.perfil]}</Badge>
            </div>
          </Campo>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Campo id="telefone" rotulo="Celular / WhatsApp" ajuda="Com DDD, por exemplo (11) 90000-0000.">
            <Input
              id="telefone"
              type="tel"
              inputMode="tel"
              autoComplete="tel"
              maxLength={25}
              value={telefone}
              onChange={(e) => setTelefone(e.target.value)}
            />
          </Campo>
          <Campo id="cargo" rotulo="Cargo" ajuda="Como você aparece para os colegas.">
            <Input id="cargo" maxLength={80} value={cargo} onChange={(e) => setCargo(e.target.value)} />
          </Campo>
        </div>

        <Campo id="observacoes" rotulo="Observações" ajuda="Horário de trabalho, quem procurar na sua ausência… (até 500 caracteres)">
          <Textarea id="observacoes" maxLength={500} rows={3} value={observacoes} onChange={(e) => setObservacoes(e.target.value)} />
        </Campo>

        <div className="flex items-center gap-3">
          <Button type="submit" disabled={!mudou || salvar.isPending}>
            {salvar.isPending ? "Salvando…" : "Salvar"}
          </Button>
          {mudou && !salvar.isPending && <span className="text-sm text-muted-foreground">Você tem mudanças por salvar.</span>}
        </div>
      </form>
    </Secao>
  );
}

function Acessos({ credenciais, cofreConfigurado }: { credenciais: Credencial[]; cofreConfigurado: boolean }) {
  return (
    <Secao
      titulo="Acessos"
      descricao="Seus tokens em serviços de terceiros. Eles ficam cifrados no servidor, valem só para você e nunca voltam para a tela: se esquecer, cadastre outro."
    >
      {!cofreConfigurado && (
        <Alert variant="destructive" className="mb-4">
          <AlertTitle>Não dá para guardar acessos agora</AlertTitle>
          <AlertDescription>
            O servidor está sem a chave de criptografia (CHAVE_CRIPTOGRAFIA). Avise um administrador: sem ela, nenhum token
            pode ser gravado com segurança.
          </AlertDescription>
        </Alert>
      )}
      {credenciais.length === 0 ? (
        <p className="text-sm text-muted-foreground">Nenhum serviço pede acesso pessoal por enquanto.</p>
      ) : (
        <ul className="flex flex-col divide-y">
          {credenciais.map((c) => (
            <li key={c.id} className="py-4 first:pt-0 last:pb-0">
              <ItemCredencial credencial={c} cofreConfigurado={cofreConfigurado} />
            </li>
          ))}
        </ul>
      )}
    </Secao>
  );
}

function ItemCredencial({ credencial, cofreConfigurado }: { credencial: Credencial; cofreConfigurado: boolean }) {
  const [editando, setEditando] = useState(false);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h3 className="font-medium">{credencial.nome}</h3>
            {credencial.cadastrada ? <Badge variant="secondary">cadastrado</Badge> : <Badge variant="outline">falta cadastrar</Badge>}
          </div>
          <p className="mt-1 text-sm text-muted-foreground">{credencial.descricao}</p>
          {credencial.cadastrada && (
            <p className="mt-1 text-sm text-muted-foreground">
              {credencial.dica ? (
                <>
                  Termina em <span className="font-mono">{credencial.dica}</span>, cadastrado em{" "}
                </>
              ) : (
                "Cadastrado em "
              )}
              {dataHora(credencial.atualizada_em)}.
            </p>
          )}
        </div>
        <div className="flex shrink-0 gap-2">
          {!editando && (
            <Button variant={credencial.cadastrada ? "outline" : "default"} disabled={!cofreConfigurado} onClick={() => setEditando(true)}>
              {credencial.cadastrada ? "Trocar" : "Cadastrar"}
            </Button>
          )}
          {credencial.cadastrada && !editando && <Remover credencial={credencial} />}
        </div>
      </div>

      {editando && <FormularioCredencial credencial={credencial} aoFechar={() => setEditando(false)} />}
    </div>
  );
}

function FormularioCredencial({ credencial, aoFechar }: { credencial: Credencial; aoFechar: () => void }) {
  const queryClient = useQueryClient();
  const [valores, setValores] = useState<Record<string, string>>({});

  const salvar = useMutation({
    mutationFn: () => api(`/perfil/credenciais/${credencial.id}`, { metodo: "PUT", json: { valores } }),
    onSuccess: () => {
      toast.success(`${credencial.nome} cadastrado`);
      aoFechar();
      queryClient.invalidateQueries({ queryKey: ["perfil"] });
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <form
      className="flex flex-col gap-4 rounded-lg border bg-muted/30 p-4"
      onSubmit={(e) => {
        e.preventDefault();
        salvar.mutate();
      }}
    >
      <p className="text-sm">{credencial.como_obter}</p>
      {credencial.link_ajuda && (
        <a
          href={credencial.link_ajuda}
          target="_blank"
          rel="noreferrer noopener"
          className="w-fit text-sm underline underline-offset-4 outline-none hover:text-ouro focus-visible:text-ouro"
        >
          Abrir a página do {credencial.nome}
        </a>
      )}

      {credencial.campos.map((campo) => (
        <Campo key={campo.id} id={`${credencial.id}-${campo.id}`} rotulo={campo.rotulo} ajuda={campo.ajuda}>
          <Input
            id={`${credencial.id}-${campo.id}`}
            // Segredo: sem preenchimento automático, sem histórico do navegador.
            type="password"
            autoComplete="off"
            spellCheck={false}
            required={campo.obrigatorio}
            maxLength={500}
            value={valores[campo.id] ?? ""}
            onChange={(e) => setValores((v) => ({ ...v, [campo.id]: e.target.value }))}
          />
        </Campo>
      ))}

      <div className="flex gap-2">
        <Button type="submit" disabled={salvar.isPending}>
          {salvar.isPending ? "Guardando…" : "Guardar"}
        </Button>
        <Button type="button" variant="outline" disabled={salvar.isPending} onClick={aoFechar}>
          Cancelar
        </Button>
      </div>
    </form>
  );
}

function Remover({ credencial }: { credencial: Credencial }) {
  const queryClient = useQueryClient();
  const [confirmando, setConfirmando] = useState(false);
  const remover = useMutation({
    mutationFn: () => api(`/perfil/credenciais/${credencial.id}`, { metodo: "DELETE" }),
    onSuccess: () => {
      setConfirmando(false);
      toast(`${credencial.nome} removido do seu perfil`);
      queryClient.invalidateQueries({ queryKey: ["perfil"] });
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <>
      <Button variant="outline" onClick={() => setConfirmando(true)}>
        Remover
      </Button>
      <AlertDialog
        open={confirmando}
        onOpenChange={(aberto) => {
          if (!remover.isPending) setConfirmando(aberto);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remover seu acesso ao {credencial.nome}?</AlertDialogTitle>
            <AlertDialogDescription>
              O token sai do servidor e os serviços que dependem dele param de funcionar para você até cadastrar outro. O
              acesso no {credencial.nome} continua existindo: para revogá-lo de vez, faça isso lá também.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remover.isPending}>Cancelar</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={remover.isPending} onClick={() => remover.mutate()}>
              {remover.isPending ? "Removendo…" : "Remover"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
