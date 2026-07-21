# Configuration
FABRIC_VERSION ?= 3.1.4
RELEASE_ARCHS  := amd64 arm64 s390x
UID := $(shell id -u)
GID := $(shell id -g)
export UID
export GID

# Container runtime — override for rootless Podman:
#   make start-x DOCKER=podman COMPOSE="podman compose"
# Note: build-image requires docker buildx (or podman buildx).
DOCKER  ?= docker
COMPOSE ?= docker compose

.PHONY: build
build:
	go build -o bin/fxevm ./cmd/fxevm

.PHONY: build-release
build-release:
	@for arch in $(RELEASE_ARCHS); do \
		mkdir -p release/linux-$$arch && \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags '-w -s' \
			-o release/linux-$$arch/fxevm ./cmd/fxevm || exit 1; \
	done

.PHONY: build-image
build-image: build-release
	$(DOCKER) buildx build \
		--file Dockerfile \
		--load \
		--build-arg VERSION=dev \
		--build-arg CREATED=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) \
		--build-arg REVISION=$(shell git rev-parse HEAD) \
		--tag fabric-x-evm:dev \
		.

.PHONY: checks
checks:
	@test -z $(shell gofmt -l -s $(shell go list -f '{{.Dir}}' ./...) | tee /dev/stderr) || (echo "Fix formatting issues"; exit 1)
	@go vet -all $(shell go list -f '{{.Dir}}' ./...)
	@go tool staticcheck ./... || (echo "Staticcheck failed"; exit 1)
	@find . -type d -name testdata -prune -o -name '*.go' -print | xargs go tool addlicense -check || (echo "Missing license headers"; exit 1)

.PHONY: unit-tests
unit-tests:
	go test ./... -short -coverprofile=coverage.out -covermode=atomic

.PHONY: pre-pull-images
pre-pull-images:
	@$(DOCKER) pull hyperledger/fabric-ccenv:$(FABRIC_VERSION) || echo "Warning: Failed to pull fabric-ccenv"

.PHONY: integration-tests
integration-tests: pre-pull-images
	@VERBOSE=$(VERBOSE) FABRIC_VERSION=$(FABRIC_VERSION) ./scripts/run_integration_test.sh

# Container images for fabric-x
TOOLS_IMAGE          ?= ghcr.io/hyperledger/fabric-x-tools:1.0.0
ORDERER_IMAGE        ?= ghcr.io/hyperledger/fabric-x-orderer:1.0.0
TEST_COMMITTER_IMAGE ?= docker.io/hyperledger/fabric-x-committer-test-node:1.0.3

# Namespace init defaults
NS      ?= basic
NS1     ?= real
NS2     ?= synthetic
POLICY  ?= AND('Org1MSP.member')
NETWORK ?= fabric-x

.PHONY: init-x
init-x:
	@rm -rf testdata/crypto
	@$(DOCKER) run --rm \
		--user "$(UID):$(GID)" \
		-v "$(PWD)/testdata:/config" \
		$(TOOLS_IMAGE) \
		cryptogen generate --config=/config/crypto-config.yaml --output=/config/crypto
	@# Routers and assemblers accept client certs from any peer org — concatenate all peer TLS CAs.
	@cat testdata/crypto/peerOrganizations/org1.example.com/msp/tlscacerts/tlsca.org1.example.com-cert.pem \
		testdata/crypto/peerOrganizations/org2.example.com/msp/tlscacerts/tlsca.org2.example.com-cert.pem \
		> testdata/crypto/client-tls-ca.pem
	@$(DOCKER) run --rm \
		--user "$(UID):$(GID)" \
		-v "$(PWD)/testdata:/config" \
		-v "$(PWD)/testdata/crypto:/crypto" \
		--entrypoint /usr/local/bin/armageddon \
		$(ORDERER_IMAGE) \
		createSharedConfigProto \
		--sharedConfigYaml=/config/shared_config.yaml \
		--output=/config/crypto/
	@$(DOCKER) run --rm \
		--user "$(UID):$(GID)" \
		-v "$(PWD)/testdata:/config" \
		$(TOOLS_IMAGE) \
		configtxgen --channelID mychannel --profile OrgsChannel \
		--outputBlock /config/crypto/config-block.pb.bin \
		--configPath /config
	@# Make crypto files readable by Prometheus (runs as nobody/uid 65534)
	@find testdata/crypto -type d -exec chmod a+rx {} +
	@find testdata/crypto -type f -exec chmod a+r {} +
	@# Make config files readable by Grafana (runs as uid 472)
	@find testdata/config -type d -exec chmod a+rx {} +
	@find testdata/config -type f -exec chmod a+r {} +

