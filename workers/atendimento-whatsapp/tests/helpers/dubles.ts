import type { MensagemRecebida } from '../../src/whatsapp/sessao.js'

// Dublê de sessão: tem a mesma forma da de verdade, sem socket nenhum.
export type SessaoFalsa = ReturnType<typeof sessaoFalsa>

export function sessaoFalsa(options: {
  bot: string
  nome: string
  aoReceber: (m: MensagemRecebida) => void | Promise<void>
}) {
  let estado: 'desconectada' | 'conectando' | 'qr' | 'aberta' = 'desconectada'
  const enviados: { telefone: string; texto: string }[] = []
  let erroAoEnviar: Error | undefined
  let encerrada = false
  let reiniciada = 0

  return {
    bot: options.bot,
    nome: options.nome,
    enviados,
    conectar: async () => {
      estado = 'aberta'
    },
    conectada: () => estado === 'aberta',
    enviarTexto: async (telefone: string, texto: string) => {
      if (erroAoEnviar) throw erroAoEnviar
      enviados.push({ telefone, texto })
      return 'id-falso'
    },
    reiniciar: async () => {
      reiniciada += 1
      estado = 'qr'
    },
    encerrar: async () => {
      encerrada = true
      estado = 'desconectada'
    },
    estado: () => estado,
    qr: () => (estado === 'qr' ? 'qr-falso' : undefined),
    numero: () => (estado === 'aberta' ? `55119000000${options.bot.length}:1@s.whatsapp.net` : undefined),
    abertaHaMs: () => (estado === 'aberta' ? 1_000 : undefined),
    ultimoErro: () => undefined,
    // Só para os testes:
    simularMensagem: (m: Omit<MensagemRecebida, 'bot'>) => options.aoReceber({ ...m, bot: options.bot }),
    derrubar: () => {
      estado = 'desconectada'
    },
    falharCom: (err: Error | undefined) => {
      erroAoEnviar = err
    },
    foiEncerrada: () => encerrada,
    vezesReiniciada: () => reiniciada,
  }
}

/** Fábrica que guarda as sessões criadas, para o teste mexer nelas. */
export function fabricaFalsa() {
  const criadas = new Map<string, SessaoFalsa>()
  const fabrica = (options: {
    bot: string
    nome: string
    aoReceber: (m: MensagemRecebida) => void | Promise<void>
  }) => {
    const s = sessaoFalsa(options)
    criadas.set(options.bot, s)
    // O gerente espera o tipo da sessão de verdade; o dublê tem a mesma forma.
    return s as unknown as ReturnType<typeof import('../../src/whatsapp/sessao.js').criarSessao>
  }
  return { fabrica, criadas }
}
