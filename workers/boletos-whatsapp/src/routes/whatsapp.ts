import type { FastifyInstance } from 'fastify'
import { z } from 'zod'
import { configuracaoEnvio, enfileirarTeste, situacaoDaFila } from '../db/fila.js'
import { getConta, getQr, getStatus, isConnected, reiniciarSessao } from '../whatsapp/client.js'
import { limparCacheDeExistencia, mascarar } from '../whatsapp/sender.js'
import { env } from '../config/env.js'
import { exigirTokenAdmin } from './auth.js'

// Telefone do teste: só dígitos, com DDI (a API manda já normalizado).
const corpoTeste = z.object({
  telefone: z.string().regex(/^[0-9]{12,15}$/, 'telefone precisa vir só com dígitos, com DDI'),
})

const corpoDesconectar = z.object({ confirmar: z.literal(true) })

/**
 * Administração do WhatsApp **dos boletos**. Sem rota no gateway: quem chama é a API da
 * plataforma (tela de boletos, só admin), pela rede interna, com o ADMIN_TOKEN.
 *
 * Não há escolha de grupo aqui — o destino de cada mensagem é o telefone do cliente, que vem do
 * cadastro. O que o admin faz nesta tela é parear o número, conferir a fila e testar.
 */
export async function rotasWhatsapp(app: FastifyInstance) {
  app.addHook('preHandler', exigirTokenAdmin)

  app.get('/whatsapp/status', async () => {
    const [fila, cfg] = await Promise.all([situacaoDaFila(), configuracaoEnvio()])
    return {
      status: getStatus(),
      connected: isConnected(),
      // Número pareado, com a conexão aberta.
      account: getConta() ?? null,
      // String bruta do QR (a tela desenha a imagem). Troca a cada ~20s enquanto não pareia.
      qr: getQr() ?? null,
      // Os dois freios: o do serviço (ambiente) e o da configuração (tela).
      envio_habilitado_servico: env.ENVIO_HABILITADO,
      envio_habilitado_configuracao: cfg.envio_habilitado,
      pausa_envio_segundos: cfg.pausa_envio_segundos,
      fila: Object.fromEntries(fila.map((s) => [s.status, s.count])),
    }
  })

  /**
   * Mensagem de teste. Vai para o número que o admin digitar — **entra na mesma fila**, com
   * tipo 'teste': o envio sai pelo mesmo caminho de sempre (e respeita a pausa e o freio),
   * em vez de um atalho que só funciona no teste.
   */
  app.post('/whatsapp/teste', async (req, reply) => {
    const corpo = corpoTeste.safeParse(req.body)
    if (!corpo.success) {
      return reply.code(400).send({ error: 'telefone_invalido', detalhe: corpo.error.issues[0]?.message })
    }
    const texto =
      'Mensagem de teste da Travus Plataforma (serviço de boletos). ' +
      'Se você recebeu isto, o número está pareado corretamente. Nenhuma ação é necessária.'
    const id = await enfileirarTeste(corpo.data.telefone, texto)
    req.log.info({ mensagem: id, destino: mascarar(corpo.data.telefone) }, 'mensagem de teste enfileirada')
    return reply.code(202).send({ enfileirada: id, conectado: isConnected() })
  })

  /** Desvincula o aparelho, apaga a sessão e gera QR novo. A fila espera. */
  app.post('/whatsapp/desconectar', async (req, reply) => {
    const corpo = corpoDesconectar.safeParse(req.body)
    if (!corpo.success) {
      return reply.code(400).send({ error: 'confirmacao_obrigatoria' })
    }
    limparCacheDeExistencia()
    await reiniciarSessao()
    req.log.warn('sessão do WhatsApp dos boletos apagada a pedido do admin')
    return { status: getStatus() }
  })
}
