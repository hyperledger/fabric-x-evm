// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Wrapper config for Fabric-EVM integration testing.
// Extends OpenZeppelin's own hardhat.config.js with a fabricevm network. Loaded via
// `--config <path-to-this-file>` with cwd set to the OpenZeppelin checkout, so
// ozConfig resolves to that checkout's own config without copying this file into it.

const path = require('path');
const ozConfig = require(path.join(process.cwd(), 'hardhat.config.js'));

// Mocha's built-in JSON reporter calls JSON.stringify on failing assertions' actual/
// expected values; a raw BigInt anywhere in a deep-equal diff throws inside that
// (uncaught, silent) and the reporter writes nothing at all for the whole run — no
// error, just empty output. ethers v6 returns BigInt everywhere, so this hits often.
// toJSON is what JSON.stringify calls when present, so this fixes it globally.
BigInt.prototype.toJSON = function () {
  return this.toString();
};

// This Hardhat version has no --reporter CLI flag; the mocha reporter must come from
// config. Gated by an env var so a plain `npx hardhat test` still gets the normal
// human-readable reporter. When set, use the combined reporter (mocha-spec-and-json-
// reporter.js) so a single run gives both the live console view and the JSON file
// cmd/baseline parses, instead of picking one or the other.
const mochaConfig = { ...ozConfig.mocha };
if (process.env.HARDHAT_JSON_OUTPUT) {
  mochaConfig.reporter = path.join(__dirname, 'mocha-spec-and-json-reporter.js');
}

module.exports = {
  ...ozConfig,

  // Hardhat defaults paths.root to this file's own directory; since this file lives
  // outside the OpenZeppelin checkout, pin it back to cwd (the checkout) explicitly.
  paths: {
    ...ozConfig.paths,
    root: process.cwd(),
  },

  mocha: mochaConfig,

  networks: {
    ...ozConfig.networks,

    // Fabric-EVM network for integration testing.
    fabricevm: {
      url: process.env.FABRIC_EVM_URL || 'http://127.0.0.1:8545',
      timeout: 60000,
    },
  },
};
