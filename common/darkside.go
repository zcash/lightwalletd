package common

import (
    "errors"
    "encoding/json"
    "os"
    "fmt"
    "bufio"
	//"github.com/zcash/lightwalletd/parser"
)

func DarkSideRawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
    startingBlockHeight := 1000
    saplingActivation := startingBlockHeight
    chainName := "main"
    branchID := "2bb40e60"

	//block_header := parser.NewBlockHeader()

    switch method {
    case "getblockchaininfo":
        // TODO: there has got to be a better way!
        data := make(map[string]interface{})
        data["chain"] = chainName
        data["upgrades"] = make(map[string]interface{})
        data["upgrades"].(map[string]interface{})["76b809bb"] = make(map[string]interface{})
        data["upgrades"].(map[string]interface{})["76b809bb"].(map[string]interface{})["activationheight"] = saplingActivation
        data["headers"] = blockHeight
        data["consensus"] = make(map[string]interface{})
        data["consensus"].(map[string]interface{})["nextblock"] = branchID

        return json.Marshal(data)

    case "getblock":
        var height string
        err := json.Unmarshal(params[0], &height)
        if err != nil {
		    return nil, errors.New("Failed to parse getblock request.")
        }
        //var verbosity string
        //err = json.Unmarshal(params[1], &verbosity)
        //if err != nil {
		//    return nil, errors.New("Failed to parse getblock request.")
        //}

        //print(verbosity)
        //if verbosity != "0" {
		//    return nil, errors.New(verbosity)
        //}

        testBlocks, err := os.Open("./testdata/blocks")
        if err != nil {
            os.Stderr.WriteString(fmt.Sprint("Error:", err))
            os.Exit(1)
        }
        scan := bufio.NewScanner(testBlocks)
        var blocks [][]byte
        for scan.Scan() { // each line (block)
            block := scan.Bytes()
            // Enclose the hex string in quotes (to make it json, to match what's
            // returned by the RPC)
            block = []byte("\"" + string(block) + "\"")
            blocks = append(blocks, block)
        }

        // TODO: return the block at height 'height' from the local store
		return blocks[0], nil

    case "getaddresstxids":
        // Not required for minimal reorg testing.
		return nil, errors.New("Not implemented yet.")

    case "getrawtransaction":
        // Not required for minimal reorg testing.
		return nil, errors.New("Not implemented yet.")

    case "sendrawtransaction":
        // TODO: save the transaction to a buffer the test client can access
		return nil, errors.New("Not implemented yet.")

    default:
        return nil, errors.New("There was an attempt to call an unsupported RPC.")
    }
}
