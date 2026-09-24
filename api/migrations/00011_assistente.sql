-- +goose Up
-- Assistente da tela inicial (api/internal/assistente): o agente responde perguntas sobre os
-- serviços e pode rodar um SELECT livre. O SELECT roda com este papel, que só LÊ e só enxerga o
-- que está abaixo: nada de sessões, senhas, tokens cifrados, conteúdo de PDF/screenshot nem a
-- tabela de versões do goose. Tabela nova não entra sozinha (sem DEFAULT PRIVILEGES): quem quiser
-- que o assistente veja uma tabela nova dá o GRANT numa migração.
--
-- Nasce sem login: `api migrate up` liga o login com ASSISTENTE_DB_SENHA (como o papel checkin).
-- A API conecta com ele num pool próprio: um SET ROLE não leva a nenhum papel com mais acesso.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'assistente') THEN
        CREATE ROLE assistente NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT CONNECTION LIMIT 4;
    END IF;
END
$$;
-- +goose StatementEnd

-- Mesmo que a transação da API falhe em pedir, o papel já nasce só leitura e com tempo curto.
ALTER ROLE assistente SET default_transaction_read_only = on;
ALTER ROLE assistente SET statement_timeout = '5s';
ALTER ROLE assistente SET idle_in_transaction_session_timeout = '30s';

-- O schema public dá USAGE a todos por padrão, mas não conte com isso (o banco de teste recria o
-- schema sem o padrão).
GRANT USAGE ON SCHEMA public TO assistente;

-- Canopus: cadastro, execuções e lances.
GRANT SELECT ON clientes, cotas, execucoes, execucao_cotas, execucao_eventos, lances TO assistente;
-- Importações antigas: sem as linhas da planilha (o que interessa já está em clientes e cotas).
GRANT SELECT (id, criada_por, arquivo_nome, status, criada_em, finalizada_em) ON importacoes TO assistente;
-- Arquivos: só os metadados, nunca o conteúdo.
GRANT SELECT (id, tipo, nome, content_type, tamanho, criado_em, expira_em) ON arquivos TO assistente;
-- Pessoas: sem o hash da senha.
GRANT SELECT (id, email, nome, perfil, ativo, criado_em, atualizado_em, telefone, cargo) ON usuarios TO assistente;
-- Quem cadastrou qual credencial pessoal, sem o valor nem a dica.
GRANT SELECT (usuario_id, credencial, criado_em, atualizado_em) ON credenciais_usuario TO assistente;
-- Integrações configuradas (ex.: google_drive), sem o token cifrado.
GRANT SELECT (nome, atualizado_em) ON integracoes TO assistente;
-- Auditoria: o que aconteceu e quando, sem o IP.
GRANT SELECT (id, usuario_id, acao, entidade, entidade_id, detalhes, criado_em) ON auditoria TO assistente;

-- Notificador de check-in: reservas, resumos e mensagens do grupo.
GRANT USAGE ON SCHEMA checkin TO assistente;
GRANT SELECT ON checkin.eventos, checkin.mensagens, checkin.reservas, checkin.resumos, checkin.configuracao TO assistente;

-- +goose Down
-- O papel fica (é do cluster e pode ter permissões em outro banco, como o de teste); tira só o acesso.
REVOKE ALL ON ALL TABLES IN SCHEMA checkin FROM assistente;
REVOKE USAGE ON SCHEMA checkin FROM assistente;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM assistente;
REVOKE USAGE ON SCHEMA public FROM assistente;
