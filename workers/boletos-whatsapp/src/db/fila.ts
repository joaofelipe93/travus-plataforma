import { pool } from './index.js'

/**
 * A fila de mensagens dos boletos (`boletos.mensagens`, migração 00017). Quem **põe** na fila é
 * a API, quando uma rodada termina; este serviço só envia e registra o resultado.
 *
 * Diferença importante em relação à fila do notificador de check-in: aqui o destino é o
 * **privado de um cliente**, não um grupo. Número que não existe no WhatsApp é falha
 * **definitiva** (não adianta tentar seis vezes), e o anexo sai do banco assim que a mensagem
 * é enviada — a cópia só existe enquanto ela está pendente.
 */

export const MAX_TENTATIVAS = 5

export type StatusMensagem = 'pendente' | 'enviada' | 'falhou' | 'cancelada'

export type MensagemFila = {
  id: number
  rodada_id: number | null
  tipo: 'envio' | 'cobranca' | 'teste'
  parcela_id: number | null
  destino_telefone: string
  destino_nome: string
  texto: string
  anexo_pdf: Buffer | null
  anexo_nome: string | null
  status: StatusMensagem
  tentativas: number
  proxima_tentativa_em: Date
  criada_em: Date
}

/** Pausa entre mensagens: é o que protege o número de ser bloqueado pela Meta. */
export type ConfiguracaoEnvio = {
  envio_habilitado: boolean
  pausa_envio_segundos: number
}

export async function configuracaoEnvio(): Promise<ConfiguracaoEnvio> {
  const { rows } = await pool.query<ConfiguracaoEnvio>(
    `SELECT envio_habilitado, pausa_envio_segundos FROM boletos.configuracao WHERE id`,
  )
  // Sem linha (banco recém-criado), o seguro é não enviar.
  return rows[0] ?? { envio_habilitado: false, pausa_envio_segundos: 20 }
}

/**
 * Pendentes na hora de envio, em ordem. Sem trava: existe um processo só (uma sessão do
 * Baileys), e o laço não sobrepõe ciclos.
 *
 * `apenasTeste` é o modo ensaio: nenhum boleto sai, mas a mensagem de teste continua saindo —
 * ela é o que responde "este número está pareado?", e vai para um telefone que o admin digitou.
 */
export async function pendentes(limite: number, apenasTeste = false): Promise<MensagemFila[]> {
  const { rows } = await pool.query<MensagemFila>(
    `SELECT id, rodada_id, tipo, parcela_id, destino_telefone, destino_nome, texto,
            anexo_pdf, anexo_nome, status, tentativas, proxima_tentativa_em, criada_em
     FROM boletos.mensagens
     WHERE status = 'pendente' AND proxima_tentativa_em <= now()
       AND ($2::boolean IS NOT TRUE OR tipo = 'teste')
     ORDER BY id ASC
     LIMIT $1`,
    [limite, apenasTeste],
  )
  return rows
}

/** Enviada: o anexo sai do banco (a cópia na fila só existe enquanto está pendente). */
export async function marcarEnviada(id: number): Promise<void> {
  await pool.query(
    `UPDATE boletos.mensagens
     SET status = 'enviada', tentativas = tentativas + 1, enviada_em = now(),
         ultimo_erro = NULL, anexo_pdf = NULL
     WHERE id = $1`,
    [id],
  )
}

/**
 * Registra a falha. Enquanto houver tentativas sobrando, reagenda com espera crescente
 * (30s, 90s, 270s...); esgotadas — ou sendo falha definitiva —, marca como `falhou` e solta o
 * anexo (ninguém mais vai mandar esta mensagem).
 */
export async function marcarFalha(
  m: MensagemFila,
  erro: string,
  { definitiva = false }: { definitiva?: boolean } = {},
) {
  const tentativas = m.tentativas + 1
  if (definitiva || tentativas >= MAX_TENTATIVAS) {
    await pool.query(
      `UPDATE boletos.mensagens
       SET status = 'falhou', tentativas = tentativas + 1, ultimo_erro = $2, anexo_pdf = NULL
       WHERE id = $1`,
      [m.id, erro],
    )
    return { retentando: false as const, tentativas }
  }
  const esperaMs = 30_000 * 3 ** m.tentativas
  const { rows } = await pool.query<{ proxima_tentativa_em: Date }>(
    `UPDATE boletos.mensagens
     SET tentativas = tentativas + 1,
         proxima_tentativa_em = now() + make_interval(secs => $2::double precision / 1000),
         ultimo_erro = $3
     WHERE id = $1
     RETURNING proxima_tentativa_em`,
    [m.id, esperaMs, erro],
  )
  return { retentando: true as const, tentativas, proxima: rows[0]?.proxima_tentativa_em }
}

export async function contarPendentes(): Promise<number> {
  const { rows } = await pool.query<{ n: number }>(
    `SELECT count(*) AS n FROM boletos.mensagens WHERE status = 'pendente'`,
  )
  return rows[0]?.n ?? 0
}

export async function situacaoDaFila(): Promise<{ status: StatusMensagem; count: number }[]> {
  const { rows } = await pool.query<{ status: StatusMensagem; count: number }>(
    `SELECT status, count(*) AS count FROM boletos.mensagens GROUP BY status ORDER BY status`,
  )
  return rows
}

/** Mensagem de teste pedida pelo admin na tela: entra na mesma fila, com tipo 'teste'. */
export async function enfileirarTeste(telefone: string, texto: string): Promise<number> {
  const { rows } = await pool.query<{ id: number }>(
    `INSERT INTO boletos.mensagens (tipo, destino_telefone, destino_nome, texto)
     VALUES ('teste', $1, 'Teste', $2)
     RETURNING id`,
    [telefone, texto],
  )
  return rows[0]!.id
}
