# Web nội bộ Mekong Green (vùng mkg) — build nhiều tầng, runtime tối thiểu, chạy không phải root.
# Build context: apps/web-internal
#   docker build -t agrigreen/web-internal \
#     --build-arg NEXT_PUBLIC_API_BASE_URL=https://mkg-internal.agrigreen.local \
#     --build-arg NEXT_PUBLIC_ADMIN_API_BASE=https://mkg-admin.agrigreen.local \
#     --build-arg NEXT_PUBLIC_OIDC_ISSUER=https://auth.agrigreen.vn/realms/agrigreen \
#     apps/web-internal
# Biến NEXT_PUBLIC_* được nhúng lúc build → mỗi môi trường một image. Chỉ gọi api-internal.

FROM node:24-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
# --ignore-scripts: không chạy postinstall của gói phụ thuộc trong image.
RUN npm ci --ignore-scripts --no-audit --no-fund

FROM node:24-alpine AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
# Không có mặc định localhost: bản production (NEXT_PUBLIC_DEPLOY_ENV mặc định) bắt buộc truyền đủ
# URL https, thiếu là `next build` dừng (src/deployEnv.ts). Compose cục bộ: NEXT_PUBLIC_DEPLOY_ENV=dev.
# Giả lập (NEXT_PUBLIC_API_MOCK=1) chỉ build được với NEXT_PUBLIC_DEPLOY_ENV=demo|dev.
ARG NEXT_PUBLIC_DEPLOY_ENV=production
ARG NEXT_PUBLIC_API_BASE_URL=
ARG NEXT_PUBLIC_ADMIN_API_BASE=
ARG NEXT_PUBLIC_OIDC_ISSUER=
ARG NEXT_PUBLIC_OIDC_CLIENT_ID=agrigreen-web-internal
ARG NEXT_PUBLIC_API_MOCK=0
ENV NEXT_PUBLIC_DEPLOY_ENV=$NEXT_PUBLIC_DEPLOY_ENV \
    NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL \
    NEXT_PUBLIC_ADMIN_API_BASE=$NEXT_PUBLIC_ADMIN_API_BASE \
    NEXT_PUBLIC_OIDC_ISSUER=$NEXT_PUBLIC_OIDC_ISSUER \
    NEXT_PUBLIC_OIDC_CLIENT_ID=$NEXT_PUBLIC_OIDC_CLIENT_ID \
    NEXT_PUBLIC_API_MOCK=$NEXT_PUBLIC_API_MOCK
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npm run build

FROM node:24-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3001 \
    HOSTNAME=0.0.0.0
COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build --chown=node:node /app/.next/static ./.next/static
COPY --from=build --chown=node:node /app/public ./public
USER node
EXPOSE 3001
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:3001/login || exit 1
CMD ["node", "server.js"]
