-- +goose Up
-- Serviço de Atendimento (issue #100): a plataforma cria e opera BOTS de WhatsApp, vários ao
-- mesmo tempo, cada um com conexão, instruções, modelo e fluxo próprios. O bot conversa, coleta
-- dados ao longo das etapas e no fim o atendimento vira um lead (etapa E da issue).
--
-- Duas decisões de arquitetura que explicam o formato daqui:
--
--   1. **A API é a cabeça, o worker são as pernas.** O agente roda na API, que já tem o laço de
--      tool use (internal/assistente), a chave da Anthropic e o banco; o worker de WhatsApp só
--      transporta mensagem, pelas rotas internas. Por isso **não há papel novo no Postgres** —
--      como no worker de boletos e no do Canopus, o segredo não sai da API.
--   2. **Atendimento NÃO entra em cliente_servicos** (migração 00018), de propósito: aquela
--      tabela é dos serviços que agem sobre um cliente do CADASTRO, e o bot atende quem chama,
--      que ainda não é cliente. Mesma razão pela qual Reservas ficou de fora.
CREATE SCHEMA atendimento;

-- Um bot = um número do WhatsApp + instruções + modelo + fluxo.
--
-- `modelo` é texto livre de propósito, validado na API (internal/atendimento tem a lista): um
-- CHECK aqui obrigaria a uma migração a cada modelo novo da Anthropic, e migração existente não
-- se edita. `slug` é o nome da pasta da sessão no volume do worker e o id nas rotas internas —
-- daí o formato restrito e a imutabilidade na prática (trocar o slug perderia o pareamento).
CREATE TABLE atendimento.bots (
    id                      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    nome                    text        NOT NULL,
    slug                    text        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$'),
    modelo                  text        NOT NULL,
    -- Quem o bot é, o que pode e o que não pode. Vai para as instruções do agente.
    instrucoes              text        NOT NULL,
    -- Primeira resposta quando uma conversa abre; vazio: o modelo abre do jeito dele.
    saudacao                text,
    -- Atende (conecta o WhatsApp e responde). Nasce desligado: ninguém liga sem querer.
    ativo                   boolean     NOT NULL DEFAULT false,
    -- Manda de verdade. Desligado = ensaio: a resposta é composta e gravada, e NADA sai. Só
    -- admin muda (a tela e a API barram operador): é o equivalente do lance real automático.
    envio_habilitado        boolean     NOT NULL DEFAULT false,
    envio_habilitado_por    bigint      REFERENCES usuarios (id),
    envio_habilitado_em     timestamptz,
    -- Tetos por bot. O de tokens é por conversa: conversa que ultrapassa é encerrada com o
    -- motivo, em vez de virar uma conta sem fim. O de mensagens é por hora, contra laço de
    -- resposta (dois bots conversando um com o outro, cliente automático do outro lado).
    max_mensagens_hora      integer     NOT NULL DEFAULT 60 CHECK (max_mensagens_hora BETWEEN 1 AND 1000),
    max_tokens_conversa     integer     NOT NULL DEFAULT 50000 CHECK (max_tokens_conversa BETWEEN 1000 AND 2000000),
    -- Espera antes de responder: conversa humana não responde em 200 ms, e o ritmo também
    -- protege o número (ver a regra 11 do CLAUDE.md, dos boletos).
    pausa_resposta_segundos integer     NOT NULL DEFAULT 3 CHECK (pausa_resposta_segundos BETWEEN 0 AND 120),
    criado_por              bigint      REFERENCES usuarios (id),
    atualizado_por          bigint      REFERENCES usuarios (id),
    criado_em               timestamptz NOT NULL DEFAULT now(),
    atualizado_em           timestamptz NOT NULL DEFAULT now()
);

-- O fluxo (decisão do usuário em 29/10/2026): **etapas com objetivo**, não árvore de menu. Cada
-- etapa diz o que precisa descobrir e quais campos coletar; o modelo conduz a conversa e a
-- plataforma avança quando os campos obrigatórios saíram.
--
-- `campos` é jsonb: [{"id":"telefone","rotulo":"Telefone","obrigatorio":true}]. É lista, não
-- tabela, porque campo só existe dentro da etapa, nunca é consultado sozinho e a tela edita a
-- etapa inteira de uma vez — tabela separada só traria junção e ordem para manter.
--
-- A unicidade de (bot_id, ordem) é DEFERRABLE de propósito: reordenar etapas na tela é um UPDATE
-- que troca posições, e com restrição imediata a troca falharia no meio da transação.
CREATE TABLE atendimento.etapas (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    bot_id        bigint      NOT NULL REFERENCES atendimento.bots (id) ON DELETE CASCADE,
    ordem         integer     NOT NULL CHECK (ordem > 0),
    nome          text        NOT NULL,
    -- O que esta etapa precisa descobrir, em uma frase. Vai para o agente.
    objetivo      text        NOT NULL,
    -- Detalhe extra só desta etapa (tom, o que não perguntar, exemplo de resposta boa).
    instrucoes    text,
    campos        jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(campos) = 'array'),
    criado_em     timestamptz NOT NULL DEFAULT now(),
    atualizado_em timestamptz NOT NULL DEFAULT now(),
    UNIQUE (bot_id, ordem) DEFERRABLE INITIALLY IMMEDIATE
);
CREATE INDEX etapas_bot_idx ON atendimento.etapas (bot_id, ordem);

