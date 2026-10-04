# Lệnh thường dùng (Linux/macOS/CI). Trên Windows: xem docs/dev-setup.md.
.PHONY: all fmt vet lint gates test test-unit build compose-up compose-down verify-db k8s-check

all: fmt vet gates test

fmt:
	gofmt -w $$(git ls-files '*.go' | grep -v /testdata/)

vet:
	go vet ./...

lint:
	golangci-lint run ./...

# Cổng kiến trúc (mục 20). agmodgraph (đồ thị module.yaml, W1.2) chạy trong archlint.
gates:
	go run ./tools/modgen -check
	go run ./tools/archlint ./...
	go run ./tools/migratecheck migrations
	go run ./tools/contractcheck

# Cần AG_TEST_PG_ADMIN_URL và AG_DB_APP_PASSWORD; thêm AG_TEST_PGBOUNCER_URL để chạy R-32 qua PgBouncer.
test:
	go test -race -count=1 ./...

test-unit:
	go test -short ./...

build:
	for c in $$(ls cmd); do CGO_ENABLED=0 go build -trimpath -o bin/$$c ./cmd/$$c; done

compose-up:
	docker compose -f deploy/compose/docker-compose.yml up -d --build

compose-down:
	docker compose -f deploy/compose/docker-compose.yml down

verify-db:
	go run ./cmd/migrate verify

# G4 Kubernetes: helm lint + bảng luồng ADR-0008 (docs/runbooks/k8s-deploy.md mục 8). Windows: deploy/k8s/check.ps1
k8s-check:
	for e in dev staging prod; do helm lint --strict deploy/k8s/charts/agrigreen -f deploy/k8s/charts/agrigreen/values-$$e.yaml || exit 1; done
	helm lint --strict deploy/k8s/charts/agrigreen -f deploy/k8s/charts/agrigreen/values-prod.yaml -f deploy/k8s/charts/agrigreen/values-phasek.yaml
	python3 deploy/k8s/tests/check_flows.py
