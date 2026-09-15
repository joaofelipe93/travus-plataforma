// Cliente da API. O navegador fala com a API pelo mesmo endereço do app
// (app.<domínio>/api/...), então o cookie de sessão vai sozinho e não há CORS.

export class ErroApi extends Error {
  constructor(
    public readonly status: number,
    mensagem: string,
  ) {
    super(mensagem);
    this.name = "ErroApi";
  }
}

// Token CSRF da sessão atual (vem de /auth/login e /auth/sessao). Fica só em memória.
let tokenCsrf: string | null = null;

export function definirCsrf(token: string | null) {
  tokenCsrf = token;
}

type Opcoes = {
  metodo?: "GET" | "POST" | "PATCH" | "DELETE";
  json?: unknown;
  formulario?: FormData;
};

export async function api<T>(caminho: string, { metodo = "GET", json, formulario }: Opcoes = {}): Promise<T> {
  const cabecalhos: Record<string, string> = {};
  let corpo: BodyInit | undefined;
  if (json !== undefined) {
    cabecalhos["Content-Type"] = "application/json";
    corpo = JSON.stringify(json);
  } else if (formulario) {
    corpo = formulario;
  }
  if (metodo !== "GET" && tokenCsrf) {
    cabecalhos["X-CSRF-Token"] = tokenCsrf;
  }

  let resposta: Response;
  try {
    resposta = await fetch(`/api${caminho}`, { method: metodo, headers: cabecalhos, body: corpo, cache: "no-store" });
  } catch {
    throw new ErroApi(0, "Sem conexão com o servidor. Verifique a internet e tente de novo.");
  }

  if (resposta.status === 204) {
    return undefined as T;
  }
  const texto = await resposta.text();
  let dados: unknown = null;
  try {
    dados = texto ? JSON.parse(texto) : null;
  } catch {
    // Respostas do gateway (ex.: 429) não são JSON.
  }

  if (!resposta.ok) {
    const mensagem = (dados as { erro?: string } | null)?.erro ?? mensagemPadrao(resposta.status);
    if (resposta.status === 401 && caminho !== "/auth/login") {
      irParaLogin();
    }
    throw new ErroApi(resposta.status, mensagem);
  }
  return dados as T;
}

function mensagemPadrao(status: number): string {
  if (status === 429) return "Muitas tentativas seguidas. Aguarde um minuto e tente de novo.";
  if (status === 403) return "Seu perfil não permite esta ação.";
  if (status === 401) return "Sessão expirada: entre novamente.";
  if (status >= 500) return "Erro no servidor. Tente de novo; se continuar, avise o suporte.";
  return `Erro inesperado (HTTP ${status}).`;
}

export function irParaLogin() {
  if (window.location.pathname === "/login") return;
  const aqui = window.location.pathname + window.location.search;
  // Fora de componente (sem useRouter) e com recarga completa de propósito: sessão expirada
  // não pode deixar dados em cache na tela.
  // eslint-disable-next-line @next/next/no-location-assign-relative-destination
  window.location.assign(`/login?proximo=${encodeURIComponent(aqui)}`);
}
