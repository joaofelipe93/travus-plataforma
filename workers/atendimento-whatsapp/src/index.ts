import { env } from './config/env.js'
import { logger } from './logger.js'
import { buildServer } from './server.js'
import { criarGerente } from './whatsapp/sessoes.js'
import { criarLacoDaFila } from './fila.js'
import { conferirApi } from './plataforma.js'

/**
 * Serviço que conecta o WhatsApp dos bots de atendimento (issue #100, etapa D).
 *
 * **Um processo, N sessões** — uma por bot ligado, cada uma com a própria pasta de credenciais.
 * É o único dos três serviços de WhatsApp da plataforma que faz isso; o notificador e os boletos
 * têm um número cada. O que não muda é a regra: **duas conexões na mesma sessão derrubam uma à
 * outra**, então nunca rode dois destes containers apontando para o mesmo AUTH_DIR.
 *
 * Ele é só o carteiro. Não sabe o que é etapa, teto, ensaio ou instrução de bot: entrega à API o
 * que chega, pega da API o que há para enviar e informa o resultado. A chave da Anthropic não
 * passa por aqui, e não há acesso ao Postgres.
 */

/**
 * Em modo `pretty` o pino-pretty roda numa worker thread: um `process.exit()` imediato engole
 * justamente as últimas linhas — as que explicam a queda.
 */
function sairApos(code: number) {
  logger.flush()
  setTimeout(() => process.exit(code), 150)
}

function instalarHandlersDeQueda() {
  process.on('uncaughtException', (err) => {
    logger.fatal({ err }, 'exceção não tratada — encerrando para o container reiniciar')
    sairApos(1)
  })
  process.on('unhandledRejection', (reason) => {
    logger.fatal({ err: reason }, 'promise rejeitada sem catch — encerrando para o container reiniciar')
    sairApos(1)
  })
  process.on('warning', (warning) => {
    logger.warn({ name: warning.name, msg: warning.message }, 'warning do Node')
  })
}

async function main() {
  instalarHandlersDeQueda()

  logger.info(
    {
      node: process.version,
      env: process.env.NODE_ENV ?? 'development',
      logLevel: env.LOG_LEVEL,
      port: env.PORT,
      authDir: env.AUTH_DIR,
      api: env.API_URL,
      envioHabilitado: env.ENVIO_HABILITADO,
      maxSessoes: env.MAX_SESSOES,
      lote: env.LOTE,
      reconciliarSegundos: env.RECONCILIAR_SEGUNDOS,
    },
    'iniciando o serviço de atendimento no WhatsApp',
  )

  // Sem a API não há o que fazer: melhor morrer no boot (o container reinicia) do que abrir
  // sessões de WhatsApp sem ter a quem entregar as mensagens.
  await conferirApi()

  const gerente = criarGerente()
  const fila = criarLacoDaFila(gerente)
  const app = buildServer(gerente)

  // As conexões não bloqueiam o boot: o HTTP precisa responder (status, QR) antes do pareamento.
  gerente.iniciar()
  fila.iniciar()
  await app.listen({ port: env.PORT, host: env.HOST })

  logger.info({ port: env.PORT }, 'servidor pronto')
  if (!env.ENVIO_HABILITADO) {
    logger.warn('ENVIO_HABILITADO=false neste serviço: nenhuma sessão abre e nada é enviado (freio de mão local)')
  }

  let encerrando = false
  const encerrar = async (sinal: string) => {
    if (encerrando) {
      logger.warn({ sinal }, 'sinal repetido durante o encerramento — ignorado')
      return
    }
    encerrando = true
    const inicio = Date.now()
    logger.info({ sinal, conectadas: gerente.conectados().length }, 'encerrando')
    try {
      // Ordem importa: para a fila primeiro (termina a mensagem em curso), depois o HTTP, depois
      // as sessões. Fechar o socket no meio de um envio faria a mensagem sair duas vezes depois.
      await fila.parar()
      await app.close()
      await gerente.parar()
      logger.info({ sinal, ms: Date.now() - inicio }, 'encerrado com sucesso')
      sairApos(0)
    } catch (err) {
      logger.error({ err, ms: Date.now() - inicio }, 'falha ao encerrar limpo')
      sairApos(1)
    }
  }

  process.on('SIGTERM', () => void encerrar('SIGTERM'))
  process.on('SIGINT', () => void encerrar('SIGINT'))
  process.on('SIGHUP', () => void encerrar('SIGHUP'))
}

main().catch((err) => {
  logger.fatal({ err }, 'falha fatal no boot')
  sairApos(1)
})
