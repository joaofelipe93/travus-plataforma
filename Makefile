# Atalhos do ambiente local. `make` sozinho lista os comandos.
COMPOSE := docker compose -f deploy/docker-compose.yml

# O worker legado roda com o uid/gid do host (screenshots em deploy/data/canopus).
export HOST_UID := $(shell id -u)
export HOST_GID := $(shell id -g)

# O Traefik leva alguns segundos para ligar as rotas depois que o container fica saudável.
ESPERAR_GATEWAY = for i in $$(seq 30); do [ "$$(curl -s -o /dev/null -w '%{http_code}' http://app.localhost/login)" = 200 ] && exit 0; sleep 1; done; echo "aviso: app.localhost/login ainda não responde 200 (veja make logs s=traefik)"

.DEFAULT_GOAL := help
.PHONY: help env up down logs ps migrate usuario google-token google-status test smoke dev-web sqlc paridade-csv worker-dry-run

help: ## Lista os comandos
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  make %-16s %s\n", $$1, $$2}'

up: env ## Sobe traefik, postgres, migrações, api, web e worker (o worker só acessa o Newcon quando alguém inicia uma execução)
	$(COMPOSE) up -d --build --wait
	@$(ESPERAR_GATEWAY)
	@echo "Pronto: http://app.localhost  |  http://api.localhost/health  |  http://traefik.localhost"

down: ## Derruba os containers (o banco fica no volume)
	$(COMPOSE) --profile canopus down

logs: ## Acompanha os logs (ex.: make logs s=worker-canopus)
	$(COMPOSE) logs -f --tail=100 $(s)

ps: ## Estado dos containers
	$(COMPOSE) ps -a

migrate: env ## Aplica as migrações pendentes
	$(COMPOSE) run --rm --build migrate

usuario: env ## Usuários: make usuario args='criar --email a@b.com --nome "Ana" --perfil operador' (ou listar, senha, desativar, ativar)
	$(COMPOSE) --profile cli run --rm --build cli usuario $(args)

google-token: env ## Importa workers/canopus/token.json (Google Drive) cifrado no banco
	@test -f workers/canopus/token.json || { echo "workers/canopus/token.json não encontrado"; exit 1; }
	$(COMPOSE) --profile cli run --rm -T --build cli google importar-token < workers/canopus/token.json

google-status: env ## Mostra o que está configurado para o envio ao Google Drive
	$(COMPOSE) --profile cli run --rm -T cli google status

test: env ## Testes: api (unitários e integração com Postgres), web (lint e tipos), worker (node:test, sem Newcon)
	$(COMPOSE) up -d --wait postgres
	$(COMPOSE) --profile teste run --rm api-teste
	cd web && npm run lint && npx next typegen && npx tsc --noEmit
	cd workers/canopus && for f in src/*.js; do node --check "$$f" || exit 1; done && npm test
	sh -n workers/canopus/docker-entrypoint.sh

smoke: ## Checagens pelo gateway com curl (precisa de make up)
	bash tools/smoke/etapa1.sh

dev-web: env ## Web em modo desenvolvimento (next dev) atrás do Traefik; make up volta ao normal
	cd web && npm ci --no-audit --no-fund
	$(COMPOSE) -f deploy/docker-compose.dev.yml up -d --wait web
	@$(ESPERAR_GATEWAY)
	@echo "Web em modo dev: http://app.localhost (logs: make logs s=web)"

sqlc: ## Gera api/internal/db a partir das migrações e das consultas
	docker run --rm --user "$$(id -u):$$(id -g)" -v "$(CURDIR)/api":/src -w /src sqlc/sqlc:1.31.1 generate

paridade-csv: ## Regera o esperado do teste de paridade rodando o csv.js original
	node tools/paridade-csv/gerar-esperado.js

worker-dry-run: env ## Script legado: dry-run de 1 cota da planilha no container (não confirma lance)
	mkdir -p deploy/data/canopus
	$(COMPOSE) --profile canopus run --rm --build worker-legado
	@echo "Screenshots em deploy/data/canopus/screenshots/"

# Garante deploy/.env com senha do Postgres, WORKER_TOKEN e CHAVE_CRIPTOGRAFIA (acrescenta o que faltar).
env: deploy/.env
	@grep -q '^WORKER_TOKEN=' deploy/.env || { umask 077; echo "WORKER_TOKEN=$$(openssl rand -hex 32)" >> deploy/.env; echo "Acrescentado WORKER_TOKEN ao deploy/.env"; }
	@grep -q '^CHAVE_CRIPTOGRAFIA=' deploy/.env || { umask 077; echo "CHAVE_CRIPTOGRAFIA=$$(openssl rand -hex 32)" >> deploy/.env; echo "Acrescentada CHAVE_CRIPTOGRAFIA ao deploy/.env (guarde com o backup do banco: sem ela, o token do Google não decifra)"; }

deploy/.env:
	@umask 077 && sed "s/troque-esta-senha/$$(openssl rand -hex 24)/" deploy/.env.example > $@
	@echo "Criado deploy/.env com senha aleatória do Postgres"