.PHONY: clean-x
clean-x:
	@rm -rf testdata/crypto

.PHONY: start-x
start-x:
	@if nc -z localhost 7050 2>/dev/null; then echo "Error: port 7050 is already in use — stop any running Fabric orderer before starting."; exit 1; fi
	@$(COMPOSE) -f compose.fabric-x.yml up -d
	@echo "Waiting for test committer to be ready..."
	@while ! nc -z localhost 7001 2>/dev/null; do sleep 1; done
	@echo "Creating namespace (retrying until the committer is ready)..."
	@ok=0; for attempt in 1 2 3 4 5; do \
		if $(DOCKER) run --rm --network $(NETWORK) \
			--user "$(UID):$(GID)" \
			--env "FX_NS=$(NS)" \
			--env "FX_POLICY=$(POLICY)" \
			-v "$(PWD)/testdata/fxconfig.yaml:/config/fxconfig.yaml:ro,Z" \
			-v "$(PWD)/testdata/crypto/peerOrganizations/org1.example.com/peers/fxconfig.org1.example.com/tls:/tls:ro,Z" \
			-v "$(PWD)/testdata/crypto/peerOrganizations/org1.example.com/users/channel_admin@org1.example.com/msp:/msp:ro,Z" \
			-v "$(PWD)/testdata/crypto/peerOrganizations/org1.example.com/msp/tlscacerts/tlsca.org1.example.com-cert.pem:/org-tls-ca.pem:ro,Z" \
			-v "$(PWD)/testdata/crypto/ordererOrganizations/orderer-org-1/msp/tlscacerts/tlsca.orderer-org-1-cert.pem:/orderer-tls-ca.pem:ro,Z" \
			$(TOOLS_IMAGE) \
			sh -c 'fxconfig namespace list --config=/config/fxconfig.yaml 2>/dev/null | grep -q ") $$FX_NS:" || \
			fxconfig namespace create "$$FX_NS" --policy="$$FX_POLICY" --endorse --submit --wait --config=/config/fxconfig.yaml'; then \
			ok=1; break; \
		fi; \
		echo "namespace setup attempt $$attempt failed; retrying in 3s..."; \
		sleep 3; \
	done; \
	[ "$$ok" = 1 ] || { echo "Error: namespace setup failed after 5 attempts"; exit 1; }

.PHONY: test-x
test-x:
	@go test -timeout 30s -v -run ^TestFabricX$$ ./integration

.PHONY: stop-x
stop-x:
	@$(COMPOSE) -f compose.fabric-x.yml down

.PHONY: start-fablo
start-fablo:
	@if nc -z localhost 7030 2>/dev/null; then echo "Error: port 7030 is already in use — stop any running Fabric orderer before starting."; exit 1; fi
	cd testdata/fablo && ./fablo up

.PHONY: stop-fablo
stop-fablo:
	cd testdata/fablo && ./fablo down

.PHONY: test-fablo
test-fablo:
	@go test -timeout 360s -run ^TestFablo$$ ./integration

.PHONY: clean-fablo
clean-fablo:
	cd testdata/fablo && ./fablo prune || true
	rm -rf testdata/fablo/snapshot.fablo.tar.gz

