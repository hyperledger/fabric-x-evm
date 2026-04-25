/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package config

import (
	"fmt"
	"os"

	"github.com/hyperledger/fabric-x-common/common/viperutil"
)

// Load reads a YAML config file and unmarshals it into a Config.
func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %q: %w", path, err)
	}
	defer f.Close()

	parser := viperutil.New()
	if err := parser.ReadConfig(f); err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := parser.EnhancedExactUnmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}

	return cfg, nil
}
