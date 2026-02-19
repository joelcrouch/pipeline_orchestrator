.PHONY: sim-up sim-down build test lint proto-gen

sim-up:
	docker compose -f docker/docker-compose.yml up --build -d

sim-down:
	docker compose -f docker/docker-compose.yml down -v --remove-orphans
	@echo "Cleaning stale bridge interfaces..."
	@for br in $$(ip link show type bridge | awk -F': ' '{print $$2}' | grep '^br-'); do \
		if ! docker network ls --format '{{.ID}}' | grep -q $${br#br-}; then \
			echo "Removing stale bridge: $$br"; \
			sudo ip link delete $$br 2>/dev/null || true; \
		fi \
	done

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