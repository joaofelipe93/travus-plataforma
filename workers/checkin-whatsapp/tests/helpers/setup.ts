/**
 * Precisa ser o PRIMEIRO import de qualquer teste que toque em `src/`.
 *
 * `config/env.ts` valida o ambiente e chama `process.exit(1)` no momento do
 * import; `db/index.ts` cria o pool do Postgres também no import. As variáveis
 * têm que existir antes disso — por isso este módulo não importa nada de `src/`
 * (imports são avaliados antes do corpo do módulo).
 *
 * O banco é o Postgres de teste da plataforma (TEST_DATABASE_URL, banco
 * `travus_teste`, com as migrações aplicadas pelos testes da API). Os arquivos
 * rodam um de cada vez (`--test-concurrency=1` no `npm test`) porque dividem
 * as tabelas. Rode pelo `make test` da raiz.
 */
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

export const TEST_SECRET = 'segredo-de-teste-32-chars'
export const TEST_ADMIN_TOKEN = 'token-de-administracao-de-teste-0123456789'
export const TEST_GROUP_JID = '1234567890-1234567890@g.us'

const dir = mkdtempSync(join(tmpdir(), 'checkin-test-'))

process.env.WEBHOOK_SECRET = TEST_SECRET
process.env.ADMIN_TOKEN = TEST_ADMIN_TOKEN
// Sem TEST_DATABASE_URL, só os testes que não tocam no banco funcionam: os demais
// falham em helpers/db.ts com a explicação.
process.env.DATABASE_URL = process.env.TEST_DATABASE_URL ?? 'postgres://sem-banco-de-teste.invalid/travus_teste'
process.env.AUTH_DIR = join(dir, 'auth_info')
process.env.LOG_LEVEL = 'fatal'
// Fora de produção o logger usa o transport pino-pretty, que sobe uma worker
// thread e segura o processo de teste no fim da execução.
process.env.NODE_ENV = 'production'

// O JID é opcional no boot: os testes que precisam dele ligam via `env`.
delete process.env.WHATSAPP_GROUP_JID

process.on('exit', () => rmSync(dir, { recursive: true, force: true }))
