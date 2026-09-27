# Web vận hành AgriGreen — build nhiều tầng, runtime tối thiểu, chạy không phải root.
# Build context: apps/web-ops
#   docker build -t agrigreen/web-ops \
#     --build-arg NEXT_PUBLIC_API_BASE_URL=https://api.agrigreen.vn \
#     --build-arg NEXT_PUBLIC_OIDC_ISSUER=https://auth.agrigreen.vn/realms/agrigreen \
#     apps/web-ops
# Biến NEXT_PUBLIC_* được nhúng lúc build → mỗi môi trường một image.

FROM node:24-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
# --ignore-scripts: không chạy postinstall của gói phụ thuộc trong image.
RUN npm ci --ignore-scripts --no-audit --no-fund

FROM node:24-alpine AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
ARG NEXT_PUBLIC_API_BASE_URL=http://localhost:8080
ARG NEXT_PUBLIC_ADMIN_API_BASE_URL=
ARG NEXT_PUBLIC_OIDC_ISSUER=http://localhost:8081/realms/agrigreen
ARG NEXT_PUBLIC_OIDC_CLIENT_ID=agrigreen-web-ops
ARG NEXT_PUBLIC_MAP_STYLE_URL=
ARG NEXT_PUBLIC_API_MOCK=0
ENV NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL \
    NEXT_PUBLIC_ADMIN_API_BASE_URL=$NEXT_PUBLIC_ADMIN_API_BASE_URL \
    NEXT_PUBLIC_OIDC_ISSUER=$NEXT_PUBLIC_OIDC_ISSUER \
    NEXT_PUBLIC_OIDC_CLIENT_ID=$NEXT_PUBLIC_OIDC_CLIENT_ID \
    NEXT_PUBLIC_MAP_STYLE_URL=$NEXT_PUBLIC_MAP_STYLE_URL \
    NEXT_PUBLIC_API_MOCK=$NEXT_PUBLIC_API_MOCK
COPY --from=deps /app/node_modules ./node_modules
COPY . .
# prebuild chép worker MapLibre vào public/maplibre rồi next build (output: standalone).
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
