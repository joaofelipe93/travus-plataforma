-- +goose Up
-- Serviço de boletos da Canopus (issue #48): todo mês a plataforma busca no Boleto Fácil os
-- boletos em aberto dos clientes cadastrados e manda por WhatsApp; dias depois, cobra quem não
-- pagou. Esta migração traz só a parte de BUSCAR e GUARDAR (o envio vem na etapa seguinte).
-- Como o site funciona está em docs/canopus-boleto-facil.md — leia antes de mexer aqui.
--
-- O worker de boletos NÃO fala com o banco: ele usa as rotas internas da API, como o worker do
-- Canopus. Por isso não há papel novo no Postgres — e o CPF dos clientes nunca sai da API.
CREATE SCHEMA boletos;

-- Uma rodada = uma passada pelo cadastro. 'envio' é a do dia 8 (manda o boleto); 'cobranca' é a
-- do dia 14 (compara com a rodada de envio e cobra o que continua em aberto).
CREATE TABLE boletos.rodadas (
    id                      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tipo                    text        NOT NULL CHECK (tipo IN ('envio', 'cobranca')),
    -- Mês de referência, sempre no dia 1: é por ele que a cobrança acha a rodada de envio.
    referencia              date        NOT NULL,
    status                  text        NOT NULL DEFAULT 'na_fila'
                            CHECK (status IN ('na_fila', 'em_andamento', 'concluida',
                                              'concluida_com_erros', 'cancelada', 'falhou')),
    origem                  text        NOT NULL DEFAULT 'manual' CHECK (origem IN ('manual', 'agendada')),
    criada_por              bigint      NOT NULL REFERENCES usuarios (id),
    cancelamento_solicitado boolean     NOT NULL DEFAULT false,
    cancelada_por           bigint      REFERENCES usuarios (id),
    worker                  text,
    trava_ate               timestamptz,
    erro                    text,
    criada_em               timestamptz NOT NULL DEFAULT now(),
    iniciada_em             timestamptz,
    finalizada_em           timestamptz
);
CREATE INDEX rodadas_criada_em_idx ON boletos.rodadas (criada_em DESC);
CREATE INDEX rodadas_referencia_idx ON boletos.rodadas (referencia, tipo);
-- Uma rodada em andamento por vez, garantido pelo banco: o Boleto Fácil tem limite de taxa
-- (429) e duas rodadas juntas multiplicariam os pedidos. O índice é sobre uma constante.
CREATE UNIQUE INDEX rodadas_uma_em_andamento ON boletos.rodadas ((1)) WHERE status = 'em_andamento';

-- Um cliente dentro da rodada. O nome é copiado: a rodada registra o que foi processado, mesmo
-- que o cadastro mude depois (mesma regra de execucao_cotas). O CPF não é copiado — ele fica só
-- em clientes.cpf_cnpj, para não espalhar documento pelo banco.
CREATE TABLE boletos.rodada_clientes (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    rodada_id            bigint      NOT NULL REFERENCES boletos.rodadas (id) ON DELETE CASCADE,
    cliente_id           bigint      NOT NULL REFERENCES clientes (id),
    ordem                integer     NOT NULL,
    cliente_nome         text        NOT NULL,
    status               text        NOT NULL DEFAULT 'pendente'
                         CHECK (status IN ('pendente', 'em_andamento', 'consultado',
                                           'sem_boleto', 'erro', 'cancelado')),
    erro_tipo            text        CHECK (erro_tipo IN ('conhecido', 'inesperado')),
    erro                 text,
    -- O que o Boleto Fácil devolveu: cotas do CPF, quantas casaram com o cadastro e quantas
    -- parcelas em aberto saíram delas. Ajuda a ver cadastro errado sem abrir o log.
    cotas_no_site        integer     NOT NULL DEFAULT 0,
    cotas_casadas        integer     NOT NULL DEFAULT 0,
    tentativas           integer     NOT NULL DEFAULT 0,
    iniciado_em          timestamptz,
    finalizado_em        timestamptz,
    UNIQUE (rodada_id, cliente_id)
);
CREATE INDEX rodada_clientes_rodada_idx ON boletos.rodada_clientes (rodada_id, ordem);

-- Uma parcela em aberto encontrada no site. É a unidade da cobrança: no dia 14 a plataforma
-- pergunta de novo e a parcela que NÃO volta mais como aberta é a que foi paga (o site não tem
-- campo de "pago" — ver docs/canopus-boleto-facil.md).
CREATE TABLE boletos.parcelas (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    rodada_cliente_id bigint      NOT NULL REFERENCES boletos.rodada_clientes (id) ON DELETE CASCADE,
    cota_id           bigint      NOT NULL REFERENCES cotas (id),
    grupo             text        NOT NULL,
    cota              text        NOT NULL,
    versao            text        NOT NULL,
    -- parcelNumber vem como texto e nem sempre é número: há linha "DIF" (diferença/reajuste).
    parcela           text        NOT NULL,
    vencimento        date        NOT NULL,
    valor_centavos    bigint      NOT NULL CHECK (valor_centavos > 0),
    -- "overdue" ou "duplicate": é o que decide a rota de impressão. Veio overdue até em parcela
    -- com vencimento no futuro, então NÃO quer dizer "em atraso".
    tipo              text        NOT NULL,
    -- Ids do site. transaction_id muda a cada generate-bill: serve de histórico, não de link.
    charge_id         bigint,
    transaction_id    bigint,
    pdf_id            uuid        REFERENCES arquivos (id) ON DELETE SET NULL,
    criada_em         timestamptz NOT NULL DEFAULT now(),
    -- A mesma parcela não entra duas vezes na mesma passada pelo cliente.
    UNIQUE (rodada_cliente_id, cota_id, parcela, vencimento)
);
CREATE INDEX parcelas_cota_idx ON boletos.parcelas (cota_id, vencimento);

-- Log da rodada, como execucao_eventos: sem ele, depurar um serviço que roda uma vez por mês é
-- adivinhação. Nunca guarde CPF nem valor aqui — isso tem lugar próprio nas tabelas acima.
CREATE TABLE boletos.eventos (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    rodada_id         bigint      NOT NULL REFERENCES boletos.rodadas (id) ON DELETE CASCADE,
    rodada_cliente_id bigint      REFERENCES boletos.rodada_clientes (id) ON DELETE CASCADE,
    nivel             text        NOT NULL CHECK (nivel IN ('info', 'ok', 'aviso', 'erro')),
    mensagem          text        NOT NULL,
    dados             jsonb       NOT NULL DEFAULT '{}',
    criado_em         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX eventos_rodada_idx ON boletos.eventos (rodada_id, id);

-- O assistente responde "a rodada do dia 8 rodou?" e "quantos boletos saíram": são as perguntas
-- que o usuário faz. Vê a rodada, o cliente e a parcela (sem CPF, que ele não lê em clientes, e
-- sem o conteúdo do PDF, que mora em arquivos e de onde ele só enxerga metadados).
GRANT USAGE ON SCHEMA boletos TO assistente;
GRANT SELECT ON boletos.rodadas, boletos.rodada_clientes, boletos.parcelas, boletos.eventos TO assistente;

-- +goose Down
REVOKE ALL ON ALL TABLES IN SCHEMA boletos FROM assistente;
REVOKE USAGE ON SCHEMA boletos FROM assistente;
DROP SCHEMA boletos CASCADE;
