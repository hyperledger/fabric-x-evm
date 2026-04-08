.PHONY: checks
checks:
	@test -z $(shell gofmt -l -s $(shell go list -f '{{.Dir}}' ./...) | tee /dev/stderr) || (echo "Fix formatting issues"; exit 1)
	@go vet -all $(shell go list -f '{{.Dir}}' ./...)
	@go tool staticcheck ./... || (echo "Staticcheck failed"; exit 1)
	find . -type d -name testdata -prune -o -name '*.go' -print | xargs go tool addlicense -check || (echo "Missing license headers"; exit 1)

.PHONY: unit-tests
unit-tests:
	go test ./... -short

.PHONY: integration-tests
integration-tests:
	@VERBOSE=$(VERBOSE) ./scripts/run_integration_test.sh

.PHONY: init-x
init-x:
	@go tool cryptogen generate --config testdata/crypto-config.yaml --output testdata/crypto
	@cd testdata && go tool configtxgen --channelID mychannel --profile OrgsChannel --outputBlock crypto/sc-genesis-block.proto.bin

.PHONY: clean-x
clean-x:
	@rm -rf testdata/crypto

.PHONY: start-x
start-x:
	@docker run -d --rm -it --name fabric-x-committer-test-node \
		-p 4001:4001 -p 2110:2110 -p 2114:2114 -p 2117:2117 -p 7001:7001 -p 7050:7050 -p 5433:5433 \
		-v "$(PWD)/testdata/crypto:/root/config/crypto" \
		-v "$(PWD)/testdata/crypto/sc-genesis-block.proto.bin:/root/config/sc-genesis-block.proto.bin" \
		-e SC_SIDECAR_ORDERER_IDENTITY_MSP_DIR=/root/config/crypto/peerOrganizations/org1.example.com/peers/committer.org1.example.com/msp \
		-e SC_SIDECAR_ORDERER_IDENTITY_MSP_ID=Org1MSP \
		-e SC_SIDECAR_ORDERER_CHANNEL_ID=mychannel \
		-e SC_SIDECAR_ORDERER_SIGNED_ENVELOPES=true \
		-e SC_QUERY_SERVICE_SERVER_ENDPOINT=:7001 \
		-e SC_ORDERER_BLOCK_SIZE=1 \
		-e SC_SIDECAR_LOGGING_LEVEL=DEBUG \
		-e SC_QUERY_SERVICE_LOGGING_LEVEL=DEBUG \
		-e SC_COORDINATOR_LOGGING_LEVEL=DEBUG \
		-e SC_ORDERER_LOGGING_LEVEL=DEBUG \
		-e SC_VC_LOGGING_LEVEL=DEBUG \
		-e SC_VERIFIER_LOGGING_LEVEL=INFO \
		docker.io/hyperledger/fabric-x-committer-test-node:0.1.7 run db orderer committer
	@while ! nc -z localhost 7050 2>/dev/null; do sleep 1; done
	@go tool fxconfig namespace create basic --channel=mychannel --orderer=localhost:7050 --mspID=Org1MSP \
		--mspConfigPath=testdata/crypto/peerOrganizations/org1.example.com/users/channel_admin@org1.example.com/msp \
		--pk=testdata/crypto/peerOrganizations/org1.example.com/peers/endorser.org1.example.com/msp/signcerts/endorser.org1.example.com-cert.pem
	@until go tool fxconfig namespace list --endpoint=localhost:7001 2>/dev/null | grep -q basic; do sleep 1; echo "waiting for namespace to be created..."; done
	@go tool fxconfig namespace list --endpoint=localhost:7001

.PHONY: test-x
test-x:
	@go test -timeout 360s -v -run ^TestFabricX$$ ./integration

.PHONY: stop-x
stop-x:
	@docker rm -f fabric-x-committer-test-node

.PHONY: init-3
init-3:
	@cd testdata && \
		curl -sSLO https://raw.githubusercontent.com/hyperledger/fabric/main/scripts/install-fabric.sh && \
		chmod +x install-fabric.sh && \
		./install-fabric.sh --fabric-version 3.1.3 && \
		rm ./install-fabric.sh


.PHONY: start-3
start-3:
	@./testdata/fabric-samples/test-network/network.sh up createChannel -i 3.1.3
	@./testdata/fabric-samples/test-network/network.sh deployCCAAS -ccn basic -ccp "$(PWD)/testdata/fabric-samples/asset-transfer-basic/chaincode-external"

.PHONY: test-3
test-3:
	@go test -timeout 360s -v -run ^TestFabric$$ ./integration

.PHONY: stop-3
stop-3:
	@./testdata/fabric-samples/test-network/network.sh down
	@docker rm -f $$(docker ps -a -q --filter "ancestor=basic_ccaas_image") || true

.PHONY: test-local
test-local:
	@go test -timeout 360s -v -run ^TestLocal$$ ./integration

.PHONY: test-local-x
test-local-x:
	@go test -timeout 360s -v -run ^TestLocalX$$ ./integration

.PHONY: eth-tests
eth-tests:
	@go test -test.fullpath=true -timeout 1000s -run ^TestEthereumTests$$ github.com/hyperledger/fabric-x-evm/integration
	# @VERBOSE=$(VERBOSE) ./scripts/run_eth_test.sh

.PHONY: eth-tests-legacy
eth-tests-legacy:
	@go test -test.fullpath=true -timeout 1000s -run ^TestEthereumTests$$ github.com/hyperledger/fabric-x-evm/integration -legacy

.PHONY: eth-tests-slow
eth-tests-slow:
	@go test -test.fullpath=true -timeout 10000s -run ^TestEthereumTests$$ github.com/hyperledger/fabric-x-evm/integration -very_slow

.PHONY: eth-tests-slow-legacy
eth-tests-slow-legacy:
	@go test -test.fullpath=true -timeout 10000s -run ^TestEthereumTests$$ github.com/hyperledger/fabric-x-evm/integration -very_slow -legacy
