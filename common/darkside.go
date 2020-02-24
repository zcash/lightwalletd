package common

import (
    "errors"
    "encoding/json"
    "os"
    "bufio"
    "strconv"
    "time"
	//"github.com/zcash/lightwalletd/parser"
)

type DarksideZcashdState struct {
    start_height int
    sapling_activation int
    branch_id string
    chain_name string
    // Should always be nonempty. Index 0 is the block at height start_height.
    blocks []string
    incoming_transactions []string
    server_start time.Time
}

// TODO
// 1. RPC for setting state.blocks
// 2. RPC for setting other chain state data
// 3. RPC for accesssing incoming_transactions

var state *DarksideZcashdState = nil

func DarkSideRawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {

    if state == nil {
        state = &DarksideZcashdState{
            start_height: 1000,
            sapling_activation: 1000,
            branch_id: "2bb40e60",
            chain_name: "main",
            blocks: make([]string, 0),
            incoming_transactions: make([]string, 0),
            server_start: time.Now(),
        }

        testBlocks, err := os.Open("./testdata/blocks")
        if err != nil {
            Log.Fatal("Error loading testdata blocks")
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
            // If an integration test can reach this, it's a bug, so
            // crash the entire lightwalletd to make it obvious.
            Log.Fatal("getblock request made for out-of-range height")
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
        state.incoming_transactions = append(state.incoming_transactions, rawtx)
		return nil, errors.New("Not implemented yet.")

    default:
        return nil, errors.New("There was an attempt to call an unsupported RPC.")
    }
}
