import { connection } from "next/server";
import { Badge } from "@/components/ui/badge";

type Health = {
  status: string;
  servico: string;
  banco: string;
  via_gateway: boolean;
  host?: string;
  horario: string;
  versao?: string;
  commit?: string;
};

type Consulta =
  | { ok: true; url: string; httpStatus: number; latenciaMs: number; corpo: Health }
  | { ok: false; url: string; erro: string };

// Chamada feita no servidor do Next.js. API_INTERNAL_URL aponta para o gateway
// (http://api.localhost resolve para o Traefik dentro da rede do Docker).
async function consultarHealth(): Promise<Consulta> {
  const url = `${process.env.API_INTERNAL_URL ?? "http://api.localhost"}/health`;
  const inicio = performance.now();
  try {
    const resp = await fetch(url, { cache: "no-store", signal: AbortSignal.timeout(5000) });
    const texto = await resp.text();
    let corpo: Health;
    try {
      corpo = JSON.parse(texto) as Health;
    } catch {
      return { ok: false, url, erro: `HTTP ${resp.status}, resposta não é JSON: ${texto.slice(0, 120)}` };
    }
    return { ok: true, url, httpStatus: resp.status, latenciaMs: Math.round(performance.now() - inicio), corpo };
  } catch (e) {
    const causa = e instanceof Error && e.cause instanceof Error ? ` (${e.cause.message})` : "";
    return { ok: false, url, erro: `${e instanceof Error ? e.message : String(e)}${causa}` };
  }
}

function rotuloVersao(versao: string | undefined) {
  return versao && versao !== "dev" ? `v${versao}` : "dev";
}

function Linha({ rotulo, children }: { rotulo: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5 py-2 sm:flex-row sm:gap-4">
      <dt className="w-40 shrink-0 text-sm text-muted-foreground">{rotulo}</dt>
      <dd className="text-sm break-all">{children}</dd>
    </div>
  );
}

export default async function PaginaStatus() {
  // Renderiza a cada requisição (e lê API_INTERNAL_URL em tempo de execução, não no build).
  await connection();
  const consulta = await consultarHealth();
  const tudoOk = consulta.ok && consulta.httpStatus === 200 && consulta.corpo.via_gateway;

  return (
    <main className="flex max-w-2xl flex-1 flex-col gap-6 px-8 py-8">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Status da plataforma</h1>
        <p className="text-sm text-muted-foreground">Consulta feita agora, pelo servidor do app, passando pelo gateway.</p>
      </div>

      <section className="rounded-lg border p-5">
        <div className="mb-3 flex items-center justify-between gap-4">
          <h2 className="font-medium">API pelo gateway</h2>
          {!consulta.ok ? (
            <Badge variant="destructive">sem resposta</Badge>
          ) : tudoOk ? (
            <Badge variant="secondary">ok</Badge>
          ) : (
            <Badge variant="outline">{consulta.corpo.status}</Badge>
          )}
        </div>

        <dl className="divide-y">
          <Linha rotulo="Versão do app">
            {rotuloVersao(process.env.NEXT_PUBLIC_VERSAO)} (commit {(process.env.NEXT_PUBLIC_COMMIT ?? "desconhecido").slice(0, 7)})
          </Linha>
          <Linha rotulo="Endereço consultado">
            <code className="font-mono">{consulta.url}</code>
          </Linha>
          {consulta.ok ? (
            <>
              <Linha rotulo="HTTP">
                {consulta.httpStatus} em {consulta.latenciaMs} ms
              </Linha>
              <Linha rotulo="Passou pelo Traefik">
                {consulta.corpo.via_gateway ? `sim (host ${consulta.corpo.host})` : "não"}
              </Linha>
              <Linha rotulo="Banco (Postgres)">{consulta.corpo.banco}</Linha>
              <Linha rotulo="Horário da API">{consulta.corpo.horario}</Linha>
              <Linha rotulo="Versão da API">
                {rotuloVersao(consulta.corpo.versao)} (commit {(consulta.corpo.commit ?? "desconhecido").slice(0, 7)})
              </Linha>
            </>
          ) : (
            <Linha rotulo="Erro">{consulta.erro}</Linha>
          )}
        </dl>
      </section>
    </main>
  );
}
