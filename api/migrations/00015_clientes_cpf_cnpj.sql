-- +goose Up
-- CPF/CNPJ do cliente, para o Boleto Fácil do Canopus (docs/canopus-boleto-facil.md):
-- a consulta de cotas em aberto é por documento (`GET /find-cota/{cpf}`), e sem ele não há
-- como buscar os boletos do mês. A planilha antiga não trazia CPF e o Newcon não dá: quem
-- digita é o admin/operador no cadastro, um documento por cliente.
--
-- Só dígitos, 11 (CPF) ou 14 (CNPJ), que é o que o site do Canopus aceita. Os dígitos
-- verificadores são conferidos na API (internal/cadastro): documento errado leva o Boleto
-- Fácil a devolver cotas vazias e, depois de três tentativas, a pedir captcha.
ALTER TABLE clientes
    ADD COLUMN cpf_cnpj text CHECK (cpf_cnpj ~ '^[0-9]{11}$' OR cpf_cnpj ~ '^[0-9]{14}$');

-- Mesmo documento = mesma pessoa. Parcial porque a maior parte do cadastro ainda está sem
-- documento (e vários nulos não se repetem num índice único comum, mas o parcial deixa a
-- intenção explícita).
CREATE UNIQUE INDEX clientes_cpf_cnpj_key ON clientes (cpf_cnpj) WHERE cpf_cnpj IS NOT NULL;

-- O papel do assistente lia clientes inteira (GRANT de tabela, migração 00011). O documento
-- é identificador forte e não serve para nenhuma pergunta que o assistente responda, então
-- passa a ser a única coluna fora do alcance dele — como senha_hash em usuarios. Consequência
-- conhecida: `SELECT *` em clientes passa a ser recusado para o assistente (as instruções do
-- agente listam as colunas).
REVOKE SELECT ON clientes FROM assistente;
GRANT SELECT (id, nome, nome_normalizado, telefone, email, origem, criado_em, atualizado_em)
    ON clientes TO assistente;

-- +goose Down
DROP INDEX clientes_cpf_cnpj_key;
ALTER TABLE clientes DROP COLUMN cpf_cnpj;
REVOKE SELECT ON clientes FROM assistente;
GRANT SELECT ON clientes TO assistente;
