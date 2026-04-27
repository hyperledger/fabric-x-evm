1) we download the dataset from https://dataverse.harvard.edu/dataset.xhtml?persistentId=doi:10.7910/DVN/8YO2VZ

in particulat we go

```
wget https://dataverse.harvard.edu/api/access/datafile/11691882
```

this will download the whole dataset in tsv.gz format.

2) filter just the transactions from USDC

```
zgrep -E '(^.{93}a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48|token_address)' 202001.tsv.gz | gzip > dataset.tsv.gz
```

3) prime the ledger with the tether smart contract; it's a proxy and so we need to get its code, its pointer to the real code and the real code. `fetcher.go` does that and creates `./testdata/USDC_contract.json`:

```
go run ./ -mode fetch
```

4) get the Go bindings so we can construct invocations to it from go

```
wget https://github.com/argotorg/solidity/releases/download/v0.6.12/solc-static-linux -O /tmp/solc
chmod u+x /tmp/solc
cd ../../solidity/USDC/
/tmp/solc --bin --abi --storage-layout \
    --overwrite -o ../../bin/USDC \
    --base-path . --allow-paths . v2/FiatTokenV2_2.sol
cd ../../bin/
go install github.com/ethereum/go-ethereum/cmd/abigen@latest
abigen   --abi bin/USDC/FiatTokenV2_2.abi   \
    --pkg contracts   --type FiatTokenV2_2   \
    --out usdc_fiattokenv2_2.go
```

5) Now we pre-generate transactions, again with `fetcher.go`:

```
go run ./ -mode generate -input ./testdata/dataset.tsv.gz
```

6) At this point we can replay transactions. For this we need to
    - prime the smart contracts
    - prime the balance of all transactors
    - replay all transactions
   `TestReplayJSONDataset` does this.