#!/usr/bin/env bash
# Prepara uma VM Ubuntu 24.04 NOVA para a Travus Plataforma. Rode como root, uma vez
# (pode repetir sem estragar nada), a partir da sua máquina:
#
#   ssh root@<ip-da-vm> 'bash -s' < deploy/vm/preparar.sh
#
# Faz: usuário travus (SSH só por chave, sudo), root e senha bloqueados no SSH, firewall (22,
# 80, 443), fail2ban, atualizações automáticas de segurança, Docker oficial com rotação de
# logs, swap de 2 GB, fuso America/Sao_Paulo e as pastas em /opt/travus.
#
# NÃO rode na VM appairbnb (outra aplicação em produção): o script para se a encontrar.
set -euo pipefail

falhar() { echo "erro: $*" >&2; exit 1; }
passo() { printf '\n== %s\n' "$*"; }

[ "$(id -u)" = 0 ] || falhar "rode como root"
. /etc/os-release
[ "${ID:-}" = ubuntu ] || falhar "feito para Ubuntu (encontrado: ${ID:-desconhecido})"
if id checkin > /dev/null 2>&1 || command -v pm2 > /dev/null 2>&1; then
  falhar "esta VM tem o usuário checkin ou o PM2: parece ser a appairbnb. Parei sem mudar nada."
fi
[ -s /root/.ssh/authorized_keys ] || [ -s /home/travus/.ssh/authorized_keys ] ||
  falhar "nenhuma chave SSH em /root/.ssh/authorized_keys: sem ela, bloquear senha e root trancaria você para fora"

export DEBIAN_FRONTEND=noninteractive

passo "Pacotes e atualizações"
apt-get update -q
apt-get upgrade -y -q
apt-get install -y -q ca-certificates curl gnupg ufw fail2ban unattended-upgrades make openssl

passo "Fuso horário"
timedatectl set-timezone America/Sao_Paulo

passo "Swap de 2 GB"
if ! swapon --show=NAME --noheadings | grep -qx /swapfile; then
  [ -f /swapfile ] || { fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile > /dev/null; }
  swapon /swapfile
  grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
fi
swapon --show

passo "Docker (repositório oficial)"
if ! command -v docker > /dev/null 2>&1; then
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu ${VERSION_CODENAME} stable" \
    > /etc/apt/sources.list.d/docker.list
  apt-get update -q
  apt-get install -y -q docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi
mkdir -p /etc/docker
DAEMON='{"log-driver": "json-file", "log-opts": {"max-size": "10m", "max-file": "5"}}'
if [ "$(cat /etc/docker/daemon.json 2> /dev/null)" != "$DAEMON" ]; then
  echo "$DAEMON" > /etc/docker/daemon.json
  systemctl restart docker
fi
systemctl enable --now docker > /dev/null
docker compose version

passo "Usuário travus"
id travus > /dev/null 2>&1 || adduser --disabled-password --gecos "" travus
usermod -aG docker,sudo travus
echo 'travus ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/travus
chmod 440 /etc/sudoers.d/travus
visudo -cf /etc/sudoers.d/travus > /dev/null
install -d -m 700 -o travus -g travus /home/travus/.ssh
if [ ! -s /home/travus/.ssh/authorized_keys ]; then
  install -m 600 -o travus -g travus /root/.ssh/authorized_keys /home/travus/.ssh/authorized_keys
fi

passo "SSH: só chave, sem root"
cat > /etc/ssh/sshd_config.d/10-travus.conf << 'EOF'
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
EOF
sshd -t
systemctl reload ssh

passo "Firewall (22, 80, 443)"
ufw default deny incoming > /dev/null
ufw default allow outgoing > /dev/null
ufw allow OpenSSH > /dev/null
ufw allow 80/tcp > /dev/null
ufw allow 443/tcp > /dev/null
ufw --force enable > /dev/null
ufw status verbose

passo "fail2ban e atualizações automáticas"
cat > /etc/fail2ban/jail.d/sshd.local << 'EOF'
[sshd]
enabled = true
EOF
systemctl enable --now fail2ban > /dev/null
systemctl restart fail2ban
cat > /etc/apt/apt.conf.d/20auto-upgrades << 'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF

passo "Pastas da plataforma"
install -d -m 755 -o travus -g travus /opt/travus /opt/travus/releases /opt/travus/backups
install -d -m 700 -o travus -g travus /opt/travus/compartilhado

cat << 'EOF'

VM pronta. A partir de agora, entre como travus (root e senha estão bloqueados no SSH):
  ssh travus@<ip-da-vm>

Próximos passos (docs/producao.md):
  1. Registros DNS do tipo A: app.<domínio> e api.<domínio> apontando para esta VM.
  2. Credenciais do Newcon do worker em /opt/travus/compartilhado/canopus.env (chmod 600).
  3. Na sua máquina: make deploy VM=travus@<ip-da-vm>
EOF
