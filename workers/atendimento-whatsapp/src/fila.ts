import { env } from './config/env.js'
import { logger } from './logger.js'
import * as plataforma from './plataforma.js'
import { mascarar, NaoConectadoError, SemWhatsappError } from './whatsapp/sessao.js'
import type { Gerente } from './whatsapp/sessoes.js'

// Laço de envio: pergunta à API o que há para enviar nos bots com sessão aberta e entrega.
//
// Três coisas que não são negociáveis aqui, as mesmas do serviço de boletos (e pelas mesmas
// razões, aprendidas lá):
//
//  1. **Ritmo.** Uma mensagem por vez, com pausa entre elas. Rajada no privado de várias pessoas
//     é o cenário clássico de bloqueio do número pela Meta. Não troque por paralelismo — nem
//     entre bots, porque o IP de saída é o mesmo.
//  2. **Resposta que saiu e não foi registrada** (a API cai entre o envio e o POST): o id fica
//     em memória e é registrado antes de qualquer outro envio. Não troque por "tenta de novo no
//     próximo ciclo": a mensagem sairia duas vezes para a pessoa.
//  3. **Número sem WhatsApp é falha definitiva**: não gasta tentativa, e aparece na tela.
//
// Uma coisa é diferente do serviço de boletos: aqui a mensagem é **resposta de conversa**. Ela
// envelhece — quem escreveu está esperando agora. Por isso a espera entre tentativas é curta (a
// API decide), e a pausa é por bot, não global.

const log = logger.child({ mod: 'fila' })

const TICK_MS = 3_000
const AVISO_SEM_SESSAO_CICLOS = 20 // ~1 min

export type LacoDaFila = ReturnType<typeof criarLacoDaFila>

/**
 * O que o laço precisa da plataforma. É injetado (e não trocado por mock de módulo) para o teste
 * exercitar o ciclo inteiro sem rede — a mesma escolha do serviço de boletos.
 */
export type ApiDaFila = {
  proximasMensagens: (bots: string[], limite: number) => Promise<plataforma.MensagemParaEnviar[]>
  marcarEnviada: (id: number) => Promise<void>
  marcarFalha: (id: number, erro: string, tentativa: number, definitivo: boolean) => Promise<void>
}

const apiPadrao: ApiDaFila = {
  proximasMensagens: plataforma.proximasMensagens,
  marcarEnviada: plataforma.marcarEnviada,
  marcarFalha: plataforma.marcarFalha,
}

