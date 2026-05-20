// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Wrapper config for Fabric-EVM integration testing
// Extends OpenZeppelin's config with fabricevm network
// This file gets copied into the OpenZeppelin directory at runtime

const ozConfig = require('./hardhat.config.js');

module.exports = {
  ...ozConfig,

  networks: {
    ...ozConfig.networks,

    // Fabric-EVM network for integration testing.
    fabricevm: {
      url: process.env.FABRIC_EVM_URL || 'http://127.0.0.1:8545',
      timeout: 60000,
    },
  },
};
