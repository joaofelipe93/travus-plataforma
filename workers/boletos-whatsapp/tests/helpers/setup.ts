/**
 * Precisa ser o PRIMEIRO import de qualquer teste que toque em `src/`.
 *
 * `config/env.ts` valida o ambiente e chama `process.exit(1)` no momento do import;
 * `db/index.ts` cria o pool do Postgres também no import. As variáveis têm que existir antes
 * disso — por isso este módulo não importa nada de `src/`.
 *
 * O banco é o Postgres de teste da plataforma (TEST_DATABASE_URL, banco `travus_teste`, com as
 * migrações aplicadas pelos testes da API). Os arquivos rodam um de cada vez
 * (`--test-concurrency=1`) porque dividem as tabelas. Rode pelo `make test` da raiz.
 *
 * **Nenhum teste deste serviço abre socket nem fala com o WhatsApp**: o laço da fila recebe o
 * "carteiro" por injeção, e os dublês registram o que teria sido enviado.
 */
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

export const TOKEN_ADMIN_TESTE = 'token-de-administracao-de-teste-0123456789'

const dir = mkdtempSync(join(tmpdir(), 'boletos-wa-test-'))

process.env.ADMIN_TOKEN = TOKEN_ADMIN_TESTE
// Sem TEST_DATABASE_URL, só os testes que não tocam no banco funcionam.
process.env.DATABASE_URL = process.env.TEST_DATABASE_URL ?? 'postgres://sem-banco-de-teste.invalid/travus_teste'
process.env.AUTH_DIR = join(dir, 'auth_info')
process.env.LOG_LEVEL = 'fatal'
// Fora de produção o logger usa o pino-pretty, que sobe uma worker thread e segura o processo.
process.env.NODE_ENV = 'production'
process.env.ENVIO_HABILITADO = 'true'
process.env.LOTE = '5'

process.on('exit', () => rmSync(dir, { recursive: true, force: true }))
