# Atalhos do ambiente local. `make` sozinho lista os comandos.
COMPOSE := docker compose -f deploy/docker-compose.yml

# O worker roda com o uid/gid do host (screenshots em deploy/data/canopus).
export HOST_UID := $(shell id -u)
export HOST_GID := $(shell id -g)

.DEFAULT_GOAL := help
.PHONY: help up down logs ps migrate test worker-dry-run

help: ## Lista os comandos
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  make %-16s %s\n", $$1, $$2}'

up: deploy/.env ## Sobe traefik, postgres, migrações, api e web
	$(COMPOSE) up -d --build --wait
	@echo "Pronto: http://app.localhost  |  http://api.localhost/health  |  http://traefik.localhost"

down: ## Derruba os containers (o banco fica no volume)
	$(COMPOSE) --profile canopus down

logs: ## Acompanha os logs (ex.: make logs s=api)
	$(COMPOSE) logs -f --tail=100 $(s)

ps: ## Estado dos containers
	$(COMPOSE) ps -a

migrate: deploy/.env ## Aplica as migrações pendentes
	$(COMPOSE) run --rm --build migrate

test: ## Testes e checagens: api (go), web (lint e tipos), worker (sintaxe)
	cd api && go vet ./... && go test ./...
	cd web && npm run lint && npx next typegen && npx tsc --noEmit
	cd workers/canopus && for f in src/*.js; do node --check "$$f" || exit 1; done
	sh -n workers/canopus/docker-entrypoint.sh

worker-dry-run: deploy/.env ## Dry-run de 1 cota no container do worker (não confirma lance)
	mkdir -p deploy/data/canopus
	$(COMPOSE) --profile canopus run --rm --build worker-canopus
	@echo "Screenshots em deploy/data/canopus/screenshots/"

deploy/.env:
	@umask 077 && sed "s/troque-esta-senha/$$(openssl rand -hex 24)/" deploy/.env.example > $@
	@echo "Criado deploy/.env com senha aleatória do Postgres"
