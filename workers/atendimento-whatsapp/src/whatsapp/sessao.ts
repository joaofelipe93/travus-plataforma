import {
  makeWASocket,
  useMultiFileAuthState,
  fetchLatestBaileysVersion,
  makeCacheableSignalKeyStore,
  DisconnectReason,
  Browsers,
  type WASocket,
  type proto,
} from 'baileys'
import { Boom } from '@hapi/boom'
import qrcode from 'qrcode-terminal'
import { join } from 'node:path'
import { rm } from 'node:fs/promises'
import { env } from '../config/env.js'
import { logger } from '../logger.js'

// Uma sessão do WhatsApp = um bot. Diferente dos outros dois serviços de WhatsApp da plataforma
// (notificador e boletos), aqui o estado NÃO é de módulo: são N sessões no mesmo processo, então
// cada uma é um objeto com a própria pasta de credenciais, o próprio socket e o próprio ciclo de
// reconexão. Misturar isso seria o jeito mais rápido de um bot responder pelo número do outro.
//
// O que esta camada faz: abre o socket, mantém o estado legível (para a tela) e entrega as
// mensagens que chegam a quem assinar `aoReceber`. Ela não sabe o que é conversa, etapa ou fila.

export type EstadoSessao = 'desconectada' | 'conectando' | 'qr' | 'aberta'

export type MensagemRecebida = {
  bot: string
  telefone: string
  contato?: string
  texto: string
}

export type Sessao = ReturnType<typeof criarSessao>

const esperar = (ms: number) => new Promise((r) => setTimeout(r, ms))

/** Nome do código de desconexão do Baileys — 428 sozinho não diz nada no log. */
function nomeDoMotivo(code: number | undefined): string {
  if (code === undefined) return 'desconhecido'
  return Object.entries(DisconnectReason).find(([, v]) => v === code)?.[0] ?? 'desconhecido'
}

/** Texto de uma mensagem recebida, nos formatos que interessam (texto puro e legenda). */
export function textoDaMensagem(msg: proto.IWebMessageInfo): string | undefined {
  const m = msg.message
  if (!m) return undefined
  const bruto =
    m.conversation ??
    m.extendedTextMessage?.text ??
    m.imageMessage?.caption ??
    m.videoMessage?.caption ??
    m.documentMessage?.caption ??
    undefined
  const texto = bruto?.trim()
  return texto ? texto : undefined
}

/**
 * Telefone de um jid de conversa privada. Devolve undefined para grupo, status, canal e lista de
 * transmissão: **o bot só responde conversa de uma pessoa com ele**. Responder num grupo seria
 * conversa de robô com robô na frente de todo mundo.
 */
export function telefoneDoJid(jid: string | null | undefined): string | undefined {
  if (!jid || !jid.endsWith('@s.whatsapp.net')) return undefined
  const digitos = jid.split('@')[0]?.split(':')[0]?.replace(/\D/g, '')
  return digitos && digitos.length >= 8 ? digitos : undefined
}

/** Telefone em log sai mascarado: é dado de cliente. */
export function mascarar(telefone: string): string {
  const d = telefone.replace(/\D/g, '')
  return d.length <= 6 ? '***' : `${d.slice(0, 4)}***${d.slice(-2)}`
}

export function jidDe(telefone: string): string {
  return `${telefone.replace(/\D/g, '')}@s.whatsapp.net`
}

export class NaoConectadoError extends Error {
  constructor(bot: string) {
    super(`o WhatsApp do bot ${bot} não está conectado`)
    this.name = 'NaoConectadoError'
  }
}

/** Número sem WhatsApp: não adianta tentar de novo, é falha definitiva. */
export class SemWhatsappError extends Error {
  constructor(telefone: string) {
    super(`o número ${mascarar(telefone)} não tem WhatsApp`)
    this.name = 'SemWhatsappError'
  }
}