-- Uma conversa com um telefone. `telefone` guarda só dígitos (o jid do WhatsApp é assunto do
-- worker); é dado pessoal, então não sai para o assistente (ver os GRANT no fim).
CREATE TABLE atendimento.conversas (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    bot_id          bigint      NOT NULL REFERENCES atendimento.bots (id),
    telefone        text        NOT NULL CHECK (telefone ~ '^[0-9]{8,20}$'),
    -- Como o cliente aparece no WhatsApp; serve para a tela e para o lead.
    contato_nome    text,
    -- Etapa em que a conversa está. Nula quando encerrou (ou quando o bot não tem fluxo).
    etapa_id        bigint      REFERENCES atendimento.etapas (id) ON DELETE SET NULL,
    status          text        NOT NULL DEFAULT 'aberta'
                                CHECK (status IN ('aberta', 'encerrada', 'abandonada', 'erro')),
    -- Por que encerrou: 'concluida' (o fluxo acabou), 'teto_tokens', 'bot_desligado', o que o
    -- agente disser ao chamar a ferramenta `encerrar`, ou o erro.
    motivo          text,
    -- Gasto da conversa, somado a cada resposta: é o que o teto de tokens compara.
    tokens_entrada  integer     NOT NULL DEFAULT 0,
    tokens_saida    integer     NOT NULL DEFAULT 0,
    iniciada_em     timestamptz NOT NULL DEFAULT now(),
    ultima_em       timestamptz NOT NULL DEFAULT now(),
    encerrada_em    timestamptz
);
-- Uma conversa aberta por telefone em cada bot: a segunda mensagem da pessoa continua a mesma
-- conversa, em vez de abrir outra e perder a etapa. O banco garante, não o código.
CREATE UNIQUE INDEX conversas_uma_aberta_idx ON atendimento.conversas (bot_id, telefone) WHERE status = 'aberta';
CREATE INDEX conversas_bot_idx ON atendimento.conversas (bot_id, ultima_em DESC);

-- Mensagens da conversa, nos dois sentidos, e a fila de envio ao mesmo tempo (como
-- boletos.mensagens): a recebida entra como 'recebida'; a que o agente compõe entra 'pendente' e
-- o worker a leva. Em modo ensaio ela nasce 'cancelada' com o motivo — é assim que se vê
-- exatamente o que sairia sem mandar nada.
CREATE TABLE atendimento.mensagens (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conversa_id          bigint      NOT NULL REFERENCES atendimento.conversas (id) ON DELETE CASCADE,
    direcao              text        NOT NULL CHECK (direcao IN ('recebida', 'enviada')),
    texto                text        NOT NULL,
    status               text        NOT NULL
                                     CHECK (status IN ('recebida', 'pendente', 'enviada', 'falhou', 'cancelada')),
    -- Por que não saiu: ensaio, bot desligado, teto da hora, número sem WhatsApp.
    motivo               text,
    tentativas           integer     NOT NULL DEFAULT 0,
    proxima_tentativa_em timestamptz NOT NULL DEFAULT now(),
    ultimo_erro          text,
    criada_em            timestamptz NOT NULL DEFAULT now(),
    enviada_em           timestamptz,
    -- Recebida não tem fila; enviada nunca entra como 'recebida'.
    CHECK ((direcao = 'recebida' AND status = 'recebida') OR (direcao = 'enviada' AND status <> 'recebida'))
);
CREATE INDEX mensagens_conversa_idx ON atendimento.mensagens (conversa_id, id);
-- A fila que o worker varre.
CREATE INDEX mensagens_fila_idx ON atendimento.mensagens (proxima_tentativa_em) WHERE status = 'pendente';

-- Os dados que saíram da conversa: é o funil virando informação. Um valor por campo (o último
-- vale), com a etapa em que foi coletado, para a tela mostrar onde cada dado apareceu.
CREATE TABLE atendimento.coletas (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conversa_id bigint      NOT NULL REFERENCES atendimento.conversas (id) ON DELETE CASCADE,
    etapa_id    bigint      REFERENCES atendimento.etapas (id) ON DELETE SET NULL,
    campo       text        NOT NULL,
    valor       text        NOT NULL,
    criado_em   timestamptz NOT NULL DEFAULT now(),
    atualizado_em timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversa_id, campo)
);

-- O assistente da tela inicial (migração 00011) enxerga só o que é CONFIGURAÇÃO e CONTAGEM:
-- bots, etapas e conversas. `mensagens` e `coletas` ficam fora de propósito — são o conteúdo da
-- conversa de um cliente e os dados pessoais que ele contou, e o assistente manda o que lê para
-- a API da Anthropic. Se um dia precisar responder sobre o conteúdo, é decisão do usuário, numa
-- migração própria. `conversas.telefone` e `contato_nome` ficam visíveis porque a tela de
-- Reservas já estabeleceu que telefone de contato o assistente pode ver (decisão de 23/09/2026).
GRANT USAGE ON SCHEMA atendimento TO assistente;
GRANT SELECT ON atendimento.bots, atendimento.etapas, atendimento.conversas TO assistente;

-- +goose Down
REVOKE ALL ON ALL TABLES IN SCHEMA atendimento FROM assistente;
REVOKE USAGE ON SCHEMA atendimento FROM assistente;
DROP SCHEMA atendimento CASCADE;
