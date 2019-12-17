package common

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/btcsuite/btcd/rpcclient"
	"github.com/pkg/errors"
)

func GetSaplingInfo(rpcClient *rpcclient.Client) (int, int, string, string, error) {
	result, rpcErr := rpcClient.RawRequest("getblockchaininfo", make([]json.RawMessage, 0))

	var err error
	var errCode int64

	// For some reason, the error responses are not JSON
	if rpcErr != nil {
		errParts := strings.SplitN(rpcErr.Error(), ":", 2)
		errCode, err = strconv.ParseInt(errParts[0], 10, 32)
		if err == nil && errCode == -8 {
			return -1, -1, "", "", nil
		}
		return -1, -1, "", "", errors.Wrap(rpcErr, "error requesting block")
	}

	var f interface{}
	err = json.Unmarshal(result, &f)
	if err != nil {
		return -1, -1, "", "", errors.Wrap(err, "error reading JSON response")
	}

	chainName := f.(map[string]interface{})["chain"].(string)

	upgradeJSON := f.(map[string]interface{})["upgrades"]
	saplingJSON := upgradeJSON.(map[string]interface{})["76b809bb"] // Sapling ID
	saplingHeight := saplingJSON.(map[string]interface{})["activationheight"].(float64)

	blockHeight := f.(map[string]interface{})["headers"].(float64)

	consensus := f.(map[string]interface{})["consensus"]
	branchID := consensus.(map[string]interface{})["nextblock"].(string)

	return int(saplingHeight), int(blockHeight), chainName, branchID, nil
}
