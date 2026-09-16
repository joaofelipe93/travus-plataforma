# Atalhos do ambiente local e da produção. `make` sozinho lista os comandos.
COMPOSE := docker compose -f deploy/docker-compose.yml
PROD := $(COMPOSE) -f deploy/docker-compose.prod.yml

# O worker legado e o backup rodam com o uid/gid do host (arquivos em deploy/data e deploy/backups).
export HOST_UID := $(shell id -u)
export HOST_GID := $(shell id -g)

# O Traefik leva alguns segundos para ligar as rotas depois que o container fica saudável.
ESPERAR_GATEWAY = for i in $$(seq 30); do [ "$$(curl -s -o /dev/null -w '%{http_code}' http://app.localhost/login)" = 200 ] && exit 0; sleep 1; done; echo "aviso: app.localhost/login ainda não responde 200 (veja make logs s=traefik)"

# VM de produção (deploy, deploy-voltar, backup-baixar): make deploy VM=travus@<ip-ou-host>
VM ?=
PRECISA_VM = @test -n "$(VM)" || { echo "informe a VM: make $@ VM=travus@<ip-ou-host>"; exit 1; }

# Na VM de produção, a stack sobe só pelo publicar.sh (HTTPS, dashboard fechado): make up subiria
# o compose local por cima.
FORA_DA_VM = @test ! -d /opt/travus/compartilhado || { echo "esta é a VM de produção: publique com make deploy (na sua máquina), não com make $@"; exit 1; }

.DEFAULT_GOAL := help
.PHONY: help env up down checkin logs ps migrate usuario google-token google-status test smoke dev-web sqlc paridade-csv worker-dry-run \
	backup restaurar-teste alerta-teste prod-local smoke-producao deploy deploy-voltar backup-baixar

help: ## Lista os comandos
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  make %-16s %s\n", $$1, $$2}'

up: env ## Sobe traefik, postgres, migrações, api, web e worker (o worker só acessa o Newcon quando alguém inicia uma execução)
	$(FORA_DA_VM)
	$(COMPOSE) up -d --build --wait
	@$(ESPERAR_GATEWAY)
	@echo "Pronto: http://app.localhost  |  http://api.localhost/health  |  http://traefik.localhost"

down: ## Derruba os containers (o banco fica no volume)
	$(COMPOSE) --profile canopus --profile backup --profile checkin down

checkin: env ## Sobe também o notificador de check-in (conecta ao WhatsApp e mostra QR; NÃO pareie o número da produção aqui)
	$(FORA_DA_VM)
	$(COMPOSE) --profile checkin up -d --build --wait
	@$(ESPERAR_GATEWAY)
	@echo "Notificador local no ar: make logs s=checkin-whatsapp (make down derruba)"

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

alerta-teste: env ## Envia um e-mail de teste com SMTP_* e ALERTA_* do deploy/.env
	$(COMPOSE) --profile cli run --rm -T --build cli alerta testar

backup: env ## Backup do Postgres agora, em deploy/backups (tem dados de clientes: fica fora do git)
	@mkdir -p deploy/backups && chmod 700 deploy/backups
	$(COMPOSE) --profile backup run --rm backup agora

restaurar-teste: env ## Restaura o último backup num banco temporário e compara com o banco em uso
	$(COMPOSE) --profile backup run --rm backup testar-restauracao

