.PHONY: build test vet clean dev-server dev-agent

build:
	cd agent && go build -o bin/pinglume ./cmd/pinglume
	cd probe-server && go build -o bin/probe-server .

test:
	cd agent && go test -race -count=1 ./...
	cd probe-server && go vet ./...

vet:
	cd agent && go vet ./...
	cd probe-server && go vet ./...

clean:
	rm -rf agent/bin probe-server/bin

# Testbed local: em um terminal `make dev-server`, em outro `make dev-agent`.
dev-server: build
	probe-server/bin/probe-server -tcp 127.0.0.1:9443

dev-agent: build
	agent/bin/pinglume run --data-dir .dev-data --dev-target ep-1=127.0.0.1:9443
