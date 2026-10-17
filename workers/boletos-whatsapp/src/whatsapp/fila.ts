import {
  configuracaoEnvio,
  contarPendentes,
  marcarEnviada,
  marcarFalha,
  pendentes,
  MAX_TENTATIVAS,
  type MensagemFila,
} from '../db/fila.js'
import { env } from '../config/env.js'
import { logger } from '../logger.js'
import { getStatus, isConnected } from './client.js'
import { enviarDocumento, enviarTexto, mascarar, NaoConectadoError, SemWhatsappError } from './sender.js'

/**
 * O que o laço precisa do WhatsApp. É injetado (e não trocado por mock de módulo) para o teste
 * poder exercitar o ciclo inteiro sem socket — e para a suíte rodar em qualquer Node.
 */
export type Carteiro = {
  conectado: () => boolean
  situacao: () => string
  enviarTexto: (telefone: string, texto: string) => Promise<string | undefined>
  enviarDocumento: (telefone: string, texto: string, anexo: Buffer, nome: string) => Promise<string | undefined>
}

const carteiroPadrao: Carteiro = {
  conectado: isConnected,
  situacao: getStatus,
  enviarTexto,
  enviarDocumento,
}

/**
 * Laço da fila: pega as mensagens pendentes e manda no privado de cada cliente.
 *
 * Três coisas que não são negociáveis aqui:
 *  1. **Ritmo.** Entre uma mensagem e a outra há a pausa de `boletos.configuracao`
 *     (`pausa_envio_segundos`). Disparo em rajada no privado de dezenas de clientes é o
 *     cenário clássico de bloqueio do número pela Meta. Não troque por paralelismo.
 *  2. **Envio que sai e não é registrado** (Postgres cai entre o envio e o UPDATE): o id fica
 *     em memória e é registrado antes de qualquer outro envio. Não troque por "tenta de novo
 *     no próximo ciclo": a mensagem sairia duas vezes para o cliente.
 *  3. **Número sem WhatsApp é falha definitiva**: não gasta as cinco tentativas, e aparece na
 *     tela para alguém corrigir o cadastro.
 */

const log = logger.child({ mod: 'fila' })

const TICK_MS = 5_000
const AVISO_OFFLINE_CICLOS = 12 // ~1 min

let timer: NodeJS.Timeout | undefined
let cicloAtual: Promise<void> | undefined
let offlineCiclos = 0
let offlineDesde: number | undefined

/** Mensagens que saíram no WhatsApp mas cujo registro no banco falhou. */
const enviadasSemRegistro = new Set<number>()

const esperar = (ms: number) => new Promise((r) => setTimeout(r, ms))

async function avisarDesconexao(carteiro: Carteiro) {
  offlineDesde ??= Date.now()
  offlineCiclos += 1
  if (offlineCiclos % AVISO_OFFLINE_CICLOS !== 0) return
  const fila = await contarPendentes()
  if (fila === 0) return
  log.warn({ fila, whatsapp: carteiro.situacao(), offlineMs: Date.now() - offlineDesde },
    'WhatsApp desconectado — os boletos seguram na fila')
}

function avisarReconexao() {
  if (offlineDesde === undefined) return
  log.info({ offlineMs: Date.now() - offlineDesde }, 'conexão de volta — retomando a fila')
  offlineCiclos = 0
  offlineDesde = undefined
}

