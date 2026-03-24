/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"encoding/json"
	"os"
)

// EthereumTest represents the JSON structure from ethereum/tests
type EthereumTest struct {
	Env         TestEnv                `json:"env"`
	Pre         map[string]TestAccount `json:"pre"`
	Transaction TestTransaction        `json:"transaction"`
	Post        map[string][]PostState `json:"post"`
}

// TestEnv contains the block environment for the test
type TestEnv struct {
	CurrentCoinbase   string `json:"currentCoinbase"`
	CurrentDifficulty string `json:"currentDifficulty"`
	CurrentGasLimit   string `json:"currentGasLimit"`
	CurrentNumber     string `json:"currentNumber"`
	CurrentTimestamp  string `json:"currentTimestamp"`
	CurrentBaseFee    string `json:"currentBaseFee,omitempty"`
	CurrentRandom     string `json:"currentRandom,omitempty"`
}

// TestAccount represents an account's initial state
type TestAccount struct {
	Balance string            `json:"balance"`
	Code    string            `json:"code"`
	Nonce   string            `json:"nonce"`
	Storage map[string]string `json:"storage"`
}

// TestTransaction contains transaction data for the test
type TestTransaction struct {
	Data                 []string `json:"data"`
	GasLimit             []string `json:"gasLimit"`
	GasPrice             string   `json:"gasPrice"`
	Nonce                string   `json:"nonce"`
	To                   string   `json:"to"`
	Value                []string `json:"value"`
	Sender               string   `json:"sender"`
	SecretKey            string   `json:"secretKey"`
	MaxFeePerGas         string   `json:"maxFeePerGas,omitempty"`
	MaxPriorityFeePerGas string   `json:"maxPriorityFeePerGas,omitempty"`
}

// PostState represents expected state after transaction execution
type PostState struct {
	Hash    string                 `json:"hash"`
	Logs    string                 `json:"logs"`
	Indexes map[string]interface{} `json:"indexes"`
}

// ParseTestFile reads and parses an Ethereum test JSON file
func ParseTestFile(path string) (map[string]EthereumTest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var tests map[string]EthereumTest
	if err := json.Unmarshal(data, &tests); err != nil {
		return nil, err
	}

	return tests, nil
}
