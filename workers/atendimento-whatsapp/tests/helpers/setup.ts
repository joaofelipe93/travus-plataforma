/**
 * Precisa ser o PRIMEIRO import de qualquer teste que toque em `src/`: `config/env.ts` valida o
 * ambiente e chama `process.exit(1)` no momento do import.
 *
 * **Nenhum teste deste serviço abre socket, fala com o WhatsApp ou chama a API da plataforma**:
 * o gerente de sessões recebe a fábrica de sessão por injeção e o laço da fila recebe a API, e
 * os dublês registram o que teria acontecido.
 */
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

export const TOKEN_ADMIN_TESTE = 'token-de-administracao-de-teste-0123456789'

const dir = mkdtempSync(join(tmpdir(), 'atendimento-wa-test-'))

process.env.ADMIN_TOKEN = TOKEN_ADMIN_TESTE
process.env.API_URL = 'http://api-de-teste.invalid:8081'
process.env.WORKER_TOKEN = 'token-de-worker-de-teste'
process.env.AUTH_DIR = join(dir, 'sessoes')
process.env.LOG_LEVEL = 'fatal'
// Fora de produção o logger usa o pino-pretty, que sobe uma worker thread e segura o processo.
process.env.NODE_ENV = 'production'
process.env.ENVIO_HABILITADO = 'true'
process.env.LOTE = '5'
process.env.MAX_SESSOES = '3'

process.on('exit', () => rmSync(dir, { recursive: true, force: true }))