.PHONY: start-full
start-full:
	@if nc -z localhost 7050 2>/dev/null; then echo "Error: port 7050 is already in use — stop any running Fabric orderer before starting."; exit 1; fi
	@mkdir -p \
		data/orderers/party1-router data/orderers/party1-batcher \
		data/orderers/party1-consenter data/orderers/party1-assembler \
		data/orderers/party2-router data/orderers/party2-batcher \
		data/orderers/party2-consenter data/orderers/party2-assembler \
		data/orderers/party3-router data/orderers/party3-batcher \
		data/orderers/party3-consenter data/orderers/party3-assembler \
		data/orderers/party4-router data/orderers/party4-batcher \
		data/orderers/party4-consenter data/orderers/party4-assembler \
		data/committer-org1/db data/committer-org1/sidecar-ledger
	@$(COMPOSE) -f compose.fabric-x.full.yaml up -d
	@echo "Waiting for committer to be ready..."
	@while ! nc -z localhost 7001 2>/dev/null; do sleep 1; done
	@echo "Waiting for committer sidecar to be ready..."
	@while ! nc -z localhost 4001 2>/dev/null; do sleep 1; done
	@$(DOCKER) run --rm --network $(NETWORK) \
		--user "$(UID):$(GID)" \
		--env "FX_NS=$(NS1)" \
		--env "FX_POLICY=$(POLICY)" \
		-v "$(PWD)/testdata/fxconfig.yaml:/config/fxconfig.yaml:ro,Z" \
		-v "$(PWD)/testdata/crypto/peerOrganizations/org1.example.com/peers/fxconfig.org1.example.com/tls:/tls:ro,Z" \
		-v "$(PWD)/testdata/crypto/peerOrganizations/org1.example.com/users/channel_admin@org1.example.com/msp:/msp:ro,Z" \
		-v "$(PWD)/testdata/crypto/peerOrganizations/org1.example.com/msp/tlscacerts/tlsca.org1.example.com-cert.pem:/org-tls-ca.pem:ro,Z" \
		-v "$(PWD)/testdata/crypto/ordererOrganizations/orderer-org-1/msp/tlscacerts/tlsca.orderer-org-1-cert.pem:/orderer-tls-ca.pem:ro,Z" \
		$(TOOLS_IMAGE) \
		sh -c 'fxconfig namespace list --config=/config/fxconfig.yaml 2>/dev/null | grep -q ") $$FX_NS:" || \
		fxconfig namespace create "$$FX_NS" --policy="$$FX_POLICY" --endorse --submit --wait --config=/config/fxconfig.yaml'
	@$(DOCKER) run --rm --network $(NETWORK) \
		--user "$(UID):$(GID)" \
		--env "FX_NS=$(NS2)" \
		--env "FX_POLICY=$(POLICY)" \
		-v "$(PWD)/testdata/fxconfig.yaml:/config/fxconfig.yaml:ro,Z" \
		-v "$(PWD)/testdata/crypto/peerOrganizations/org1.example.com/peers/fxconfig.org1.example.com/tls:/tls:ro,Z" \
		-v "$(PWD)/testdata/crypto/peerOrganizations/org1.example.com/users/channel_admin@org1.example.com/msp:/msp:ro,Z" \
		-v "$(PWD)/testdata/crypto/peerOrganizations/org1.example.com/msp/tlscacerts/tlsca.org1.example.com-cert.pem:/org-tls-ca.pem:ro,Z" \
		-v "$(PWD)/testdata/crypto/ordererOrganizations/orderer-org-1/msp/tlscacerts/tlsca.orderer-org-1-cert.pem:/orderer-tls-ca.pem:ro,Z" \
		$(TOOLS_IMAGE) \
		sh -c 'fxconfig namespace list --config=/config/fxconfig.yaml 2>/dev/null | grep -q ") $$FX_NS:" || \
		fxconfig namespace create "$$FX_NS" --policy="$$FX_POLICY" --endorse --submit --wait --config=/config/fxconfig.yaml'

.PHONY: stop-full
stop-full:
	@$(COMPOSE) -f compose.fabric-x.full.yaml down
	@rm -rf data/

.PHONY: test-local
test-local:
	@go test -timeout 30s -v -run ^TestLocal$$ ./integration

.PHONY: test-local-x
test-local-x:
	@go test -timeout 30s -v -run ^TestLocalX$$ ./integration

.PHONY: fetch-execution-specs-tests
fetch-execution-specs-tests:
	@./scripts/fetch_execution_specs_tests.sh

.PHONY: eth-tests
eth-tests:
	@go test -test.fullpath=true -timeout 2000s -run ^TestEthereumTests$$ github.com/hyperledger/fabric-x-evm/integration
	# @VERBOSE=$(VERBOSE) ./scripts/run_eth_test.sh

.PHONY: eth-tests-legacy
eth-tests-legacy:
	@go test -test.fullpath=true -timeout 2000s -run ^TestEthereumTests$$ github.com/hyperledger/fabric-x-evm/integration -legacy

.PHONY: eth-tests-slow
eth-tests-slow:
	@go test -test.fullpath=true -timeout 10000s -run ^TestEthereumTests$$ github.com/hyperledger/fabric-x-evm/integration -very_slow

.PHONY: eth-tests-slow-legacy
eth-tests-slow-legacy:
	@go test -test.fullpath=true -timeout 10000s -run ^TestEthereumTests$$ github.com/hyperledger/fabric-x-evm/integration -very_slow -legacy

.PHONY: hardhat-tests
hardhat-tests:
	@./scripts/run_hardhat_test.sh

.PHONY: perf-tests
perf-tests: pre-pull-images
	@VERBOSE=$(VERBOSE) FABRIC_VERSION=$(FABRIC_VERSION) ./scripts/run_perf_test.sh
