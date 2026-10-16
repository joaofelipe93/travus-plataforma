-- +goose Up
-- Etapa 3 do serviço de boletos (issue #48): a fila de mensagens no WhatsApp e a configuração
-- do agendamento. **Outro número do WhatsApp**, outro serviço, outra sessão: o do notificador
-- de check-in manda em grupo, este manda no privado de cada cliente.
--
-- Desenho igual ao do check-in, e pelo mesmo motivo: quem monta a mensagem só enfileira, e um
-- laço separado envia quando o WhatsApp está conectado. Mensagem duplicada no privado de um
-- cliente é pior do que atraso, então a dedução de repetição é no banco, nos índices abaixo.

CREATE TABLE boletos.mensagens (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- Teste de envio não tem rodada; mensagem de rodada apagada vai com ela.
    rodada_id            bigint      REFERENCES boletos.rodadas (id) ON DELETE CASCADE,
    rodada_cliente_id    bigint      REFERENCES boletos.rodada_clientes (id) ON DELETE CASCADE,
    -- 'envio': uma mensagem por parcela, com o boleto anexado.
    -- 'cobranca': uma por cliente que continua com parcela em aberto, sem anexo.
    -- 'teste': disparo manual do admin para conferir o número pareado.
    tipo                 text        NOT NULL CHECK (tipo IN ('envio', 'cobranca', 'teste')),
    parcela_id           bigint      REFERENCES boletos.parcelas (id) ON DELETE SET NULL,
    -- Telefone só com dígitos, com DDI (5511999999999): é o que o WhatsApp usa como destino.
    destino_telefone     text        NOT NULL CHECK (destino_telefone ~ '^[0-9]{12,15}$'),
    destino_nome         text        NOT NULL,
    texto                text        NOT NULL,
    -- O PDF fica em arquivos (é o que a tela mostra) e uma cópia viaja aqui ENQUANTO a mensagem
    -- está pendente: assim o laço de envio não depende de ler o schema public, e o anexo é
    -- apagado no envio (anexo_pdf = NULL), limitando a duplicação à fila.
    pdf_id               uuid        REFERENCES arquivos (id) ON DELETE SET NULL,
    anexo_pdf            bytea,
    anexo_nome           text,
    status               text        NOT NULL DEFAULT 'pendente'
                                     CHECK (status IN ('pendente', 'enviada', 'falhou', 'cancelada')),
    tentativas           integer     NOT NULL DEFAULT 0,
    proxima_tentativa_em timestamptz NOT NULL DEFAULT now(),
    ultimo_erro          text,
    enviada_em           timestamptz,
    criada_em            timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX mensagens_pendentes_idx ON boletos.mensagens (proxima_tentativa_em, id)
    WHERE status = 'pendente';
CREATE INDEX mensagens_rodada_idx ON boletos.mensagens (rodada_id, id);

-- As duas travas contra mandar duas vezes para o mesmo cliente:
-- uma mensagem por parcela no envio, e uma por cliente na cobrança.
CREATE UNIQUE INDEX mensagens_uma_por_parcela ON boletos.mensagens (parcela_id, tipo)
    WHERE parcela_id IS NOT NULL AND status <> 'cancelada';
CREATE UNIQUE INDEX mensagens_uma_cobranca_por_cliente ON boletos.mensagens (rodada_cliente_id, tipo)
    WHERE parcela_id IS NULL AND rodada_cliente_id IS NOT NULL AND status <> 'cancelada';

-- Configuração do serviço (tela Canopus → Boletos). Uma linha só, como a do Canopus.
-- Os padrões saem da issue #48: boleto no dia 8, cobrança no dia 14, antecipando para o dia
-- útil anterior quando cai em sábado, domingo ou feriado. **Nasce com o envio DESLIGADO**: a
-- chave BOLETOS_ENVIO_HABILITADO do servidor é o interruptor mestre, e nenhum agente liga nada.
CREATE TABLE boletos.configuracao (
    id                      boolean     PRIMARY KEY DEFAULT true CHECK (id),
    -- Rodadas automáticas (buscar os boletos e cobrar). Desligado: ninguém roda sozinho.
    agendamento_ativo       boolean     NOT NULL DEFAULT false,
    dia_envio               smallint    NOT NULL DEFAULT 8  CHECK (dia_envio BETWEEN 1 AND 31),
    hora_envio              smallint    NOT NULL DEFAULT 9  CHECK (hora_envio BETWEEN 0 AND 23),
    dia_cobranca            smallint    NOT NULL DEFAULT 14 CHECK (dia_cobranca BETWEEN 1 AND 31),
    hora_cobranca           smallint    NOT NULL DEFAULT 9  CHECK (hora_cobranca BETWEEN 0 AND 23),
    antecipar_dia_util      boolean     NOT NULL DEFAULT true,
    -- Envio de verdade no WhatsApp dos clientes. Desligado, a rodada monta as mensagens e as
    -- deixa como 'cancelada' com o motivo: dá para ver o que sairia sem mandar nada (ensaio).
    envio_habilitado        boolean     NOT NULL DEFAULT false,
    envio_habilitado_por    bigint      REFERENCES usuarios (id),
    -- Proteção contra banimento do número: pausa entre mensagens e teto por rodada.
    pausa_envio_segundos    smallint    NOT NULL DEFAULT 20 CHECK (pausa_envio_segundos BETWEEN 5 AND 600),
    max_mensagens_rodada    integer     NOT NULL DEFAULT 200 CHECK (max_mensagens_rodada BETWEEN 1 AND 5000),
    -- Ocorrência já atendida de cada rodada automática (dispara uma vez por mês, mesmo com
    -- reinício da API). Os resultados são os mesmos nomes do dry-run agendado do Canopus.
    ultimo_envio_dia        date,
    ultimo_envio_em         timestamptz,
    ultimo_envio_resultado  text        CHECK (ultimo_envio_resultado IN
                                        ('criada', 'sem_clientes', 'sem_responsavel', 'atrasada', 'antes_de_ligar')),
    ultima_cobranca_dia     date,
    ultima_cobranca_em      timestamptz,
    ultima_cobranca_resultado text      CHECK (ultima_cobranca_resultado IN
                                        ('criada', 'sem_envio', 'sem_clientes', 'sem_responsavel', 'atrasada', 'antes_de_ligar')),
    atualizado_por          bigint      REFERENCES usuarios (id),
    atualizado_em           timestamptz NOT NULL DEFAULT now()
);
INSERT INTO boletos.configuracao (id) VALUES (true);

-- Papel do serviço de envio (workers/boletos-whatsapp): só a fila e a configuração, nada do
-- resto da plataforma. Nasce sem login: `api migrate up` liga com BOLETOS_DB_SENHA.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'boletos') THEN
        CREATE ROLE boletos NOLOGIN;
    END IF;
END
$$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA boletos TO boletos;
GRANT SELECT, INSERT, UPDATE ON boletos.mensagens TO boletos;
GRANT SELECT ON boletos.configuracao TO boletos;

-- O assistente responde "as mensagens do dia 8 saíram?": vê a fila sem o anexo e sem o
-- telefone do cliente, e a configuração.
GRANT SELECT (id, rodada_id, rodada_cliente_id, tipo, parcela_id, destino_nome, texto, pdf_id,
              status, tentativas, proxima_tentativa_em, ultimo_erro, enviada_em, criada_em)
    ON boletos.mensagens TO assistente;
GRANT SELECT ON boletos.configuracao TO assistente;

-- +goose Down
REVOKE ALL ON boletos.mensagens, boletos.configuracao FROM assistente;
REVOKE ALL ON boletos.mensagens, boletos.configuracao FROM boletos;
DROP TABLE boletos.mensagens;
DROP TABLE boletos.configuracao;
