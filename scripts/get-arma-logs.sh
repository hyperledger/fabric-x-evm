#!/bin/bash

# Party 1
docker logs orderer-party1-batcher >& arma-b1.log
docker logs orderer-party1-assembler >& arma-a1.log
docker logs orderer-party1-consenter >& arma-c1.log
docker logs orderer-party1-router >& arma-r1.log

# Party 2
docker logs orderer-party2-batcher >& arma-b2.log
docker logs orderer-party2-assembler >& arma-a2.log
docker logs orderer-party2-consenter >& arma-c2.log
docker logs orderer-party2-router >& arma-r2.log

# Party 3
docker logs orderer-party3-batcher >& arma-b3.log
docker logs orderer-party3-assembler >& arma-a3.log
docker logs orderer-party3-consenter >& arma-c3.log
docker logs orderer-party3-router >& arma-r3.log

# Party 4
docker logs orderer-party4-batcher >& arma-b4.log
docker logs orderer-party4-assembler >& arma-a4.log
docker logs orderer-party4-consenter >& arma-c4.log
docker logs orderer-party4-router >& arma-r4.log