async function enviarUma(m: MensagemFila, carteiro: Carteiro): Promise<'enviada' | 'falhou' | 'parar'> {
  try {
    const id =
      m.anexo_pdf && m.anexo_pdf.length > 0
        ? await carteiro.enviarDocumento(m.destino_telefone, m.texto, m.anexo_pdf, m.anexo_nome ?? 'boleto.pdf')
        : await carteiro.enviarTexto(m.destino_telefone, m.texto)
    try {
      await marcarEnviada(m.id)
    } catch (err) {
      enviadasSemRegistro.add(m.id)
      log.error({ err, mensagem: m.id },
        'mensagem enviada, mas o registro no banco falhou — não será reenviada')
      return 'parar'
    }
    log.info({ mensagem: m.id, tipo: m.tipo, rodada: m.rodada_id, destino: mascarar(m.destino_telefone), waId: id },
      'boleto enviado')
    return 'enviada'
  } catch (err) {
    if (err instanceof NaoConectadoError) {
      // Não gasta tentativa: a mensagem continua pendente para o próximo ciclo.
      log.warn({ mensagem: m.id }, 'conexão caiu antes do envio')
      return 'parar'
    }
    const texto = err instanceof Error ? err.message : String(err)
    const definitiva = err instanceof SemWhatsappError
    const r = await marcarFalha(m, texto, { definitiva })
    if (r.retentando) {
      log.warn({ mensagem: m.id, tentativas: r.tentativas, max: MAX_TENTATIVAS, proxima: r.proxima, err: texto },
        'falha no envio, reagendado')
    } else {
      log.error({ mensagem: m.id, tentativas: r.tentativas, definitiva, err: texto },
        'falha definitiva: este boleto NÃO será entregue')
    }
    return 'falhou'
  }
}

async function ciclo(carteiro: Carteiro = carteiroPadrao) {
  try {
    if (!env.ENVIO_HABILITADO) return
    if (!carteiro.conectado()) {
      await avisarDesconexao(carteiro)
      return
    }
    avisarReconexao()

    // Freio de mão do próprio serviço: a plataforma só põe na fila o que está liberado, mas se
    // o envio for desligado na tela no meio de uma rodada, o que já está na fila para aqui.
    const cfg = await configuracaoEnvio()
    if (!cfg.envio_habilitado) {
      const fila = await contarPendentes()
      if (fila > 0) {
        log.warn({ fila }, 'envio desligado na configuração — a fila espera')
      }
      return
    }

    for (const id of enviadasSemRegistro) {
      await marcarEnviada(id)
      enviadasSemRegistro.delete(id)
      log.info({ mensagem: id }, 'envio registrado depois da falha no banco')
    }

    const lote = await pendentes(env.LOTE)
    for (const [i, m] of lote.entries()) {
      if (i > 0) await esperar(cfg.pausa_envio_segundos * 1000)
      if (!carteiro.conectado()) break
      if ((await enviarUma(m, carteiro)) === 'parar') return
    }
    if (lote.length === env.LOTE) {
      log.info({ enviadas: lote.length, fila: await contarPendentes() }, 'lote cheio — ainda há fila')
    }
  } catch (err) {
    // Inclui o Postgres fora do ar: a mensagem continua pendente e o próximo ciclo tenta.
    log.error({ err }, 'erro no ciclo da fila')
  }
}

export function iniciarFila(carteiro: Carteiro = carteiroPadrao) {
  if (timer) return
  timer = setInterval(() => {
    if (cicloAtual) return // um envio lento não sobrepõe o próximo ciclo
    cicloAtual = ciclo(carteiro).finally(() => {
      cicloAtual = undefined
    })
  }, TICK_MS)
  log.info({ intervaloMs: TICK_MS, lote: env.LOTE, envioHabilitado: env.ENVIO_HABILITADO },
    'laço da fila de boletos iniciado')
}

/** Espera o ciclo em andamento (encerramento e testes). */
export function aguardarCiclo(): Promise<void> {
  return cicloAtual ?? Promise.resolve()
}

export async function pararFila(): Promise<void> {
  if (!timer) return
  clearInterval(timer)
  timer = undefined
  await aguardarCiclo()
}

/** Um ciclo, para os testes (o laço de verdade é o setInterval acima). */
export const rodarUmCiclo = ciclo

/** Esquece o estado do aviso de desconexão entre testes. */
export function _zerarEstado(): void {
  offlineCiclos = 0
  offlineDesde = undefined
  enviadasSemRegistro.clear()
}
