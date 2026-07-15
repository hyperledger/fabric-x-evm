// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Wrapper config for Fabric-EVM integration testing.
// Extends OpenZeppelin's own hardhat.config.js with a fabricevm network. Loaded via
// `--config <path-to-this-file>` with cwd set to the OpenZeppelin checkout, so
// ozConfig resolves to that checkout's own config without copying this file into it.

const path = require('path');
const ozConfig = require(path.join(process.cwd(), 'hardhat.config.js'));

module.exports = {
  ...ozConfig,

  // Hardhat defaults paths.root to this file's own directory; since this file lives
  // outside the OpenZeppelin checkout, pin it back to cwd (the checkout) explicitly.
  paths: {
    ...ozConfig.paths,
    root: process.cwd(),
  },

  networks: {
    ...ozConfig.networks,

    // Fabric-EVM network for integration testing.
    fabricevm: {
      url: process.env.FABRIC_EVM_URL || 'http://127.0.0.1:8545',
      timeout: 60000,
    },
  },
};
