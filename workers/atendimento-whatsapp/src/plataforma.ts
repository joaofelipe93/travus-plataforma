import { env } from './config/env.js'
import { logger } from './logger.js'

// Cliente das rotas internas da API (porta 8081, token de serviço). É a única fonte de verdade
// deste worker: ele não tem banco e não decide nada.
//
// A API é a cabeça, o worker são as pernas (decisão da etapa A da issue #100): aqui não existe
// ideia de etapa, teto, ensaio ou instrução de bot — só "chegou isto", "o que devo enviar" e
// "enviei" / "não consegui".

const log = logger.child({ mod: 'plataforma' })

export type BotDoWorker = {
  slug: string
  nome: string
  ativo: boolean
  pausa_resposta_segundos: number
}

export type MensagemParaEnviar = {
  id: number
  conversa_id: number
  bot: string
  telefone: string
  texto: string
  tentativa: number
}

export type ResultadoDaMensagem = {
  conversa_id: number
  status: string
  motivo: string
  encerrada: boolean
}

export class ErroDaPlataforma extends Error {
  constructor(
    readonly status: number,
    mensagem: string,
  ) {
    super(mensagem)
    this.name = 'ErroDaPlataforma'
  }
}

async function chamar<T>(metodo: string, caminho: string, corpo?: unknown): Promise<T> {
  const resposta = await fetch(`${env.API_URL}${caminho}`, {
    method: metodo,
    headers: {
      Authorization: `Bearer ${env.WORKER_TOKEN}`,
      'X-Worker': env.WORKER_NOME,
      ...(corpo === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    body: corpo === undefined ? undefined : JSON.stringify(corpo),
  })
  const texto = await resposta.text()
  if (!resposta.ok) {
    let detalhe = texto.slice(0, 300)
    try {
      const json = JSON.parse(texto) as { erro?: string }
      if (json.erro) detalhe = json.erro
    } catch {
      // resposta não-JSON (gateway, proxy): o texto cru já serve
    }
    throw new ErroDaPlataforma(resposta.status, `${metodo} ${caminho}: HTTP ${resposta.status} — ${detalhe}`)
  }
  return (texto ? JSON.parse(texto) : null) as T
}

/** Quais bots conectar. A API só devolve os ligados. */
export async function bots(): Promise<BotDoWorker[]> {
  const r = await chamar<{ bots: BotDoWorker[] }>('GET', '/internal/atendimento/bots')
  return r.bots ?? []
}

/**
 * Entrega uma mensagem que chegou no WhatsApp. A resposta do bot NÃO volta por aqui: a API a põe
 * na fila, e o laço de envio a leva. Assim uma resposta que demora não prende o socket, e o que
 * não pôde sair fica registrado em vez de se perder.
 */
export async function receber(
  bot: string,
  telefone: string,
  contato: string | undefined,
  texto: string,
): Promise<ResultadoDaMensagem> {
  return chamar<ResultadoDaMensagem>('POST', '/internal/atendimento/mensagens', {
    bot,
    telefone,
    contato: contato ?? '',
    texto,
  })
}

/** O que há para enviar nos bots com sessão aberta. */
export async function proximasMensagens(botsConectados: string[], limite: number): Promise<MensagemParaEnviar[]> {
  if (botsConectados.length === 0) return []
  const r = await chamar<{ mensagens: MensagemParaEnviar[] }>('POST', '/internal/atendimento/fila/proxima', {
    bots: botsConectados,
    limite,
  })
  return r.mensagens ?? []
}

export async function marcarEnviada(id: number): Promise<void> {
  await chamar<unknown>('POST', `/internal/atendimento/mensagens/${id}/enviada`, {})
}

/**
 * Registra a falha. `definitivo` é para o que não melhora tentando de novo (número sem
 * WhatsApp); quem decide entre reenfileirar e desistir é a API.
 */
export async function marcarFalha(
  id: number,
  erro: string,
  tentativa: number,
  definitivo: boolean,
): Promise<void> {
  await chamar<unknown>('POST', `/internal/atendimento/mensagens/${id}/falhou`, {
    erro: erro.slice(0, 400),
    tentativa,
    definitivo,
  })
}

/** Usado no boot: falha aqui é motivo para o container reiniciar, não para seguir cego. */
export async function conferirApi(): Promise<void> {
  const lista = await bots()
  log.info({ bots: lista.length }, 'API da plataforma respondeu')
}
