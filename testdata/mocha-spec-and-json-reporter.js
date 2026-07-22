// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Combines Mocha's own built-in Spec (human-readable, to stdout) and JSON
// (machine-readable, to a file) reporters against the same test run, so one
// invocation gives both a live console view and a report `cmd/baseline` can
// parse — no need to run the suite twice, and no third-party reporter
// package: both are required directly from the already-installed `mocha`.
//
// Resolved relative to process.cwd() (the OpenZeppelin checkout), not this
// file's own directory, since this file lives outside that checkout — same
// reason hardhat.wrapper.config.js resolves ozConfig via process.cwd().
'use strict';

const path = require('path');
const mochaLib = path.join(process.cwd(), 'node_modules', 'mocha', 'lib');
const Spec = require(path.join(mochaLib, 'reporters', 'spec'));
const JSONReporter = require(path.join(mochaLib, 'reporters', 'json'));

module.exports = function (runner, options) {
  new Spec(runner, options);

  const outputPath = process.env.HARDHAT_JSON_OUTPUT;
  new JSONReporter(runner, {
    ...options,
    reporterOption: outputPath ? { output: outputPath } : undefined,
  });
};
