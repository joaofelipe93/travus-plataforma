import { getSocket, isConnected } from './client.js'

export class NaoConectadoError extends Error {
  constructor() {
    super('WhatsApp não está conectado')
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

/** Telefone em log sai mascarado: é dado de cliente. */
export function mascarar(telefone: string): string {
  const d = telefone.replace(/\D/g, '')
  return d.length <= 6 ? '***' : `${d.slice(0, 4)}***${d.slice(-2)}`
}

export function jidDe(telefone: string): string {
  return `${telefone.replace(/\D/g, '')}@s.whatsapp.net`
}

/**
 * Confere se o número existe no WhatsApp antes de mandar. Vale a chamada: sem ela, uma
 * mensagem para um número errado "sai" sem erro e ninguém recebe nada — e o cliente fica sem o
 * boleto sem ninguém saber. O resultado é guardado por execução (a lista de clientes repete
 * pouco, mas a cobrança repete o mesmo número da rodada de envio).
 */
const cacheExistencia = new Map<string, boolean>()

export async function existeNoWhatsapp(telefone: string): Promise<boolean> {
  if (!isConnected()) throw new NaoConectadoError()
  const digitos = telefone.replace(/\D/g, '')
  const guardado = cacheExistencia.get(digitos)
  if (guardado !== undefined) return guardado
  const resultado = await getSocket().onWhatsApp(digitos)
  const existe = Array.isArray(resultado) && resultado.some((r) => r?.exists)
  cacheExistencia.set(digitos, existe)
  return existe
}

/** Esquece o que foi conferido (usado nos testes e ao reiniciar a sessão). */
export function limparCacheDeExistencia(): void {
  cacheExistencia.clear()
}

export async function enviarTexto(telefone: string, texto: string): Promise<string | undefined> {
  if (!isConnected()) throw new NaoConectadoError()
  if (!(await existeNoWhatsapp(telefone))) throw new SemWhatsappError(telefone)
  const r = await getSocket().sendMessage(jidDe(telefone), { text: texto })
  return r?.key?.id ?? undefined
}

/**
 * Manda o boleto como documento, com o texto na legenda. Uma mensagem só: o cliente recebe o
 * PDF e a explicação juntos, e não dá para o anexo chegar sem o texto (ou o contrário).
 */
export async function enviarDocumento(
  telefone: string,
  texto: string,
  anexo: Buffer,
  nome: string,
): Promise<string | undefined> {
  if (!isConnected()) throw new NaoConectadoError()
  if (!(await existeNoWhatsapp(telefone))) throw new SemWhatsappError(telefone)
  const r = await getSocket().sendMessage(jidDe(telefone), {
    document: anexo,
    mimetype: 'application/pdf',
    fileName: nome,
    caption: texto,
  })
  return r?.key?.id ?? undefined
}
