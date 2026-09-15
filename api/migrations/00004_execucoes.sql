-- +goose Up
-- Execuções do worker Canopus. Etapa 2: só dry_run (a API recusa criar "real").
CREATE TABLE execucoes (
    id                      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tipo                    text        NOT NULL CHECK (tipo IN ('dry_run', 'real')),
    status                  text        NOT NULL DEFAULT 'na_fila'
                            CHECK (status IN ('na_fila', 'em_andamento', 'concluida', 'concluida_com_erros', 'cancelada', 'falhou')),
    -- Uma sessão de login por vez no Newcon: serializa por credencial.
    credencial              text        NOT NULL DEFAULT 'canopus',
    criada_por              bigint      NOT NULL REFERENCES usuarios (id),
    aprovada_por            bigint      REFERENCES usuarios (id),
    cancelamento_solicitado boolean     NOT NULL DEFAULT false,
    cancelada_por           bigint      REFERENCES usuarios (id),
    worker                  text,
    trava_ate               timestamptz,
    erro                    text,
    criada_em               timestamptz NOT NULL DEFAULT now(),
    iniciada_em             timestamptz,
    finalizada_em           timestamptz
);
CREATE INDEX execucoes_criada_em_idx ON execucoes (criada_em DESC);
-- No máximo uma execução em andamento por credencial, garantido pelo banco.
CREATE UNIQUE INDEX execucoes_uma_em_andamento_por_credencial ON execucoes (credencial) WHERE status = 'em_andamento';

-- Screenshots (e, na Etapa 3, PDFs). Têm dados pessoais: só saem pela API, com sessão.
CREATE TABLE arquivos (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tipo         text        NOT NULL CHECK (tipo IN ('screenshot', 'pdf')),
    nome         text        NOT NULL,
    content_type text        NOT NULL,
    tamanho      integer     NOT NULL,
    sha256       text        NOT NULL,
    conteudo     bytea       NOT NULL,
    criado_em    timestamptz NOT NULL DEFAULT now(),
    expira_em    timestamptz
);
CREATE INDEX arquivos_expira_em_idx ON arquivos (expira_em) WHERE expira_em IS NOT NULL;

-- Máquina de estados por cota (ver CLAUDE.md). Grupo/cota/cliente são copiados:
-- a execução registra o que foi processado, mesmo que o cadastro mude depois.
CREATE TABLE execucao_cotas (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    execucao_id   bigint      NOT NULL REFERENCES execucoes (id) ON DELETE CASCADE,
    cota_id       bigint      NOT NULL REFERENCES cotas (id),
    ordem         integer     NOT NULL,
    grupo         text        NOT NULL,
    cota          text        NOT NULL,
    versao        text        NOT NULL,
    cliente_nome  text        NOT NULL,
    modalidade    text        NOT NULL,
    status        text        NOT NULL DEFAULT 'pendente'
                  CHECK (status IN ('pendente', 'em_andamento', 'verificada', 'erro_antes_confirmar',
                                    'confirmacao_iniciada', 'confirmada', 'erro_apos_confirmar', 'cancelada')),
    erro_tipo     text        CHECK (erro_tipo IN ('conhecido', 'inesperado')),
    erro          text,
    detalhes      jsonb       NOT NULL DEFAULT '{}',
    screenshot_id uuid        REFERENCES arquivos (id) ON DELETE SET NULL,
    tentativas    integer     NOT NULL DEFAULT 0,
    iniciada_em   timestamptz,
    finalizada_em timestamptz,
    UNIQUE (execucao_id, cota_id),
    UNIQUE (execucao_id, ordem)
);

-- Proteção contra lance duplicado no próprio banco: depois de iniciar a confirmação,
-- a cota nunca volta a um estado que permitiria processá-la de novo.
-- +goose StatementBegin
CREATE FUNCTION execucao_cotas_proteger_confirmacao() RETURNS trigger AS $$
BEGIN
    IF OLD.status = 'confirmacao_iniciada'
       AND NEW.status NOT IN ('confirmacao_iniciada', 'confirmada', 'erro_apos_confirmar') THEN
        RAISE EXCEPTION 'cota % já iniciou a confirmação: não pode passar para %', OLD.id, NEW.status;
    END IF;
    IF OLD.status IN ('confirmada', 'erro_apos_confirmar')
       AND NEW.status NOT IN ('confirmada', 'erro_apos_confirmar') THEN
        RAISE EXCEPTION 'cota % já passou pela confirmação (%): não pode passar para %', OLD.id, OLD.status, NEW.status;
    END IF;
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER execucao_cotas_proteger_confirmacao
    BEFORE UPDATE OF status ON execucao_cotas
    FOR EACH ROW EXECUTE FUNCTION execucao_cotas_proteger_confirmacao();

-- Log de cada execução (o que aparece ao vivo na tela).
CREATE TABLE execucao_eventos (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    execucao_id      bigint      NOT NULL REFERENCES execucoes (id) ON DELETE CASCADE,
    execucao_cota_id bigint      REFERENCES execucao_cotas (id) ON DELETE CASCADE,
    nivel            text        NOT NULL CHECK (nivel IN ('info', 'ok', 'aviso', 'erro')),
    mensagem         text        NOT NULL,
    dados            jsonb       NOT NULL DEFAULT '{}',
    criado_em        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX execucao_eventos_execucao_idx ON execucao_eventos (execucao_id, id);

-- Avisa a API (LISTEN execucao_eventos) para empurrar o evento por SSE.
-- +goose StatementBegin
CREATE FUNCTION execucao_eventos_notificar() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('execucao_eventos', NEW.execucao_id::text);
    RETURN NEW;
END
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER execucao_eventos_notificar
    AFTER INSERT ON execucao_eventos
    FOR EACH ROW EXECUTE FUNCTION execucao_eventos_notificar();

-- +goose Down
DROP TABLE execucao_eventos;
DROP FUNCTION execucao_eventos_notificar;
DROP TABLE execucao_cotas;
DROP FUNCTION execucao_cotas_proteger_confirmacao;
DROP TABLE arquivos;
DROP TABLE execucoes;
