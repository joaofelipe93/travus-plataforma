import type { FastifyInstance } from 'fastify'
import { z } from 'zod'
import { env } from '../config/env.js'
import { mascarar } from '../whatsapp/sessao.js'
import type { Gerente } from '../whatsapp/sessoes.js'
import { exigirTokenAdmin } from './auth.js'

// Administração dos WhatsApps dos bots. Sem rota no gateway: quem chama é a API da plataforma
// (tela Conexões do Atendimento, só admin), pela rede interna, com o ADMIN_TOKEN.
//
// Tudo aqui é **por bot**, porque são N números: o QR de um bot não serve para outro, e
// desconectar um não encosta nos demais.

const corpoTeste = z.object({
  telefone: z.string().regex(/^[0-9]{12,15}$/, 'telefone precisa vir só com dígitos, com DDI'),
})
const corpoDesconectar = z.object({ confirmar: z.literal(true) })

export function rotasWhatsapp(gerente: Gerente) {
  return async function (app: FastifyInstance) {
    app.addHook('preHandler', exigirTokenAdmin)

    app.get('/whatsapp/status', async () => ({
      max_sessoes: env.MAX_SESSOES,
      // Bots ligados que não couberam no teto: a tela precisa poder explicar o número ausente.
      sem_sessao: gerente.semSessao(),
      sessoes: gerente.status(),
    }))

    /**
     * Mensagem de teste pelo número de um bot. **Sai para um telefone de verdade, mesmo em modo
     * ensaio**: é com ela que se confere o pareamento. Vai direto pelo socket, e não pela fila,
     * porque a fila é das conversas — teste não é conversa e não deve virar histórico de ninguém.
     */
    app.post<{ Params: { slug: string } }>('/whatsapp/:slug/teste', async (req, reply) => {
      const sessao = gerente.de(req.params.slug)
      if (!sessao) return reply.code(409).send({ error: 'sem_sessao' })
      if (!sessao.conectada()) return reply.code(409).send({ error: 'not_connected' })

      const corpo = corpoTeste.safeParse(req.body)
      if (!corpo.success) {
        return reply.code(400).send({ error: 'telefone_invalido', detalhe: corpo.error.issues[0]?.message })
      }
      const texto =
        `Mensagem de teste da Travus Plataforma (atendimento ${sessao.nome}). ` +
        'Se você recebeu isto, o número está pareado corretamente. Nenhuma ação é necessária.'
      try {
        await sessao.enviarTexto(corpo.data.telefone, texto)
      } catch (err) {
        req.log.warn({ err, bot: sessao.bot, destino: mascarar(corpo.data.telefone) },
          'falha na mensagem de teste')
        return reply.code(502).send({ error: 'falha_no_envio', detalhe: err instanceof Error ? err.message : 'erro' })
      }
      req.log.warn({ bot: sessao.bot, destino: mascarar(corpo.data.telefone) },
        'mensagem de teste enviada a pedido do admin')
      return reply.code(202).send({ enviada: true })
    })

    /** Desvincula o aparelho daquele bot, apaga a sessão dele e gera QR novo. A fila espera. */
    app.post<{ Params: { slug: string } }>('/whatsapp/:slug/desconectar', async (req, reply) => {
      const sessao = gerente.de(req.params.slug)
      if (!sessao) return reply.code(409).send({ error: 'sem_sessao' })
      const corpo = corpoDesconectar.safeParse(req.body)
      if (!corpo.success) return reply.code(400).send({ error: 'confirmacao_obrigatoria' })

      await sessao.reiniciar()
      req.log.warn({ bot: sessao.bot }, 'sessão apagada a pedido do admin')
      return { estado: sessao.estado() }
    })
  }
}