export function criarSessao(options: {
  bot: string
  nome: string
  aoReceber: (m: MensagemRecebida) => void | Promise<void>
}) {
  const { bot, nome, aoReceber } = options
  const log = logger.child({ mod: 'sessao', bot })
  // Baileys é muito verboso em info; mantemos o socket em 'warn' salvo em debug.
  const baileysLogger = logger.child(
    { mod: 'baileys', bot },
    { level: env.LOG_LEVEL === 'trace' || env.LOG_LEVEL === 'debug' ? env.LOG_LEVEL : 'warn' },
  )
  // Uma pasta por bot: é o que mantém as sessões independentes.
  const pasta = join(env.AUTH_DIR, bot)

  let sock: WASocket | undefined
  let estado: EstadoSessao = 'desconectada'
  let qrAtual: string | undefined
  let tentativas = 0
  let timer: NodeJS.Timeout | undefined
  let parada = false
  let abertaEm: number | undefined
  let ultimoErro: string | undefined
  // Muda a cada reiniciarSessao: um connect() que começou antes desiste ao ver outra geração,
  // senão haveria dois sockets na mesma sessão.
  let geracao = 0
  let reiniciando: Promise<void> | undefined
  const existeNoWhatsapp = new Map<string, boolean>()

  function agendarReconexao() {
    if (parada) return
    // 2s, 4s, 8s… teto de 60s, para não martelar o servidor do WhatsApp.
    const atraso = Math.min(2_000 * 2 ** tentativas, 60_000)
    tentativas += 1
    const nivel = tentativas > 5 ? 'warn' : 'info'
    log[nivel]({ atrasoMs: atraso, tentativa: tentativas }, 'reagendando reconexão')
    timer = setTimeout(() => {
      conectar().catch((err) => {
        log.error({ err }, 'falha ao reconectar')
        agendarReconexao()
      })
    }, atraso)
  }

  async function conectar(): Promise<void> {
    if (parada || reiniciando) return
    const minhaGeracao = geracao
    estado = 'conectando'

    const { state, saveCreds } = await useMultiFileAuthState(pasta)
    const { version } = await fetchLatestBaileysVersion()
    if (parada || minhaGeracao !== geracao) return
    log.info({ waVersion: version.join('.'), pasta }, 'abrindo socket')

    const este = makeWASocket({
      version,
      auth: {
        creds: state.creds,
        keys: makeCacheableSignalKeyStore(state.keys, baileysLogger),
      },
      logger: baileysLogger,
      // Nome em "Dispositivos conectados" no celular. Leva o nome do bot: são vários números, e
      // é por aqui que se sabe qual aparelho é qual.
      browser: Browsers.ubuntu(`Travus ${nome}`.slice(0, 30)),
      markOnlineOnConnect: false,
      syncFullHistory: false,
    })
    sock = este

    // Eventos de um socket já substituído são ignorados: senão o fechamento dele agendaria outra
    // reconexão e as credenciais antigas seriam regravadas na pasta recém-apagada.
    este.ev.on('creds.update', () => {
      if (este === sock) void saveCreds()
    })

    este.ev.on('connection.update', (update) => {
      if (este !== sock) return
      const { connection, lastDisconnect, qr } = update

      if (qr) {
        qrAtual = qr
        estado = 'qr'
        log.warn({ bot }, 'escaneie o QR com o WhatsApp DESTE bot')
        // Cru no stdout: o QR precisa continuar escaneável no log do container.
        qrcode.generate(qr, { small: true })
      }

      if (connection === 'open') {
        qrAtual = undefined
        estado = 'aberta'
        abertaEm = Date.now()
        ultimoErro = undefined
        log.info({ jid: sock?.user?.id, aposTentativas: tentativas }, 'conexão aberta')
        tentativas = 0
        return
      }

      if (connection === 'close') {
        estado = 'desconectada'
        const codigo = (lastDisconnect?.error as Boom | undefined)?.output?.statusCode
        const durouMs = abertaEm !== undefined ? Date.now() - abertaEm : undefined
        abertaEm = undefined
        ultimoErro = lastDisconnect?.error?.message

        if (parada) {
          log.debug({ durouMs }, 'socket fechado durante o encerramento')
          return
        }
        if (codigo === DisconnectReason.loggedOut) {
          // Sessão invalidada no celular: as credenciais não servem mais.
          log.error({ codigo, motivo: nomeDoMotivo(codigo), durouMs },
            'sessão encerrada no aparelho — limpando credenciais, será preciso novo QR')
          void rm(pasta, { recursive: true, force: true })
            .then(() => {
              tentativas = 0
              agendarReconexao()
            })
            .catch((err) => log.error({ err }, 'falha ao limpar a pasta da sessão'))
          return
        }
        if (codigo === DisconnectReason.restartRequired) {
          log.info({ codigo }, 'restart pedido pelo WhatsApp — reconectando')
          tentativas = 0
          void conectar().catch((err) => {
            log.error({ err }, 'falha no restart')
            agendarReconexao()
          })
          return
        }
        log.warn({ codigo, motivo: nomeDoMotivo(codigo), durouMs, err: ultimoErro }, 'conexão fechada')
        agendarReconexao()
      }
    })

    este.ev.on('messages.upsert', (evento) => {
      if (este !== sock) return
      if (evento.type !== 'notify') return
      for (const msg of evento.messages) {
        // Mensagem que o próprio bot mandou volta no evento: responder a ela seria um laço.
        if (msg.key.fromMe) continue
        const telefone = telefoneDoJid(msg.key.remoteJid)
        if (!telefone) continue // grupo, status, canal: o bot não entra
        const texto = textoDaMensagem(msg)
        if (!texto) {
          // Áudio, figurinha, localização: a plataforma não transcreve nada (ainda). Ignorar em
          // silêncio deixaria a pessoa falando sozinha, mas responder "não entendi" é decisão
          // da API, não do transporte — então registramos e seguimos.
          log.info({ de: mascarar(telefone) }, 'mensagem sem texto ignorada')
          continue
        }
        const contato = msg.pushName ?? undefined
        void Promise.resolve(aoReceber({ bot, telefone, contato, texto })).catch((err) =>
          log.error({ err, de: mascarar(telefone) }, 'falha ao tratar mensagem recebida'),
        )
      }
    })
  }

  function conectada(): boolean {
    return estado === 'aberta' && sock !== undefined
  }

  function socketAberto(): WASocket {
    if (!sock || estado !== 'aberta') throw new NaoConectadoError(bot)
    return sock
  }

  /** Confere se o número existe no WhatsApp: sem isso, a mensagem "sai" e ninguém recebe. */
  async function temWhatsapp(telefone: string): Promise<boolean> {
    const digitos = telefone.replace(/\D/g, '')
    const guardado = existeNoWhatsapp.get(digitos)
    if (guardado !== undefined) return guardado
    const r = await socketAberto().onWhatsApp(digitos)
    const existe = Array.isArray(r) && r.some((x) => x?.exists)
    existeNoWhatsapp.set(digitos, existe)
    return existe
  }

  async function enviarTexto(telefone: string, texto: string): Promise<string | undefined> {
    if (!conectada()) throw new NaoConectadoError(bot)
    if (!(await temWhatsapp(telefone))) throw new SemWhatsappError(telefone)
    const r = await socketAberto().sendMessage(jidDe(telefone), { text: texto })
    return r?.key?.id ?? undefined
  }

  /** "Desconectar e gerar novo QR": desvincula, apaga as credenciais e abre socket novo. */
  function reiniciar(): Promise<void> {
    if (reiniciando) return reiniciando
    if (parada) return Promise.resolve()
    geracao += 1
    const pedido = (async () => {
      const antigo = sock
      const estavaAberta = estado === 'aberta'
      sock = undefined
      estado = 'desconectada'
      qrAtual = undefined
      abertaEm = undefined
      existeNoWhatsapp.clear()
      if (timer) clearTimeout(timer)
      tentativas = 0

      if (antigo && estavaAberta) {
        try {
          await antigo.logout()
        } catch (err) {
          log.warn({ err }, 'falha ao desvincular no WhatsApp; apagando as credenciais mesmo assim')
        }
      }
      try {
        antigo?.end(undefined)
      } catch (err) {
        log.debug({ err }, 'erro ao fechar o socket antigo')
      }
      await rm(pasta, { recursive: true, force: true })
      log.warn({ estavaAberta }, 'sessão apagada a pedido — gerando novo QR')
    })().finally(() => {
      reiniciando = undefined
    })
    reiniciando = pedido
    void pedido
      .then(() => conectar())
      .catch((err) => {
        log.error({ err }, 'falha ao reiniciar a sessão')
        agendarReconexao()
      })
    return pedido
  }

  /** Fecha sem deslogar: a sessão continua válida para a próxima subida. */
  async function encerrar(): Promise<void> {
    parada = true
    if (timer) clearTimeout(timer)
    try {
      sock?.end(undefined)
    } catch (err) {
      log.warn({ err }, 'erro ao fechar socket')
    }
    sock = undefined
    estado = 'desconectada'
    // Dá um instante para o Baileys liberar os arquivos antes de alguém reabrir a pasta.
    await esperar(10)
  }

  return {
    bot,
    nome,
    conectar,
    conectada,
    enviarTexto,
    reiniciar,
    encerrar,
    estado: () => estado,
    qr: () => qrAtual,
    numero: () => (estado === 'aberta' ? sock?.user?.id : undefined),
    abertaHaMs: () => (abertaEm === undefined ? undefined : Date.now() - abertaEm),
    ultimoErro: () => ultimoErro,
  }
}
