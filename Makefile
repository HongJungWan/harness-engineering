.PHONY: build test run lint docker-up docker-down docker-debug migrate-up migrate-down clean help

# Variables
APP_NAME    := harness-order
GO          := go
GOFLAGS     := -trimpath -ldflags="-s -w"
DOCKER_COMP := docker compose

# Build & Run
build:
	$(GO) build $(GOFLAGS) -o bin/$(APP_NAME) ./cmd/server

run: build
	./bin/$(APP_NAME)

test:
	$(GO) test -v -race -count=1 -coverprofile=coverage.out ./...

test-short:
	$(GO) test -v -short -race ./...

lint:
	golangci-lint run ./...

# Docker
docker-up:
	$(DOCKER_COMP) up -d mysql kafka kafka-init
	@echo "Waiting for services to be healthy..."
	$(DOCKER_COMP) up -d --wait

docker-down:
	$(DOCKER_COMP) down

docker-debug:
	$(DOCKER_COMP) --profile debug up -d

docker-clean:
	$(DOCKER_COMP) down -v --remove-orphans

docker-logs:
	$(DOCKER_COMP) logs -f

# Database Migrations
MIGRATE := migrate -path ./migrations -database "mysql://harness:harness@tcp(localhost:3306)/harness"

migrate-up:
	$(MIGRATE) up

migrate-down:
	$(MIGRATE) down 1

migrate-status:
	$(MIGRATE) version

# Utilities
clean:
	rm -rf bin/ coverage.out

kafka-topics:
	docker exec harness-kafka /opt/kafka/bin/kafka-topics.sh \
		--bootstrap-server localhost:9092 --list

kafka-consume:
	docker exec harness-kafka /opt/kafka/bin/kafka-console-consumer.sh \
		--bootstrap-server localhost:9092 --topic order.events --from-beginning

# Architecture check
arch-check:
	bash ci/arch-check.sh

help:
	@echo "Available targets:"
	@echo "  build         - Build the application binary"
	@echo "  run           - Build and run the application"
	@echo "  test          - Run all tests with race detector"
	@echo "  test-short    - Run short tests only"
	@echo "  lint          - Run golangci-lint"
	@echo "  docker-up     - Start MySQL + Kafka"
	@echo "  docker-down   - Stop all containers"
	@echo "  docker-debug  - Start all containers including Kafka UI"
	@echo "  docker-clean  - Stop containers and remove volumes"
	@echo "  migrate-up    - Apply all pending migrations"
	@echo "  migrate-down  - Roll back one migration"
	@echo "  arch-check    - Run architecture violation checks"
	@echo "  clean         - Remove build artifacts"
	@echo "  kafka-topics  - List Kafka topics"
	@echo "  kafka-consume - Consume from order.events topic"