test: env ## Testes: api e checkin-whatsapp (com Postgres), web (lint e tipos), worker Canopus (node:test, sem Newcon), scripts (sintaxe)
	$(COMPOSE) up -d --wait postgres
	$(COMPOSE) --profile teste run --rm api-teste
	cd web && npm run lint && npx next typegen && npx tsc --noEmit
	cd workers/canopus && for f in src/*.js; do node --check "$$f" || exit 1; done && npm test
	$(COMPOSE) --profile teste run --rm --build checkin-teste
	sh -n workers/canopus/docker-entrypoint.sh
	sh -n deploy/backup/backup.sh
	for f in deploy/vm/*.sh tools/smoke/*.sh; do bash -n "$$f" || exit 1; done
	DOMINIO_APP=app.exemplo.com.br DOMINIO_API=api.exemplo.com.br BACKUP_DIR=/tmp $(PROD) config --quiet

smoke: ## Checagens pelo gateway com curl (precisa de make up)
	bash tools/smoke/etapa1.sh

dev-web: env ## Web em modo desenvolvimento (next dev) atrás do Traefik; make up volta ao normal
	$(FORA_DA_VM)
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

# ---------------------------------------------------------------- produção

prod-local: env ## Modo produção nesta máquina (HTTPS com certificado não confiável); volte com make down e make up
	$(FORA_DA_VM)
	@mkdir -p deploy/backups && chmod 700 deploy/backups
	DOMINIO_APP=app.localhost DOMINIO_API=api.localhost CERT_RESOLVER=le-teste BACKUP_DIR=$(CURDIR)/deploy/backups \
		$(PROD) up -d --build --wait --remove-orphans
	@for i in $$(seq 30); do [ "$$(curl -sk -o /dev/null -w '%{http_code}' https://app.localhost/login)" = 200 ] && exit 0; sleep 1; done; echo "aviso: https://app.localhost/login ainda não responde 200"
	@echo "Modo produção local: https://app.localhost. Checagens: make smoke-producao APP=app.localhost API=api.localhost INSEGURO=1"

smoke-producao: ## Checagens HTTPS de fora: make smoke-producao APP=app.<domínio> API=api.<domínio> (INSEGURO=1 com certificado de teste)
	@test -n "$(APP)" && test -n "$(API)" || { echo "uso: make smoke-producao APP=app.<domínio> API=api.<domínio>"; exit 1; }
	INSEGURO=$(INSEGURO) bash tools/smoke/producao.sh $(APP) $(API)

deploy: ## Publica o commit atual na VM: make deploy VM=travus@<ip-ou-host> (não publica com execução em andamento)
	$(PRECISA_VM)
	@git diff --quiet && git diff --cached --quiet || { echo "há mudanças sem commit: o deploy publica só o que está commitado"; exit 1; }
	@versao=$$(git rev-parse --short=12 HEAD); \
	echo "Publicando $$versao em $(VM)"; \
	git archive --format=tar HEAD | ssh $(VM) "mkdir -p /opt/travus/releases/$$versao && tar -x -C /opt/travus/releases/$$versao" && \
	ssh -t $(VM) "FORCAR=$(FORCAR) bash /opt/travus/releases/$$versao/deploy/vm/publicar.sh $$versao"

deploy-voltar: ## Volta a VM para a versão publicada antes: make deploy-voltar VM=travus@<ip-ou-host>
	$(PRECISA_VM)
	ssh -t $(VM) "FORCAR=$(FORCAR) bash /opt/travus/atual/deploy/vm/publicar.sh --voltar"

backup-baixar: ## Copia o último backup da VM para deploy/backups: make backup-baixar VM=travus@<ip-ou-host>
	$(PRECISA_VM)
	@mkdir -p deploy/backups && chmod 700 deploy/backups
	@ultimo=$$(ssh $(VM) 'ls -1 /opt/travus/backups/diario/travus-*.dump 2>/dev/null | sort -r | head -n 1'); \
	test -n "$$ultimo" || { echo "a VM ainda não tem backup"; exit 1; }; \
	scp -p "$(VM):$$ultimo" deploy/backups/ && \
	echo "Copiado para deploy/backups/$$(basename "$$ultimo") (tem dados de clientes: guarde com cuidado)"

# Garante deploy/.env com senha do Postgres, WORKER_TOKEN, CHAVE_CRIPTOGRAFIA e os segredos do notificador
# de check-in (acrescenta o que faltar).
env: deploy/.env
	@grep -q '^WORKER_TOKEN=' deploy/.env || { umask 077; echo "WORKER_TOKEN=$$(openssl rand -hex 32)" >> deploy/.env; echo "Acrescentado WORKER_TOKEN ao deploy/.env"; }
	@grep -q '^CHECKIN_WEBHOOK_SECRET=' deploy/.env || { umask 077; echo "CHECKIN_WEBHOOK_SECRET=$$(openssl rand -hex 32)" >> deploy/.env; echo "Acrescentado CHECKIN_WEBHOOK_SECRET ao deploy/.env"; }
	@grep -q '^CHECKIN_ADMIN_TOKEN=' deploy/.env || { umask 077; echo "CHECKIN_ADMIN_TOKEN=$$(openssl rand -hex 32)" >> deploy/.env; echo "Acrescentado CHECKIN_ADMIN_TOKEN ao deploy/.env"; }
	@grep -q '^CHECKIN_DB_SENHA=' deploy/.env || { umask 077; echo "CHECKIN_DB_SENHA=$$(openssl rand -hex 32)" >> deploy/.env; echo "Acrescentada CHECKIN_DB_SENHA ao deploy/.env (aplique com make migrate ou make up)"; }
	@grep -q '^CHAVE_CRIPTOGRAFIA=' deploy/.env || { umask 077; echo "CHAVE_CRIPTOGRAFIA=$$(openssl rand -hex 32)" >> deploy/.env; echo "Acrescentada CHAVE_CRIPTOGRAFIA ao deploy/.env (guarde com o backup do banco: sem ela, o token do Google não decifra)"; }

deploy/.env:
	@umask 077 && sed "s/troque-esta-senha/$$(openssl rand -hex 24)/" deploy/.env.example > $@
	@echo "Criado deploy/.env com senha aleatória do Postgres"
