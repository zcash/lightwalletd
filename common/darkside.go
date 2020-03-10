package common

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"time"
)

type DarksideZcashdState struct {
	start_height       int
	sapling_activation int
	branch_id          string
	chain_name         string
	// Should always be nonempty. Index 0 is the block at height start_height.
	blocks                []string
	incoming_transactions [][]byte
	server_start          time.Time
}

var state *DarksideZcashdState = nil

func DarkSideRawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {

	if state == nil {
		state = &DarksideZcashdState{
			start_height:          1000,
			sapling_activation:    1000,
			branch_id:             "2bb40e60",
			chain_name:            "main",
			blocks:                make([]string, 0),
			incoming_transactions: make([][]byte, 0),
			server_start:          time.Now(),
		}

		testBlocks, err := os.Open("./testdata/default-darkside-blocks")
		if err != nil {
			Log.Fatal("Error loading default darksidewalletd blocks")
		}
		scan := bufio.NewScanner(testBlocks)
		for scan.Scan() { // each line (block)
			block := scan.Bytes()
			state.blocks = append(state.blocks, string(block))
		}
	}

	if time.Now().Sub(state.server_start).Minutes() >= 30 {
		Log.Fatal("Shutting down darksidewalletd to prevent accidental deployment in production.")
	}

	switch method {
	case "getblockchaininfo":
		// TODO: there has got to be a better way to construct this!
		data := make(map[string]interface{})
		data["chain"] = state.chain_name
		data["upgrades"] = make(map[string]interface{})
		data["upgrades"].(map[string]interface{})["76b809bb"] = make(map[string]interface{})
		data["upgrades"].(map[string]interface{})["76b809bb"].(map[string]interface{})["activationheight"] = state.sapling_activation
		data["headers"] = state.start_height + len(state.blocks) - 1
		data["consensus"] = make(map[string]interface{})
		data["consensus"].(map[string]interface{})["nextblock"] = state.branch_id

		return json.Marshal(data)

	case "getblock":
		var height string
		err := json.Unmarshal(params[0], &height)
		if err != nil {
			return nil, errors.New("Failed to parse getblock request.")
		}

		height_i, err := strconv.Atoi(height)
		if err != nil {
			return nil, errors.New("Error parsing height as integer.")
		}
		index := height_i - state.start_height

		if index == len(state.blocks) {
			// The current ingestor keeps going until it sees this error,
			// meaning it's up to the latest height.
			return nil, errors.New("-8:")
		}

		if index < 0 || index > len(state.blocks) {
			// If an integration test can reach this, it could be a bug, so generate an error.
			Log.Errorf("getblock request made for out-of-range height %d (have %d to %d)", height_i, state.start_height, state.start_height+len(state.blocks)-1)
			return nil, errors.New("-8:")
		}

		return []byte("\"" + state.blocks[index] + "\""), nil

	case "getaddresstxids":
		// Not required for minimal reorg testing.
		return nil, errors.New("Not implemented yet.")

	case "getrawtransaction":
		// Not required for minimal reorg testing.
		return nil, errors.New("Not implemented yet.")

	case "sendrawtransaction":
		var rawtx string
		err := json.Unmarshal(params[0], &rawtx)
		if err != nil {
			return nil, errors.New("Failed to parse sendrawtransaction JSON.")
		}
		txbytes, err := hex.DecodeString(rawtx)
		if err != nil {
			return nil, errors.New("Failed to parse sendrawtransaction value as a hex string.")
		}
		state.incoming_transactions = append(state.incoming_transactions, txbytes)
		return nil, nil

	case "x_setstate":
		var new_state map[string]interface{}

		err := json.Unmarshal(params[0], &new_state)
		if err != nil {
			Log.Fatal("Could not unmarshal the provided state.")
		}

		block_strings := make([]string, 0)
		for _, block_str := range new_state["blocks"].([]interface{}) {
			block_strings = append(block_strings, block_str.(string))
		}

		state = &DarksideZcashdState{
			start_height:          int(new_state["start_height"].(float64)),
			sapling_activation:    int(new_state["sapling_activation"].(float64)),
			branch_id:             new_state["branch_id"].(string),
			chain_name:            new_state["chain_name"].(string),
			blocks:                block_strings,
			incoming_transactions: state.incoming_transactions,
			server_start:          state.server_start,
		}

		return nil, nil

	case "x_getincomingtransactions":
		txlist := "["
		for i, tx := range state.incoming_transactions {
			txlist += "\"" + hex.EncodeToString(tx) + "\""
			// add commas after all but the last
			if i < len(state.incoming_transactions)-1 {
				txlist += ", "
			}
		}
		txlist += "]"

		return []byte(txlist), nil

	default:
		return nil, errors.New("There was an attempt to call an unsupported RPC.")
	}
}
