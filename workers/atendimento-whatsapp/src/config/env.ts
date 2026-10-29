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

  // Token da tela (Conexões do Atendimento, só admin), pela rede interna. Quem tem este token
  // **lê o QR** — e com o QR pareia o próprio aparelho na conta de um bot.
  // **Nunca crie rota no gateway para /whatsapp/***.
  ADMIN_TOKEN: z.string().min(32, 'ADMIN_TOKEN precisa ter ao menos 32 caracteres'),

  // A API da plataforma pela rede interna (porta 8081) e o token de serviço. Este worker **não
  // fala com o Postgres**: quem sabe o que é etapa, teto e ensaio é a API (decisão da etapa A).
  API_URL: z.string().regex(/^https?:\/\//, 'API_URL deve ser http:// ou https://'),
  WORKER_TOKEN: z.string().min(16, 'WORKER_TOKEN precisa ter ao menos 16 caracteres'),
  // Nome deste worker no log da plataforma.
  WORKER_NOME: z.string().default('atendimento-whatsapp'),

  // Pasta das sessões. **Uma subpasta por bot** (o slug): duas conexões na mesma sessão do
  // Baileys derrubam uma à outra. Nunca aponte para a pasta do notificador nem a dos boletos.
  AUTH_DIR: z.string().default('./data/sessoes'),

  // Quantas respostas por ciclo da fila, somando todos os bots. Baixo de propósito: o que
  // protege um número é o ritmo.
  LOTE: z.coerce.number().int().min(1).max(50).default(5),

  // De quanto em quanto tempo o worker pergunta à API quais bots conectar.
  RECONCILIAR_SEGUNDOS: z.coerce.number().int().min(5).max(600).default(30),

  // Teto de sessões neste processo. Cada sessão do Baileys custa memória; a VM da plataforma
  // tem 2 GB (docs/producao.md), e o teto existe para o serviço falhar com mensagem clara em
  // vez de morrer por falta de memória no meio de uma conversa.
  MAX_SESSOES: z.coerce.number().int().min(1).max(20).default(4),
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
