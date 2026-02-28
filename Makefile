.PHONY: up
up:
	docker compose up -d

.PHONY: up-no-daemon
up-no-daemon:
	docker compose up

.PHONY: down
down:
	docker compose down

.PHONY: destroy
destroy:
	docker compose stop
	docker compose rm -f

.PHONY: build
build:
	docker build --target builder -t mojiokoshin-builder .

.PHONY: lint
lint:
	docker compose run --rm backend golangci-lint run

.PHONY: format
format:
	docker compose run --rm backend golangci-lint fmt -E gofmt

.PHONY: test
test:
	docker compose run --rm backend go test ./...
