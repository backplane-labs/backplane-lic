# backplane-lic — the Keygen CE-compatible licensing API on Backplane.
#
# Conventions (shared with backplane): every tool lives in ./bin, pinned
# below; `git clone` + `make configure` = working environment.

SHELL := /bin/bash
.ONESHELL:
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

BIN := $(CURDIR)/bin

GOLANGCI_LINT_VERSION      := v2.11.3
SQLD_VERSION               := v1.1.10
EASYP_VERSION              := v0.16.6
PROTOC_GEN_GO_VERSION      := v1.36.11
PROTOC_GEN_GO_GRPC_VERSION := v1.6.2

# The console's TS protobuf generators are installed with configure-web.
EASYP := PATH="$(CURDIR)/web/node_modules/.bin:$(BIN):$$PATH" "$(BIN)/easyp"
YARN := env -u HTTPS_PROXY -u HTTP_PROXY -u https_proxy -u http_proxy yarn

OGEN        := go run github.com/ogen-go/ogen/cmd/ogen@v1.20.3
OPENAPI     := openapi/openapi.yaml
# ogen consumes OpenAPI 3.0; this is the down-projected build artifact.
OPENAPI_30  := openapi/.build/openapi.3.0.yaml
OAS_DIR     := internal/oas
OAS_PKG     := oas

COMPOSE := docker compose -f deployments/compose.yaml

# Scratch database `make db-init` realizes the schema in: sqld drops every
# non-system schema in it, so never point it at a real database.
DB_DEV_URL ?= postgres://lic:lic@127.0.0.1:5440/lic_scratch?sslmode=disable

# The database the conformance suite boots the server under (override
# on the command line to isolate from the shared compose database).
LIC_TEST_DATABASE_URL ?= postgres://lic:lic@127.0.0.1:5440/lic?sslmode=disable

# The database the conformance suite boots the server under (override
# on the command line to isolate from the shared compose database).
LIC_TEST_DATABASE_URL ?= postgres://lic:lic@127.0.0.1:5440/lic?sslmode=disable

BUILD_PKG := github.com/backplane-labs/backplane/pkg/backplane/build
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0)
COMMIT    ?= $(shell git rev-parse --verify -q HEAD 2>/dev/null || echo unknown)
DATE      ?= $(shell date -u +%FT%TZ)
LDFLAGS    = -X $(BUILD_PKG).Service=lic -X $(BUILD_PKG).Version=$(VERSION) -X $(BUILD_PKG).Commit=$(COMMIT) -X $(BUILD_PKG).Date=$(DATE)

MAX_MAJOR := 1

.PHONY: help
help: ## List all targets with explanations
	@grep -hE '^[a-zA-Z0-9_.-]+:.*## ' $(MAKEFILE_LIST) | \
	  awk -F':.*## ' '{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: configure
configure: ## Install the pinned linter, sqld and proto generators into ./bin
	mkdir -p "$(BIN)"
	GOBIN="$(BIN)" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	GOBIN="$(BIN)" go install github.com/gopherex/sqld/cmd/sqld@$(SQLD_VERSION)
	GOBIN="$(BIN)" go install github.com/gopherex/sqld/cmd/sqld-gen-go@$(SQLD_VERSION)
	GOBIN="$(BIN)" go install github.com/easyp-tech/easyp/cmd/easyp@$(EASYP_VERSION)
	GOBIN="$(BIN)" go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	GOBIN="$(BIN)" go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)
	@echo "✓ configure done — tools in $(BIN)"

.PHONY: up
up: ## Start the dev dependencies (postgres :5440, valkey :6390)
	$(COMPOSE) up -d postgres valkey

.PHONY: down
down: ## Stop the dev dependencies
	$(COMPOSE) down

