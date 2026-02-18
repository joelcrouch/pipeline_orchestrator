.PHONY: sim-up sim-down build test lint proto-gen

sim-up:
	docker compose -f docker/docker-compose.yml up --build -d

sim-down:
	docker compose -f docker/docker-compose.yml down -v

build:
	cd control-plane && go build ./...

test:
	cd control-plane && go test ./...
	cd worker && python -m pytest

lint:
	cd control-plane && golangci-lint run
# 	cd worker && ruff check .

proto-gen:
	bash scripts/proto-gen.sh