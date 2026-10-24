# Một Dockerfile cho mọi lệnh trong cmd/: docker build --build-arg CMD=api-platform .
# Image tối giản, non-root, hệ tệp gốc chỉ đọc khi chạy (mục 18).
ARG GO_VERSION=1.26.10

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG CMD
RUN test -n "$CMD" && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${CMD}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
# Commit của image (CI: --build-arg AG_BUILD_SHA=${{ github.sha }}); api-platform trả ở /healthz. Đặt SAU COPY để
# đổi commit chỉ thêm một lớp metadata.
ARG AG_BUILD_SHA=
ENV AG_BUILD_SHA=$AG_BUILD_SHA
USER nonroot:nonroot
ENTRYPOINT ["/app"]