.PHONY: keygen-env
keygen-env: ## Generate deployments/keygen/.env for the keygen reference profile
	mkdir -p deployments/keygen
	if [ -f deployments/keygen/.env ]; then
	  echo "✓ deployments/keygen/.env already exists"
	  exit 0
	fi
	{
	  echo "SECRET_KEY_BASE=$$(openssl rand -hex 64)"
	  echo "ENCRYPTION_DETERMINISTIC_KEY=$$(openssl rand -base64 32)"
	  echo "ENCRYPTION_PRIMARY_KEY=$$(openssl rand -base64 32)"
	  echo "ENCRYPTION_KEY_DERIVATION_SALT=$$(openssl rand -base64 32)"
	  echo "KEYGEN_EDITION=CE"
	  echo "KEYGEN_MODE=singleplayer"
	  echo "KEYGEN_HOST=api.keygen.test"
	  echo "KEYGEN_ACCOUNT_ID=2c1d4f2e-9a3b-4c5d-8e7f-0a1b2c3d4e5f"
	  echo "KEYGEN_ADMIN_EMAIL=admin@example.com"
	  echo "KEYGEN_ADMIN_PASSWORD=adminadmin123"
	  echo "RAILS_LOG_TO_STDOUT=1"
	} > deployments/keygen/.env
	@echo "✓ wrote deployments/keygen/.env"

.PHONY: keygen-up
keygen-up: keygen-env ## Start the Keygen CE reference server (:3900; run keygen-setup once)
	$(COMPOSE) --profile keygen up -d
	$(COMPOSE) --profile keygen run --rm keygen-setup

.PHONY: keygen-down
keygen-down: ## Stop the Keygen CE reference server
	$(COMPOSE) --profile keygen down

.PHONY: spec
spec: ## Bundle openapi/src fragments into openapi/openapi.yaml (3.1)
	python3 scripts/bundle_openapi.py openapi/src openapi/openapi.yaml

.PHONY: configure-web
configure-web: ## Install the console plugin dependencies
	cd "$(CURDIR)/web" && $(YARN) install --frozen-lockfile

.PHONY: gen
gen: lint-proto ## Generate the console Go API and TypeScript client
	$(EASYP) generate

.PHONY: lint-proto
lint-proto: ## Lint the console protocol
	"$(BIN)/easyp" lint -p proto

