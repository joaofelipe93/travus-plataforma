# Produção (Etapa 4)

Roteiro para colocar a Travus Plataforma numa VM e mantê-la. Decisões do usuário:

- **VM**: DigitalOcean `s-2vcpu-2gb` (2 vCPU, 2 GB de RAM, 60 GB), Ubuntu 24.04, **dedicada** (nunca a `appairbnb`). Parada, a plataforma usa menos de 200 MB; os picos são o Chromium num dry-run (0,5 a 1 GB) e o build no deploy (o `publicar.sh` compila uma imagem por vez), com os 2 GB de swap do `preparar.sh` de folga. Se apertar, redimensione para `s-2vcpu-4gb` (seção 1). O Newcon aceitou login a partir de IP da DigitalOcean (teste de 14/09/2026).
- **Domínio**: a definir (`app.<domínio>` e `api.<domínio>`).
- **Backup do Postgres**: na própria VM, com cópia manual para fora (`make backup-baixar`).
- **Alertas**: e-mail (vigia da API) + monitor externo.
- **Notificador de check-in (WhatsApp)**: sai da VM `appairbnb` e passa a rodar nesta VM, com fila e eventos no mesmo Postgres. O número é **pareado de novo** pela tela WhatsApp (sem copiar a sessão nem o histórico da `appairbnb`). Roteiro na seção 9.

Nada disso roda sem pedido: criar a VM, mexer no DNS, publicar e ligar o lance real são decisões do usuário, na hora.

## Como está montado

```
sua máquina                                  VM (/opt/travus)
make deploy VM=travus@<ip>  ── git archive ─► releases/<commit>/        (código, sem segredos)
                            ── ssh ─────────► deploy/vm/publicar.sh <commit>
                                                ├─ compartilhado/.env         (deploy/.env de produção)
                                                ├─ compartilhado/canopus.env  (credenciais do Newcon)
                                                ├─ docker compose -f docker-compose.yml -f docker-compose.prod.yml up
                                                └─ atual → releases/<commit>
                                              backups/ (diario, semanal, mensal)
```

- `deploy/docker-compose.prod.yml` vai por cima do compose local: Traefik nas portas 80 e 443, com o HTTP redirecionando; certificados do Let's Encrypt (`deploy/traefik/traefik.producao.yml`); HSTS; dashboard fechado; backup diário ligado; vigia com a pasta de backups e o certificado.
- O deploy publica **só o que está commitado** (sem push para o GitHub) e **recusa** publicar com execução em andamento (`FORCAR=1` passa por cima).
- Os segredos ficam só na VM, em `/opt/travus/compartilhado`, e são ligados a cada versão por link simbólico.

## 1. Criar a VM (usuário)

1. Droplet **Basic, Regular, `s-2vcpu-2gb`** (2 vCPU, 2 GB, 60 GB), **Ubuntu 24.04 LTS**, autenticação **por chave SSH** (sua chave pública), nome `travus-producao`. Não precisa de script de inicialização (user data): as atualizações e o resto vêm do `preparar.sh` (seção 2).
   - **Aumentar depois**: desligue a VM (sem execução em andamento), Resize → **CPU and RAM only** → `s-2vcpu-4gb` → ligue. Dá para voltar. **Não** marque o aumento de disco: esse não volta atrás.
2. Opcional e pago: backups semanais da própria DigitalOcean. Os backups do Postgres ficam na mesma VM; se ela se perder, só sobra a última cópia baixada.
3. Anote o IP. **Não use nem altere a VM `appairbnb`**: o `preparar.sh` para se encontrar o usuário `checkin` ou o PM2. Ela continua mandando as notificações até a seção 9, e quem a desliga é você.

## 2. Preparar a VM

Na sua máquina, uma vez:

```bash
ssh root@<ip> 'bash -s' < deploy/vm/preparar.sh
ssh travus@<ip>        # a partir daqui, root e senha estão bloqueados no SSH
```

Para repetir o script depois (ele não estraga o que já foi feito): `ssh travus@<ip> 'sudo bash -s' < deploy/vm/preparar.sh`. Se ao fim houver `/var/run/reboot-required` (o `upgrade` costuma trazer kernel novo), reinicie agora, com a VM ainda vazia: `ssh travus@<ip> sudo systemctl reboot`.

