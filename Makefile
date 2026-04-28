.PHONY: checks
checks:
	@test -z $(shell gofmt -l -s $(shell go list -f '{{.Dir}}' ./...) | tee /dev/stderr) || (echo "Fix formatting issues"; exit 1)
	@go vet -all $(shell go list -f '{{.Dir}}' ./...)
	@go tool staticcheck ./... || (echo "Staticcheck failed"; exit 1)
	@find . -type d -name testdata -prune -o -name '*.go' -print | xargs go tool addlicense -check || (echo "Missing license headers"; exit 1)

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
		-v "$(PWD)/testdata/crypto/sc-genesis-block.proto.bin:/root/artifacts/config-block.pb.bin" \
		-v "$(PWD)/testdata/crypto/peerOrganizations/Org1/peers/committer.org1.example.com/tls/server.crt:/server-certs/public-key.pem" \
		-v "$(PWD)/testdata/crypto/peerOrganizations/Org1/peers/committer.org1.example.com/tls/server.key:/server-certs/private-key.pem" \
		-v "$(PWD)/testdata/crypto/peerOrganizations/Org1/peers/committer.org1.example.com/tls/ca.crt:/server-certs/ca-certificate.pem" \
		-v "$(PWD)/testdata/crypto/peerOrganizations/Org1/peers/committer.org1.example.com/tls/server.crt:/client-certs/public-key.pem" \
		-v "$(PWD)/testdata/crypto/peerOrganizations/Org1/peers/committer.org1.example.com/tls/server.key:/client-certs/private-key.pem" \
		-v "$(PWD)/testdata/crypto/peerOrganizations/Org1/peers/committer.org1.example.com/tls/ca.crt:/client-certs/ca-certificate.pem" \
		-e SC_SIDECAR_ORDERER_IDENTITY_MSP_DIR=/root/config/crypto/peerOrganizations/Org1/peers/committer.org1.example.com/msp \
		-e SC_SIDECAR_ORDERER_IDENTITY_MSP_ID=Org1MSP \
		-e SC_SIDECAR_ORDERER_CHANNEL_ID=mychannel \
		-e SC_SIDECAR_ORDERER_SIGNED_ENVELOPES=true \
		-e SC_QUERY_SERVICE_SERVER_ENDPOINT=:7001 \
		-e SC_ORDERER_BLOCK_SIZE=1 \
		docker.io/hyperledger/fabric-x-committer-test-node:0.1.9 run db orderer committer
	@while ! nc -z localhost 7001 2>/dev/null; do sleep 1; done
	@go tool fxconfig namespace create basic --policy="OR('Org1MSP.member')" --endorse --submit --wait --config=testdata/fxconfig.yaml

.PHONY: test-x
test-x:
	@go test -timeout 30s -v -run ^TestFabricX$$ ./integration

.PHONY: stop-x
stop-x:
	@docker rm -f fabric-x-committer-test-node

.PHONY: start-fablo
start-fablo:
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
.PHONY: test-local
test-local:
	@go test -timeout 30s -v -run ^TestLocal$$ ./integration

.PHONY: test-local-x
test-local-x:
	@go test -timeout 30s -v -run ^TestLocalX$$ ./integration

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
