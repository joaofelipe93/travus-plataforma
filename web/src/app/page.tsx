import { connection } from "next/server";

type Health = {
  status: string;
  servico: string;
  banco: string;
  via_gateway: boolean;
  host?: string;
  horario: string;
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

function Linha({ rotulo, children }: { rotulo: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5 py-2 sm:flex-row sm:gap-4">
      <dt className="w-40 shrink-0 text-sm text-zinc-500">{rotulo}</dt>
      <dd className="text-sm break-all">{children}</dd>
    </div>
  );
}

export default async function Home() {
  // Renderiza a cada requisição (e lê API_INTERNAL_URL em tempo de execução, não no build).
  await connection();
  const consulta = await consultarHealth();

  const tudoOk = consulta.ok && consulta.httpStatus === 200 && consulta.corpo.via_gateway;
  const selo = !consulta.ok
    ? { texto: "sem resposta", cor: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-200" }
    : tudoOk
      ? { texto: "ok", cor: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200" }
      : { texto: consulta.corpo.status, cor: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-200" };

  return (
    <main className="mx-auto flex w-full max-w-2xl flex-1 flex-col gap-8 px-4 py-12 sm:py-20">
      <header>
        <p className="text-sm text-zinc-500">Etapa 0 · fundação</p>
        <h1 className="text-3xl font-semibold tracking-tight">Travus Plataforma</h1>
      </header>

      <section className="rounded-lg border border-black/10 bg-white p-5 dark:border-white/10 dark:bg-zinc-900">
        <div className="mb-3 flex items-center justify-between gap-4">
          <h2 className="font-medium">API pelo gateway</h2>
          <span className={`rounded-full px-2.5 py-0.5 text-xs font-medium ${selo.cor}`}>{selo.texto}</span>
        </div>

        <dl className="divide-y divide-black/5 dark:divide-white/5">
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
            </>
          ) : (
            <Linha rotulo="Erro">{consulta.erro}</Linha>
          )}
        </dl>

        {consulta.ok && (
          <pre className="mt-4 overflow-x-auto rounded-md bg-zinc-100 p-3 font-mono text-xs dark:bg-zinc-950">
            {JSON.stringify(consulta.corpo, null, 2)}
          </pre>
        )}
      </section>
    </main>
  );
}
