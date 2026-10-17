import { z } from 'zod'
import { existsSync } from 'node:fs'

// Node 24 carrega .env nativamente — sem dotenv. Variáveis já definidas no ambiente
// (compose, systemd) têm precedência e não são sobrescritas.
if (existsSync('.env')) {
  process.loadEnvFile('.env')
}

const schema = z.object({
  PORT: z.coerce.number().int().positive().default(3000),
  HOST: z.string().default('0.0.0.0'),
  LOG_LEVEL: z.enum(['trace', 'debug', 'info', 'warn', 'error', 'fatal', 'silent']).default('info'),
  LOG_FORMAT: z.enum(['pretty', 'json']).default('pretty'),

  // Único token deste serviço: ele não tem webhook. Quem chama é a API da plataforma (tela de
  // boletos, só admin), pela rede interna. Quem tem este token lê o QR — ou seja, pareia o
  // próprio aparelho na conta. **Nunca crie rota no gateway para /whatsapp/***.
  ADMIN_TOKEN: z.string().min(32, 'ADMIN_TOKEN precisa ter ao menos 32 caracteres'),

  // Postgres da plataforma com o papel `boletos` (migração 00017): só a fila de mensagens e a
  // configuração do schema boletos.
  DATABASE_URL: z.string().regex(/^postgres(ql)?:\/\//, 'DATABASE_URL deve ser uma URL postgres://'),

  // Sessão do WhatsApp deste serviço. **Outro número, outra pasta**: nunca aponte para a do
  // notificador de check-in — duas conexões na mesma sessão derrubam uma à outra.
  AUTH_DIR: z.string().default('./data/auth_info'),

  // Trava de segurança do próprio serviço, independente da API: com ela desligada, o laço nem
  // tenta enviar. A decisão de enviar continua sendo da plataforma (que só põe na fila o que
  // está liberado); isto é o freio de mão.
  ENVIO_HABILITADO: z
    .enum(['true', 'false'])
    .default('true')
    .transform((v) => v === 'true'),

  // Quantas mensagens por ciclo. Baixo de propósito: o que protege o número é o ritmo, e a
  // pausa entre mensagens vem da configuração no banco (pausa_envio_segundos).
  LOTE: z.coerce.number().int().min(1).max(20).default(5),
})

function load() {
  const raw = Object.fromEntries(Object.entries(process.env).filter(([, v]) => v !== undefined && v !== ''))
  const result = schema.safeParse(raw)
  if (!result.success) {
    const issues = result.error.issues.map((i) => `  - ${i.path.join('.')}: ${i.message}`).join('\n')
    console.error(`Configuração inválida:\n${issues}\n\nVeja .env.example`)
    process.exit(1)
  }
  return result.data
}

export const env = load()
export type Env = typeof env
