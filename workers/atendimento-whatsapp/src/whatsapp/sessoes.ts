import { env } from '../config/env.js'
import { logger } from '../logger.js'
import * as plataforma from '../plataforma.js'
import { criarSessao, mascarar, type MensagemRecebida, type Sessao } from './sessao.js'

// O gerente das sessões: um processo, N sessões de WhatsApp — uma por bot ligado.
//
// A lista de bots é da API, e este laço só a persegue: bot que apareceu, conecta; bot que foi
// desligado ou excluído, encerra (sem deslogar: a sessão continua válida para quando ele voltar).
// É o mesmo princípio dos outros laços da plataforma — o estado desejado mora no banco, e o
// processo converge para ele.
//
// Por que um processo e não um container por bot: cada sessão do Baileys custa memória, mas um
// container por bot custa muito mais, e a VM tem 2 GB. O teto (MAX_SESSOES) existe para o
// serviço recusar a quinta sessão com mensagem clara em vez de morrer sem memória no meio de uma
// conversa — e o painel mostra o bot sem sessão.

const log = logger.child({ mod: 'sessoes' })

type Fabrica = (options: {
  bot: string
  nome: string
  aoReceber: (m: MensagemRecebida) => void | Promise<void>
}) => Sessao

export type Gerente = ReturnType<typeof criarGerente>

export function criarGerente(options?: {
  // Injetados nos testes: a fábrica de sessão e quem busca os bots.
  fabrica?: Fabrica
  buscarBots?: () => Promise<plataforma.BotDoWorker[]>
  entregar?: (m: MensagemRecebida) => Promise<unknown>
}) {
  const fabrica = options?.fabrica ?? criarSessao
  const buscarBots = options?.buscarBots ?? plataforma.bots
  const entregar =
    options?.entregar ??
    ((m: MensagemRecebida) => plataforma.receber(m.bot, m.telefone, m.contato, m.texto))

  const sessoes = new Map<string, Sessao>()
  // Bots que a API quer, mas que não couberam no teto. Aparecem no status para a tela explicar.
  let recusados: string[] = []
  let timer: NodeJS.Timeout | undefined
  let parado = false
  let cicloAtual: Promise<void> | undefined

  async function aoReceber(m: MensagemRecebida) {
    try {
      const r = await entregar(m)
      const res = r as plataforma.ResultadoDaMensagem | null
      log.info(
        {
          bot: m.bot,
          de: mascarar(m.telefone),
          conversa: res?.conversa_id,
          status: res?.status,
          encerrada: res?.encerrada,
        },
        'mensagem entregue à plataforma',
      )
    } catch (err) {
      // A plataforma é a única fonte de verdade: se ela não aceitou, não há resposta a dar. O
      // cliente vai ficar sem resposta e isso tem de aparecer no log (e no vigia, depois).
      log.error({ err, bot: m.bot, de: mascarar(m.telefone) }, 'a plataforma não aceitou a mensagem recebida')
    }
  }

  /** Aproxima as sessões abertas da lista de bots que a API quer. */
  async function reconciliar(): Promise<void> {
    if (parado) return
    const desejados = await buscarBots()
    const ligados = desejados.filter((b) => b.ativo)

    // Dentro do teto, na ordem em que a API devolveu (nome): previsível, e não depende de sorte.
    const cabem = ligados.slice(0, env.MAX_SESSOES)
    const sobraram = ligados.slice(env.MAX_SESSOES).map((b) => b.slug)
    if (sobraram.length > 0 && JSON.stringify(sobraram) !== JSON.stringify(recusados)) {
      log.error({ teto: env.MAX_SESSOES, semSessao: sobraram },
        'mais bots ligados do que o teto de sessões: estes ficam sem WhatsApp')
    }
    recusados = sobraram

    const queremos = new Set(cabem.map((b) => b.slug))
    // Sessões que não devem mais existir.
    for (const [slug, sessao] of sessoes) {
      if (queremos.has(slug)) continue
      log.warn({ bot: slug }, 'bot não está mais na lista — encerrando a sessão (sem deslogar)')
      sessoes.delete(slug)
      await sessao.encerrar()
    }
    // Sessões novas.
    for (const bot of cabem) {
      if (sessoes.has(bot.slug)) continue
      log.info({ bot: bot.slug, nome: bot.nome }, 'bot novo — abrindo sessão')
      const sessao = fabrica({ bot: bot.slug, nome: bot.nome, aoReceber })
      sessoes.set(bot.slug, sessao)
      // Conectar não bloqueia o laço: a sessão cuida do próprio ciclo de reconexão.
      void sessao.conectar().catch((err) => log.error({ err, bot: bot.slug }, 'falha na conexão inicial'))
    }
  }

  function iniciar(): void {
    const ciclo = () => {
      cicloAtual = reconciliar()
        .catch((err) => log.error({ err }, 'falha ao reconciliar as sessões'))
        .finally(() => {
          cicloAtual = undefined
          if (!parado) timer = setTimeout(ciclo, env.RECONCILIAR_SEGUNDOS * 1_000)
        })
    }
    ciclo()
  }

  async function parar(): Promise<void> {
    parado = true
    if (timer) clearTimeout(timer)
    await cicloAtual?.catch(() => {})
    await Promise.all([...sessoes.values()].map((s) => s.encerrar()))
    sessoes.clear()
  }

  return {
    iniciar,
    parar,
    reconciliar,
    de: (slug: string) => sessoes.get(slug),
    /** Slugs com sessão aberta: é o que a fila pergunta antes de pedir trabalho. */
    conectados: () => [...sessoes.values()].filter((s) => s.conectada()).map((s) => s.bot),
    status: () =>
      [...sessoes.values()].map((s) => ({
        bot: s.bot,
        estado: s.estado(),
        conectado: s.conectada(),
        numero: s.numero() ?? null,
        qr: s.qr() ?? null,
        aberta_ha_ms: s.abertaHaMs() ?? null,
        ultimo_erro: s.ultimoErro() ?? null,
      })),
    semSessao: () => [...recusados],
  }
}
