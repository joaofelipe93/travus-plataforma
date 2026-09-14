# Imagem oficial do Playwright — já traz Node.js 20 e todos os deps do Chromium
FROM mcr.microsoft.com/playwright:v1.47.0-jammy

# Diretório de trabalho dentro do container
WORKDIR /app

# Só copia os manifests primeiro pra maximizar cache de build
COPY package.json package-lock.json* ./

# Instala dependências
RUN npm install --omit=dev --no-audit --no-fund

# Copia o resto do projeto
COPY src ./src
COPY README.md ./

# Cria as pastas de dados (serão volumes)
RUN mkdir -p /app/data/downloads /app/data/screenshots /app/data/logs

# Usuário não-root (a imagem playwright já traz "pwuser")
RUN chown -R pwuser:pwuser /app
USER pwuser

# Porta padrão para o callback OAuth. Só é usada quando você roda `npm run auth`.
EXPOSE 43117

# O modo é passado via CLI no `docker compose run`
CMD ["npm", "run", "dry-run"]
