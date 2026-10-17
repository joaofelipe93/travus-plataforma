import { env } from './config/env.js'
import { logger } from './logger.js'
import { closeDb } from './db/index.js'
import { configuracaoEnvio, situacaoDaFila } from './db/fila.js'
import { buildServer } from './server.js'
import { connect, disconnect, getStatus } from './whatsapp/client.js'
import { iniciarFila, pararFila } from './whatsapp/fila.js'

/**
 * Serviço que manda os boletos da Canopus no WhatsApp **de cada cliente** (issue #48).
 *
 * É um serviço separado do notificador de check-in de propósito: **outro número, outra
 * sessão**. Duas conexões na mesma sessão do Baileys derrubam uma à outra, e os dois serviços
 * têm laços de fila próprios — misturar faria mensagem sair duas vezes.
 *
 * Quem decide o que enviar é a API (ela põe na fila só o que está liberado). Este processo é o
 * carteiro: pega o que está pendente, respeita a pausa entre mensagens e registra o resultado.
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
      host: env.HOST,
      authDir: env.AUTH_DIR,
      envioHabilitado: env.ENVIO_HABILITADO,
      lote: env.LOTE,
    },
    'iniciando o serviço de boletos no WhatsApp',
  )

  const app = buildServer()

  // A conexão do WhatsApp não bloqueia o boot: o HTTP precisa responder (status, QR) antes do
  // pareamento, e a fila segura as mensagens até conectar.
  connect().catch((err) => logger.error({ err }, 'falha na conexão inicial do WhatsApp'))

  iniciarFila()
  await app.listen({ port: env.PORT, host: env.HOST })

  // Fila que sobrou do processo anterior: sem isto um restart esconde que há boletos parados.
  const fila = Object.fromEntries((await situacaoDaFila()).map((s) => [s.status, s.count]))
  const cfg = await configuracaoEnvio()
  logger.info({ port: env.PORT, fila, envioHabilitadoConfiguracao: cfg.envio_habilitado }, 'servidor pronto')
  if (!env.ENVIO_HABILITADO) {
    logger.warn('ENVIO_HABILITADO=false neste serviço: nada será enviado (freio de mão local)')
  }
  if (!cfg.envio_habilitado) {
    logger.warn('envio desligado na configuração da plataforma: a fila espera até um admin ligar')
  }

  let encerrando = false
  const encerrar = async (sinal: string) => {
    if (encerrando) {
      logger.warn({ sinal }, 'sinal repetido durante o encerramento — ignorado')
      return
    }
    encerrando = true
    const inicio = Date.now()
    logger.info({ sinal, whatsapp: getStatus() }, 'encerrando')
    try {
      // Espera o ciclo em andamento: fechar o banco no meio dele perderia o registro do envio
      // (e a mensagem sairia duas vezes depois).
      await pararFila()
      await app.close()
      await disconnect()
      await closeDb()
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
