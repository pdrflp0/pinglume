.PHONY: build web test vet clean dev-server dev-agent

STATIC := agent/internal/webui/static

build: web
	cd agent && go build -o bin/pinglume ./cmd/pinglume
	cd probe-server && go build -o bin/probe-server .

web:
	cd web && npm ci --no-audit --no-fund
	find $(STATIC) -mindepth 1 ! -name README -exec rm -rf {} +
	cd web && npm run build

test:
	cd agent && go test -race -count=1 ./...
	cd probe-server && go test -race -count=1 ./...
	cd web && npm run typecheck && npm test

vet:
	cd agent && go vet ./...
	cd probe-server && go vet ./...

clean:
	rm -rf agent/bin probe-server/bin
	find $(STATIC) -mindepth 1 ! -name README -exec rm -rf {} +

dev-server:
	probe-server/bin/probe-server -tcp 127.0.0.1:9443 -dns 127.0.0.1:15353 -admin 127.0.0.1:9900

dev-agent:
	agent/bin/pinglume run --data-dir .dev-data \
		--dev-target ep-1=127.0.0.1:9443 --dev-dns ep-1=127.0.0.1:15353 \
		--dev-ca testbed/tls/probe-server.pem