export function criarLacoDaFila(
  gerente: Gerente,
  opcoes?: { tickMs?: number; api?: ApiDaFila; pausaEntreMensagensMs?: number },
) {
  const tickMs = opcoes?.tickMs ?? TICK_MS
  const api = opcoes?.api ?? apiPadrao
  // Pausa entre mensagens do mesmo ciclo: é o ritmo que protege o número. Nos testes, 0.
  const pausaMs = opcoes?.pausaEntreMensagensMs ?? 1_200
  let timer: NodeJS.Timeout | undefined
  let cicloAtual: Promise<void> | undefined
  let parado = false
  let ciclosSemSessao = 0
  // Respostas que saíram no WhatsApp mas cujo registro na API falhou.
  const enviadasSemRegistro = new Map<number, number>()

  const esperar = (ms: number) => new Promise((r) => setTimeout(r, ms))

  async function registrarPendencias(): Promise<void> {
    for (const [id, tentativa] of [...enviadasSemRegistro]) {
      try {
        await api.marcarEnviada(id)
        enviadasSemRegistro.delete(id)
        log.info({ mensagem: id, tentativa }, 'registro atrasado concluído')
      } catch (err) {
        log.warn({ err, mensagem: id }, 'registro ainda não foi aceito — tentarei no próximo ciclo')
        // Se a API continua fora, não adianta tentar as outras agora.
        return
      }
    }
  }

  async function enviarUma(m: plataforma.MensagemParaEnviar): Promise<'enviada' | 'falhou' | 'parar'> {
    const sessao = gerente.de(m.bot)
    if (!sessao || !sessao.conectada()) {
      // A sessão caiu entre a reserva e o envio: devolve à fila sem gastar tentativa de verdade.
      await api
        .marcarFalha(m.id, 'a sessão do WhatsApp caiu antes do envio', 0, false)
        .catch((err) => log.error({ err, mensagem: m.id }, 'falha ao devolver a mensagem à fila'))
      return 'parar'
    }
    try {
      await sessao.enviarTexto(m.telefone, m.texto)
      try {
        await api.marcarEnviada(m.id)
      } catch (err) {
        enviadasSemRegistro.set(m.id, m.tentativa)
        log.error({ err, mensagem: m.id },
          'resposta enviada, mas o registro na plataforma falhou — não será reenviada')
        return 'parar'
      }
      log.info({ mensagem: m.id, bot: m.bot, para: mascarar(m.telefone), conversa: m.conversa_id },
        'resposta entregue')
      return 'enviada'
    } catch (err) {
      if (err instanceof NaoConectadoError) {
        await api.marcarFalha(m.id, 'WhatsApp desconectado', 0, false).catch(() => {})
        return 'parar'
      }
      const definitivo = err instanceof SemWhatsappError
      const mensagem = err instanceof Error ? err.message : String(err)
      await api
        .marcarFalha(m.id, mensagem, m.tentativa, definitivo)
        .catch((e) => log.error({ err: e, mensagem: m.id }, 'falha ao registrar a falha de envio'))
      log[definitivo ? 'warn' : 'info'](
        { mensagem: m.id, bot: m.bot, para: mascarar(m.telefone), definitivo, err: mensagem },
        'resposta não entregue',
      )
      return 'falhou'
    }
  }

  async function ciclo(): Promise<void> {
    if (parado) return
    if (enviadasSemRegistro.size > 0) {
      await registrarPendencias()
      if (enviadasSemRegistro.size > 0) return
    }

    const conectados = gerente.conectados()
    if (conectados.length === 0) {
      ciclosSemSessao += 1
      if (ciclosSemSessao % AVISO_SEM_SESSAO_CICLOS === 0) {
        log.warn('nenhuma sessão de WhatsApp aberta — as respostas seguram na fila')
      }
      return
    }
    ciclosSemSessao = 0

    let mensagens: plataforma.MensagemParaEnviar[]
    try {
      mensagens = await api.proximasMensagens(conectados, env.LOTE)
    } catch (err) {
      log.error({ err }, 'falha ao pedir a fila à plataforma')
      return
    }
    if (mensagens.length === 0) return

    for (const m of mensagens) {
      if (parado) {
        // SIGTERM: o que não saiu volta à fila sozinho quando a trava da API vencer.
        log.info({ restantes: mensagens.length }, 'encerrando: o resto da fila fica para a próxima subida')
        return
      }
      const r = await enviarUma(m)
      if (r === 'parar') return
      if (pausaMs > 0) await esperar(pausaMs)
    }
  }

  function iniciar(): void {
    const rodar = () => {
      cicloAtual = ciclo()
        .catch((err) => log.error({ err }, 'falha no ciclo da fila'))
        .finally(() => {
          cicloAtual = undefined
          if (!parado) timer = setTimeout(rodar, tickMs)
        })
    }
    rodar()
  }

  /** Espera o ciclo em andamento: sair no meio dele mandaria a mensagem duas vezes depois. */
  async function parar(): Promise<void> {
    parado = true
    if (timer) clearTimeout(timer)
    await cicloAtual?.catch(() => {})
    if (enviadasSemRegistro.size > 0) {
      log.error({ mensagens: [...enviadasSemRegistro.keys()] },
        'respostas enviadas sem registro na plataforma: confira a conversa antes de reenviar nada')
    }
  }

  return { iniciar, parar, ciclo, pendenciasDeRegistro: () => enviadasSemRegistro.size }
}
