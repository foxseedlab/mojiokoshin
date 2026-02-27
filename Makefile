@PHONY: up
up:
	docker compose up -d

@PHONY: up-no-daemon
up-no-daemon:
	docker compose up

@PHONY: down
down:
	docker compose down

@PHONY: destroy
destroy:
	docker compose stop
	docker compose rm -f

@PHONY: lint
lint:
	docker run --rm -v ./:/app -w /app golangci/golangci-lint:v2.10.1 golangci-lint run