.PHONY: web
web: ## Build and embed the console module (run before build)
	cd "$(CURDIR)/web" && $(YARN) typecheck && $(YARN) build
	rm -rf "$(CURDIR)/internal/ui/dist"/*
	cp -r "$(CURDIR)/web/dist/plugin/." "$(CURDIR)/internal/ui/dist/"
	rm -rf "$(CURDIR)/internal/ui/dist/.vite"

.PHONY: gen-api
gen-api: spec ## Generate the ogen server code into internal/oas
	@mkdir -p "$(dir $(OPENAPI_30))"
	python3 scripts/openapi_to_30.py "$(OPENAPI)" "$(OPENAPI_30)"
	$(OGEN) --config .ogen.yaml --target $(OAS_DIR) --package $(OAS_PKG) --clean "$(OPENAPI_30)"

.PHONY: db
db: ## Generate the typed queries (internal/infra/postgres/db) and validate migrations
	"$(BIN)/sqld" generate -c sqld.yaml
	"$(BIN)/sqld" migrate validate -c sqld.yaml

.PHONY: db-init
db-init: ## Regenerate the single init migration from schema.sql (pre-release: no history)
	until $(COMPOSE) exec -T postgres pg_isready -U lic >/dev/null 2>&1; do sleep 1; done
	$(COMPOSE) exec -T postgres dropdb -U lic --if-exists lic_scratch
	$(COMPOSE) exec -T postgres createdb -U lic lic_scratch
	rm -f internal/infra/postgres/migrations/*.sql
	"$(BIN)/sqld" migrate generate init -c sqld.yaml --dev-url "$(DB_DEV_URL)"
	sed -i -e 's|^CREATE SCHEMA "lic";|CREATE SCHEMA IF NOT EXISTS "lic";|' -e '/^DROP SCHEMA "lic";$$/d' internal/infra/postgres/migrations/*_init.sql
	"$(BIN)/sqld" migrate validate -c sqld.yaml

.PHONY: lint
lint: ## Lint Go code
	"$(BIN)/golangci-lint" run ./...

.PHONY: fmt
fmt: ## Format Go code
	"$(BIN)/golangci-lint" fmt ./...

.PHONY: test
test: ## Unit tests with the race detector (PG/Valkey tests skip without LIC_TEST_*)
	go vet ./...
	go test -race ./...

.PHONY: test-live
test-live: ## Tests against the compose PostgreSQL and Valkey
	LIC_TEST_DATABASE_URL="postgres://lic:lic@127.0.0.1:5440/lic?sslmode=disable" \
	LIC_TEST_VALKEY_ADDR="127.0.0.1:6390" \
	go test -race -count=1 ./...

.PHONY: conformance
conformance: ## Diff the server (in-process over the compose PostgreSQL) against the recorded goldens
	LIC_TEST_DATABASE_URL="$(LIC_TEST_DATABASE_URL)" \
	go test -count=1 ./test/conformance

.PHONY: conformance-record
conformance-record: ## Record Keygen CE (:3900); LIC_CONFORMANCE_REFERENCE_TOKEN skips password login
	go test -count=1 ./test/conformance -record

.PHONY: build
build: ## Build the service into ./bin/lic
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN)/lic" ./cmd/lic

.PHONY: release
release: ## Create and push a version tag; no local builds or tests
	cd "$$(git rev-parse --show-toplevel)"
	if [ -n "$$(git status --porcelain)" ]; then
	  echo "✗ Working tree is not clean — commit or stash first:"
	  git status --short
	  exit 1
	fi
	if ! upstream="$$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null)"; then
	  echo "✗ Current branch has no upstream — push it first."
	  exit 1
	fi
	git fetch --quiet --prune --tags
	read -r behind ahead <<< "$$(git rev-list --left-right --count "$$upstream...HEAD")"
	if [ "$$ahead" -gt 0 ] || [ "$$behind" -gt 0 ]; then
	  echo "✗ Branch differs from $$upstream ($$ahead ahead, $$behind behind) — synchronize first."
	  exit 1
	fi
	cur="$$(git tag -l 'v[0-9]*.[0-9]*.[0-9]*' | sed 's/^v//' | sort -t. -k1,1n -k2,2n -k3,3n | tail -1)"
	cur="$${cur:-0.0.0}"
	head="$$(git rev-parse --short HEAD)"
	echo "Latest release: v$$cur    HEAD: $$head"
	echo
	echo "  1) bump version"
	echo "  2) recreate last tag (v$$cur) on HEAD   [force]"
	echo "  3) cancel"
	read -r -p "> " action
	case "$$action" in
	1)
	  IFS=. read -r MA MI PA <<< "$$cur"
	  echo
	  echo "  1) major  -> v$$((MA+1)).0.0"
	  echo "  2) minor  -> v$$MA.$$((MI+1)).0"
	  echo "  3) patch  -> v$$MA.$$MI.$$((PA+1))"
	  read -r -p "> " comp
	  case "$$comp" in
	  1) MA=$$((MA+1)); MI=0; PA=0 ;;
	  2) MI=$$((MI+1)); PA=0 ;;
	  3) PA=$$((PA+1)) ;;
	  *) echo "Aborted."; exit 0 ;;
	  esac
	  if [ "$$MA" -gt "$(MAX_MAJOR)" ]; then
	    echo "✗ v$$MA requires semantic import versioning (/v$$MA in the module path)."
	    exit 1
	  fi
	  new="$$MA.$$MI.$$PA"
	  echo
	  echo "Release v$$new — will create tag v$$new on $$head and push."
	  read -r -p "Type 'yes' to proceed: " ok
	  [ "$$ok" = "yes" ] || { echo "Aborted."; exit 0; }
	  git tag -a "v$$new" -m "v$$new"
	  git push origin "v$$new"
	  echo "✓ Released v$$new."
	  ;;
	2)
	  if ! git tag -l "v$$cur" | grep -q .; then
	    echo "✗ No release tag to recreate."; exit 1
	  fi
	  echo
	  echo "Will DELETE and recreate tag v$$cur on $$head, then force-push."
	  read -r -p "Type 'yes' to proceed: " ok
	  [ "$$ok" = "yes" ] || { echo "Aborted."; exit 0; }
	  git tag -d "v$$cur"
	  git push origin ":refs/tags/v$$cur" 2>/dev/null || true
	  git tag -a "v$$cur" -m "v$$cur"
	  git push origin --force "v$$cur"
	  echo "✓ Recreated v$$cur on $$head."
	  ;;
	*) echo "Cancelled." ;;
	esac