O script faz `apt-get update` e `apt-get upgrade` e instala Docker (repositório oficial, logs com rotação), firewall (22, 80, 443), fail2ban, atualizações automáticas de segurança (`unattended-upgrades`, todo dia), swap de 2 GB, fuso `America/Sao_Paulo`, o usuário `travus` (sudo, só chave) e as pastas em `/opt/travus`.

As atualizações automáticas **não reiniciam a VM** de propósito: um reinício no meio de um dry-run ou de um lance real derrubaria o worker. Atualização de kernel fica pendente até o reinício manual (ver Rotina).

Rede até o Newcon a partir da VM (sem login):

```bash
scp -r tools/teste-ip-vm travus@<ip>:/tmp/ && ssh travus@<ip> 'bash /tmp/teste-ip-vm/teste-ip.sh --sem-login'
```

## 3. DNS (usuário)

Dois registros do tipo **A**, TTL 300: `app.<domínio>` e `api.<domínio>` → IP da VM. Confira com `dig +short app.<domínio>` antes do primeiro deploy (o Let's Encrypt precisa alcançar a VM pela porta 80).

## 4. Segredos na VM

**Credenciais do Newcon do worker** (só as três variáveis; a URL com essa caixa exata):

```bash
ssh travus@<ip> 'umask 077; cat > /opt/travus/compartilhado/canopus.env'
# cole, depois Ctrl+D:
# NEWCON_URL=https://cnp3.consorciocanopus.com.br/WWW/frmCorCcCnsLogin.aspx
# NEWCON_USER=...
# NEWCON_PASS=...
```

**deploy/.env de produção**: o primeiro `make deploy` cria `/opt/travus/compartilhado/.env` a partir de `deploy/.env.producao.example`, com senha do Postgres, `WORKER_TOKEN` e `CHAVE_CRIPTOGRAFIA` novos, e para. A cada deploy, o `publicar.sh` acrescenta os segredos que faltarem sem trocar os existentes (`CHECKIN_WEBHOOK_SECRET`, `CHECKIN_ADMIN_TOKEN`, `CHECKIN_DB_SENHA`). Então:

```bash
ssh -t travus@<ip> 'nano /opt/travus/compartilhado/.env'
```

- `DOMINIO_APP`, `DOMINIO_API`;
- `CERT_RESOLVER=le-teste` no primeiro deploy (certificado de teste, sem limite de emissões);
- `SMTP_*`, `ALERTA_DE`, `ALERTA_PARA` (Gmail: `smtp.gmail.com`, porta 587, senha de app);
- `LANCE_REAL_HABILITADO=false`;
- **copie a `CHAVE_CRIPTOGRAFIA` para o seu gerenciador de senhas**: sem ela, o token do Google no banco não decifra.

## 5. Primeiro deploy

```bash
make deploy VM=travus@<ip>                                                   # cria o .env e para
make deploy VM=travus@<ip>                                                   # sobe tudo
make smoke-producao APP=app.<domínio> API=api.<domínio> INSEGURO=1            # certificado de teste
```

Com tudo verde, troque `CERT_RESOLVER=le` no `.env` da VM e publique de novo (o mesmo commit serve):

```bash
make deploy VM=travus@<ip>
make smoke-producao APP=app.<domínio> API=api.<domínio>                       # certificado de verdade
```

**O Traefik não troca sozinho o certificado de teste pelo de verdade**: enquanto tiver um certificado válido para o domínio (o de teste, em `acme-teste.json`), ele não pede outro, e o `acme.json` fica vazio. Depois de publicar com `CERT_RESOLVER=le`, apague os de teste e reinicie o Traefik (alguns segundos fora do ar):

```bash
ssh travus@<ip> 'docker exec travus-traefik-1 rm -f /letsencrypt/acme-teste.json && cd /opt/travus/atual && docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.prod.yml restart traefik'
```

Confira o emissor (sem `STAGING`) e só então rode o smoke: `echo | openssl s_client -connect <ip>:443 -servername app.<domínio> 2>/dev/null | openssl x509 -noout -issuer`.

Se o DNS da sua rede ainda guarda um IP antigo (troca recente de registro), o smoke falha por ir ao lugar errado. Rode-o de um container com os domínios fixados no IP da VM:

```bash
docker run --rm --add-host app.<domínio>:<ip> --add-host api.<domínio>:<ip> -v "$PWD/tools/smoke:/smoke:ro" debian:bookworm-slim \
  bash -c 'apt-get update -qq >/dev/null && apt-get install -y -qq curl openssl ca-certificates >/dev/null && bash /smoke/producao.sh app.<domínio> api.<domínio>'
```

## 6. Usuários, planilha, Drive e alertas

Comandos na VM rodam dentro da versão atual:

```bash
ssh -t travus@<ip> 'cd /opt/travus/atual && make usuario args="criar --email ana@exemplo.com --nome \"Ana\" --perfil admin"'
ssh -t travus@<ip> 'cd /opt/travus/atual && make alerta-teste'
```

- **Cadastro**: a produção começa com o banco vazio. Cadastre os clientes e as cotas pela tela (Canopus → Clientes → Novo cliente, `/canopus/clientes/novo`).
- **Google Drive** (opcional): preencha `GOOGLE_*` no `.env`, publique e importe o token:
  ```bash
  scp workers/canopus/token.json travus@<ip>:/opt/travus/atual/workers/canopus/token.json
  ssh -t travus@<ip> 'cd /opt/travus/atual && make google-token && rm workers/canopus/token.json && make google-status'
  ```

## 7. Monitor externo

O vigia só avisa enquanto a API está de pé. Para a VM inteira fora do ar:

1. **healthchecks.io** (grátis): crie um check com período de 5 min e tolerância de 10 min, integração por e-mail; copie a URL de ping para `VIGIA_PING_URL` e publique. O vigia faz o ping a cada 5 min quando a API e o banco respondem; se os pings param, o healthchecks.io avisa.
2. **UptimeRobot** (grátis, opcional, recomendado): monitores HTTP de 5 min para `https://api.<domínio>/health` e `https://app.<domínio>/login` (cobre Traefik e web).

## 8. Primeiro dry-run a partir da VM

Só com o ok do usuário, e com **nenhuma outra sessão do Newcon** aberta (script antigo parado). Crie um dry-run de 1 cota pela tela e acompanhe. O lance real continua desligado.

## 9. Notificador de check-in: migrar da `appairbnb`

O notificador (`workers/checkin-whatsapp`) sobe junto com a plataforma, desde o primeiro deploy, e fica esperando o pareamento: sem sessão do WhatsApp nem grupo, os webhooks que chegarem são gravados e nada é enviado. A `appairbnb` continua de pé até o passo 7, e **só ela recebe os webhooks do PMS até o passo 5**: não há mensagem em dobro no grupo.

Só com o ok do usuário, com a plataforma já no ar pela seção 5 e um usuário admin criado.

1. **Parear** (admin, pela tela): `https://app.<domínio>/reservas/whatsapp` (Reservas → WhatsApp) → aparece o QR. No celular do **chip dedicado** (o mesmo da `appairbnb`): WhatsApp → Configurações → Dispositivos conectados → Conectar dispositivo → escaneie. A tela mostra "conectado como +55 …". O aparelho novo aparece no celular como **Travus Plataforma**; o da `appairbnb` continua lá como **Check-in Notifier** (cada um tem a própria sessão, os dois funcionam ao mesmo tempo).
2. **Grupo**: "Escolher grupo" → o mesmo grupo que recebe as notificações hoje → "Usar este grupo". Se ele não aparece na lista, o número não participa do grupo.
3. **Teste**: "Enviar mensagem de teste" (todos do grupo recebem; avise antes).
4. **Token do webhook** (novo, diferente do da `appairbnb`):
   ```bash
   ssh travus@<ip> 'grep ^CHECKIN_WEBHOOK_SECRET= /opt/travus/compartilhado/.env'
   ```
5. **Trocar no PMS** (usuário): URL do webhook `https://api.<domínio>/webhooks/nova-reserva`, cabeçalho `x-webhook-token: <token do passo 4>` (ou `Authorization: Bearer <token>`), o mesmo formato de hoje. Só `POST` nessa rota chega ao notificador.
   - **Não reprocesse reservas antigas no PMS logo depois da troca**: o banco novo não conhece as que a `appairbnb` já notificou e mandaria de novo ao grupo.
6. **Conferir** na próxima reserva ou cancelamento real: a mensagem chega **uma vez** no grupo e a tela WhatsApp mostra "Enviadas: 1". Se algo der errado, volte a URL e o token antigos no PMS: a `appairbnb` ainda está de pé.
7. **Desligar a `appairbnb`** (usuário, depois de alguns dias sem problema): pare o notificador antigo na `appairbnb` (instalada pelo `bootstrap-vps.sh` do projeto airbnb: `sudo -iu checkin pm2 stop checkin-notifier && sudo -iu checkin pm2 save`) e, no celular, em Dispositivos conectados, **remova o "Check-in Notifier"** (nunca o "Travus Plataforma"). Destruir a VM é decisão sua, depois; o `data/` dela tem o histórico antigo de reservas.

Depois da migração:

- **Sessão caiu** (removida no celular, número trocado, conexão travada): o vigia avisa em 10 min com o link da tela; um admin escaneia o QR de novo. "Desconectar e gerar novo QR" na tela força uma sessão nova (as mensagens esperam na fila).
- **Trocar o token do webhook**: apague a linha `CHECKIN_WEBHOOK_SECRET` do `.env` da VM, publique (o `publicar.sh` gera outro) e atualize o PMS.
- **Backup**: eventos, reservas, resumos e fila estão no Postgres (`checkin.eventos`, `checkin.reservas`, `checkin.resumos`, `checkin.mensagens`) e entram no dump; os payloads têm nome, telefone e e-mail de hóspedes. A sessão do WhatsApp fica no volume `travus_checkin-dados` e **não** tem backup: perdida a VM, pareie de novo.

## 10. Deploy pelo GitHub (padrão)

Depois do primeiro deploy manual (seções 1 a 5), as versões seguintes vão pelo GitHub:

1. Os PRs entram na `main` (CI verde). O release-please mantém o PR **"chore: versão X.Y.Z"** com o `CHANGELOG.md`.
2. **Você faz o merge do PR de versão** quando quiser lançar. Isso cria a tag `vX.Y.Z` e o release.
3. O workflow **Deploy** compila as imagens da tag e publica no GHCR (job "Imagens da versão").
4. **Na primeira vez, e sempre que surgir um pacote novo**: em https://github.com/joaofelipe93?tab=packages, abra cada `travus-*` → Package settings → Change visibility → **Public**. A VM baixa sem login; as imagens não têm segredos.
5. O job **Produção** fica esperando: em Actions → Deploy → **Review deployments** → marque `producao` → **Approve and deploy**.
6. A VM recusa se houver execução em andamento (tente de novo depois), baixa as imagens, faz backup, migra e sobe. O workflow roda o smoke de produção e confere a versão no `/health`. Se algo falhar depois de começar a troca, volta sozinho para a versão anterior e o job fica vermelho.

Voltar à mão: Actions → **Voltar versão** → Run workflow → digite `VOLTAR` → aprovar. Para uma versão específica: Actions → **Deploy** → Run workflow → `vX.Y.Z` (precisa já ter release).

Configuração (já feita em 17/09/2026; refazer só se trocar de VM ou de chave):

```bash
ssh-keygen -t ed25519 -N "" -C travus-deploy-github-actions -f /tmp/deploy
make configurar-ci VM=travus@<ip> CHAVE=/tmp/deploy.pub     # entrada-ci.sh + chave restrita
gh secret set DEPLOY_SSH_KEY --env producao < /tmp/deploy
ssh-keygen -F <ip> | grep -v '^#' | gh secret set DEPLOY_KNOWN_HOSTS --env producao
gh variable set DEPLOY_HOST --env producao --body <ip>
rm /tmp/deploy /tmp/deploy.pub
```

Para revogar a chave: apague a linha `restrict,command="/opt/travus/bin/entrada-ci.sh" …` do `~/.ssh/authorized_keys` do `travus` na VM.

## Rotina

| Tarefa | Como |
|---|---|
| Publicar | merge do PR de versão → aprovar o job Produção no GitHub (seção 10). Plano B: `make deploy VM=travus@<ip>` |
| Voltar uma versão | workflow **Voltar versão** no GitHub, ou `make deploy-voltar VM=travus@<ip>`. **Migrações não são desfeitas** (backup antes de cada deploy em `backups/antes-do-deploy`) |
| Logs | `ssh -t travus@<ip> 'cd /opt/travus/atual && make logs s=api'` (api, worker-canopus, checkin-whatsapp, traefik, backup…) |
| WhatsApp do notificador | tela `https://app.<domínio>/reservas/whatsapp` (só admin): conexão, QR, grupo, teste, fila |
| Estado | `ssh travus@<ip> 'cd /opt/travus/atual && make ps'` |
| Cópia do backup | `make backup-baixar VM=travus@<ip>` (semanal; vai para `deploy/backups/`, fora do git; tem dados de clientes) |
| Testar a restauração | `ssh -t travus@<ip> 'cd /opt/travus/atual && make restaurar-teste'` (mensal) |
| Atualizações do sistema | as de segurança entram sozinhas. Uma vez por mês: `ssh travus@<ip> 'cat /var/run/reboot-required 2>/dev/null \|\| echo nada pendente'`; se pedir reinício, confira que não há execução em andamento (tela Execuções) e `ssh travus@<ip> sudo reboot` (os containers voltam sozinhos) |
| Backup fora de hora | `ssh -t travus@<ip> 'cd /opt/travus/atual && make backup'` |

Na VM, `make up`, `make prod-local` e `make dev-web` se recusam a rodar: a stack de produção sobe só pelo `publicar.sh`.

### Backup

- `backup` (container `postgres:18.6-alpine`) faz `pg_dump -Fc` todo dia a partir de `BACKUP_HORA` (3 h) em `BACKUP_DIR` (`/opt/travus/backups`), confere o arquivo com `pg_restore -l` e guarda 7 diários, 4 semanais (domingo) e 6 mensais (dia 1). Os PDFs de comprovante estão no banco, então entram no dump.
- `ultimo-ok` e `ultimo-erro` são lidos pelo vigia.
- **Restaurar de verdade** (VM nova ou banco perdido), com a mesma `CHAVE_CRIPTOGRAFIA` no `.env`:
  ```bash
  cd /opt/travus/atual
  P="docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.prod.yml"
  $P stop web api worker-canopus
  $P exec -T postgres sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists --no-owner' < travus-AAAA-MM-DD.dump
  $P up -d --wait
  ```
  O `make restaurar-teste` confere que o dump restaura num banco temporário; a restauração por cima do banco em uso ainda não foi ensaiada.

### O que o vigia avisa (e-mail, a cada 5 min, só quando muda; lembrete a cada 12 h)

- banco inacessível; worker sem contato com a API há mais de 10 min;
- execução na fila há mais de 30 min, ou em andamento sem sinal do worker há mais de 5 min;
- **cota em `erro_apos_confirmar`** (clicou em Confirmar sem resultado: conferir no Histórico);
- comprovante que não foi para o Drive depois de 5 tentativas, ou esperando há mais de 1 h;
- backup atrasado (mais de 26 h) ou com erro; disco acima de 80%;
- certificado HTTPS vencendo em menos de 14 dias ou não verificável (com `CERT_RESOLVER=le-teste`, esse alerta é esperado);
- **notificador de check-in** (`VIGIA_CHECKIN=true`): serviço sem resposta ou WhatsApp desconectado/esperando QR há mais de 10 min; conectado sem grupo escolhido; mensagens que desistiram nas últimas 24 h; mensagens paradas há mais de 30 min com o WhatsApp conectado. Antes do passo 1 da seção 9, o alerta "esperando a leitura do QR" é esperado.

Os e-mails não levam nome de cliente nem de hóspede, só grupo, cota e versão.

### Lance real em produção

`LANCE_REAL_HABILITADO=false` sempre, a não ser com pedido explícito do usuário, na hora: trocar no `.env` da VM, publicar, fazer o lance, voltar para `false` e publicar de novo.
