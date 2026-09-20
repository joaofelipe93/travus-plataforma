# syntax=docker/dockerfile:1
FROM node:26.8.2-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

FROM node:26.8.2-alpine AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
# Versão (version.txt) e commit, passados pelo compose; o Next embute no build (NEXT_PUBLIC_*).
ARG VERSAO=dev
ARG COMMIT=desconhecido
ENV NEXT_PUBLIC_VERSAO=${VERSAO} \
    NEXT_PUBLIC_COMMIT=${COMMIT}
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npm run build

# Só o servidor standalone (sem node_modules completo).
FROM node:26.8.2-alpine
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0
COPY --from=build --chown=node:node /app/public ./public
COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build --chown=node:node /app/.next/static ./.next/static
USER node
EXPOSE 3000
CMD ["node", "server.js"]
