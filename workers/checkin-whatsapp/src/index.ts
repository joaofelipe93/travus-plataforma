import { env } from './config/env.js'
import { logger } from './logger.js'
import { closeDb } from './db/index.js'
import { stats } from './db/outbox.js'
import { grupoDestino } from './db/configuracao.js'
import { buildServer } from './server.js'
import { connect, disconnect, getStatus } from './whatsapp/client.js'
import { startOutboxWorker, stopOutboxWorker } from './whatsapp/outbox.js'
import { preencherReservas, startResumoDiario, stopResumoDiario } from './resumos/agendador.js'

/**
 * Em modo `pretty` o pino-pretty roda numa worker thread: um `process.exit()`
 * imediato engole justamente as últimas linhas — as que explicam a queda.
 */
function sairApos(code: number) {
  logger.flush()
  setTimeout(() => process.exit(code), 150)
}

/**
 * Erro fora de qualquer try/catch derruba o processo. Sem isto o pm2 reinicia e
 * a única pista fica na saída crua do Node, sem timestamp nem contexto.
 */
function installCrashHandlers() {
  process.on('uncaughtException', (err) => {
    logger.fatal({ err }, 'exceção não tratada — encerrando para o pm2 reiniciar')
    sairApos(1)
  })

  process.on('unhandledRejection', (reason) => {
    logger.fatal({ err: reason }, 'promise rejeitada sem catch — encerrando para o pm2 reiniciar')
    sairApos(1)
  })

  // pm2 avisa antes de matar por max_memory_restart; registrar ajuda a
  // diferenciar reinício por memória de queda por erro.
  process.on('warning', (warning) => {
    logger.warn({ name: warning.name, msg: warning.message }, 'warning do Node')
  })
}

async function main() {
  installCrashHandlers()

  logger.info(
    {
      node: process.version,
      env: process.env.NODE_ENV ?? 'development',
      logLevel: env.LOG_LEVEL,
      logFormat: env.LOG_FORMAT,
      port: env.PORT,
      host: env.HOST,
      authDir: env.AUTH_DIR,
      groupJidAmbiente: env.WHATSAPP_GROUP_JID ?? null,
    },
    'iniciando notificador de check-in',
  )

  // Antes do webhook e do primeiro resumo (ver preencherReservas). Banco fora do ar derruba o
  // boot, e o container sobe de novo.
  await preencherReservas()

  const app = buildServer()

  // A conexão do WhatsApp não bloqueia o boot: o HTTP precisa aceitar webhooks
  // mesmo antes do pareamento — o outbox segura as mensagens até conectar.
  connect().catch((err) => logger.error({ err }, 'falha na conexão inicial do WhatsApp'))

  startOutboxWorker()
  startResumoDiario()

  await app.listen({ port: env.PORT, host: env.HOST })

  // Fila que sobrou do processo anterior: sem isto um restart esconde que
  // existem mensagens paradas esperando conexão.
  const pendentes = Object.fromEntries((await stats()).map((s) => [s.status, s.count]))
  logger.info({ port: env.PORT, outbox: pendentes }, 'servidor pronto')

  const destino = await grupoDestino()
  if (destino) {
    logger.info({ grupo: destino.jid, nome: destino.nome, origem: destino.origem }, 'grupo de destino')
  } else {
    logger.warn(
      'nenhum grupo do WhatsApp escolhido — eventos serão salvos mas não enviados. ' +
        'Conecte e escolha o grupo na tela WhatsApp da plataforma.',
    )
  }

  let shuttingDown = false
  const shutdown = async (signal: string) => {
    if (shuttingDown) {
      logger.warn({ signal }, 'sinal repetido durante o encerramento — ignorado')
      return
    }
    shuttingDown = true

    const startedAt = Date.now()
    logger.info({ signal, whatsapp: getStatus() }, 'encerrando')

    try {
      // Espera o ciclo em andamento: fechar o banco no meio dele perderia o registro do envio.
      await stopResumoDiario()
      await stopOutboxWorker()
      await app.close()
      await disconnect()
      await closeDb()
      logger.info({ signal, ms: Date.now() - startedAt }, 'encerrado com sucesso')
      sairApos(0)
    } catch (err) {
      // Sai mesmo assim: travar aqui faria o pm2 esperar o kill_timeout inteiro.
      logger.error({ err, ms: Date.now() - startedAt }, 'falha ao encerrar limpo')
      sairApos(1)
    }
  }

  // SIGINT/SIGTERM: `pm2 stop`/`pm2 restart`. SIGHUP chega em `pm2 reload`.
  process.on('SIGTERM', () => void shutdown('SIGTERM'))
  process.on('SIGINT', () => void shutdown('SIGINT'))
  process.on('SIGHUP', () => void shutdown('SIGHUP'))
}

main().catch((err) => {
  logger.fatal({ err }, 'falha fatal no boot')
  sairApos(1)
})
