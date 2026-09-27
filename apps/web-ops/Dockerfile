# Web váº­n hÃ nh AgriGreen â€” build nhiá»u táº§ng, runtime tá»‘i thiá»ƒu, cháº¡y khÃ´ng pháº£i root.
# Build context: apps/web-ops
#   docker build -t agrigreen/web-ops \
#     --build-arg NEXT_PUBLIC_API_BASE_URL=https://api.agrigreen.vn \
#     --build-arg NEXT_PUBLIC_OIDC_ISSUER=https://auth.agrigreen.vn/realms/agrigreen \
#     apps/web-ops
# Biáº¿n NEXT_PUBLIC_* Ä‘Æ°á»£c nhÃºng lÃºc build â†’ má»—i mÃ´i trÆ°á»ng má»™t image.

FROM node:24-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
# --ignore-scripts: khÃ´ng cháº¡y postinstall cá»§a gÃ³i phá»¥ thuá»™c trong image.
RUN npm ci --ignore-scripts --no-audit --no-fund

FROM node:24-alpine AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
ARG NEXT_PUBLIC_API_BASE_URL=http://localhost:8080
ARG NEXT_PUBLIC_ADMIN_API_BASE=
ARG NEXT_PUBLIC_OIDC_ISSUER=http://localhost:8081/realms/agrigreen
ARG NEXT_PUBLIC_OIDC_CLIENT_ID=agrigreen-web-ops
ARG NEXT_PUBLIC_MAP_STYLE_URL=
ARG NEXT_PUBLIC_API_MOCK=0
ENV NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL \
    NEXT_PUBLIC_ADMIN_API_BASE=$NEXT_PUBLIC_ADMIN_API_BASE \
    NEXT_PUBLIC_OIDC_ISSUER=$NEXT_PUBLIC_OIDC_ISSUER \
    NEXT_PUBLIC_OIDC_CLIENT_ID=$NEXT_PUBLIC_OIDC_CLIENT_ID \
    NEXT_PUBLIC_MAP_STYLE_URL=$NEXT_PUBLIC_MAP_STYLE_URL \
    NEXT_PUBLIC_API_MOCK=$NEXT_PUBLIC_API_MOCK
COPY --from=deps /app/node_modules ./node_modules
COPY . .
# prebuild chÃ©p worker MapLibre vÃ o public/maplibre rá»“i next build (output: standalone).
RUN npm run build

FROM node:24-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0
COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build --chown=node:node /app/.next/static ./.next/static
COPY --from=build --chown=node:node /app/public ./public
USER node
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:3000/login || exit 1
CMD ["node", "server.js"]
