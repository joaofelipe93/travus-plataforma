import './setup.js'
import { pool } from '../../src/db/index.js'
import type { MensagemFila } from '../../src/db/fila.js'

export { pool }

const url = process.env.TEST_DATABASE_URL
if (!url) {
  throw new Error('TEST_DATABASE_URL não definida: os testes do banco usam o Postgres de teste (rode make test na raiz)')
}
if (!new URL(url).pathname.includes('teste')) {
  // Os testes apagam as tabelas: nunca contra o banco de verdade.
  throw new Error('TEST_DATABASE_URL precisa apontar para um banco de teste (nome com "teste")')
}

/** Zera a fila e devolve a configuração aos padrões (com o envio ligado, que é o que se testa). */
export async function limparBanco(): Promise<void> {
  await pool.query('TRUNCATE boletos.mensagens RESTART IDENTITY CASCADE')
  await pool.query(`INSERT INTO boletos.configuracao (id) VALUES (true) ON CONFLICT (id) DO NOTHING`)
  await pool.query(`UPDATE boletos.configuracao SET envio_habilitado = true, pausa_envio_segundos = 5 WHERE id`)
}

export async function desligarEnvio(): Promise<void> {
  await pool.query(`UPDATE boletos.configuracao SET envio_habilitado = false WHERE id`)
}

/** Põe uma mensagem na fila do jeito que a API faria. */
export async function enfileirar(opcoes: {
  telefone?: string
  nome?: string
  texto?: string
  anexo?: Buffer | null
  tipo?: 'envio' | 'cobranca' | 'teste'
}): Promise<number> {
  const { rows } = await pool.query<{ id: number }>(
    `INSERT INTO boletos.mensagens (tipo, destino_telefone, destino_nome, texto, anexo_pdf, anexo_nome)
     VALUES ($1, $2, $3, $4, $5, $6)
     RETURNING id`,
    [
      opcoes.tipo ?? 'envio',
      opcoes.telefone ?? '5511900000000',
      opcoes.nome ?? 'CLIENTE DE TESTE',
      opcoes.texto ?? 'Oi Cliente, chegou o boleto.',
      opcoes.anexo === undefined ? Buffer.from('%PDF-1.4\nficticio\n%%EOF\n') : opcoes.anexo,
      opcoes.anexo === null ? null : 'boleto.pdf',
    ],
  )
  return rows[0]!.id
}

export async function buscar(id: number): Promise<MensagemFila & { ultimo_erro: string | null; enviada_em: Date | null }> {
  const { rows } = await pool.query(`SELECT * FROM boletos.mensagens WHERE id = $1`, [id])
  if (!rows[0]) throw new Error(`mensagem ${id} não encontrada`)
  return rows[0] as MensagemFila & { ultimo_erro: string | null; enviada_em: Date | null }
}

/** Dublê do WhatsApp: registra o que teria sido enviado, sem socket nenhum. */
export function carteiroFalso(opcoes: { conectado?: boolean; falhar?: Error } = {}) {
  const enviados: { telefone: string; texto: string; anexo: number | null }[] = []
  return {
    enviados,
    conectado: () => opcoes.conectado ?? true,
    situacao: () => (opcoes.conectado === false ? 'disconnected' : 'open'),
    async enviarTexto(telefone: string, texto: string) {
      if (opcoes.falhar) throw opcoes.falhar
      enviados.push({ telefone, texto, anexo: null })
      return 'wa-id-texto'
    },
    async enviarDocumento(telefone: string, texto: string, anexo: Buffer) {
      if (opcoes.falhar) throw opcoes.falhar
      enviados.push({ telefone, texto, anexo: anexo.length })
      return 'wa-id-documento'
    },
  }
}
