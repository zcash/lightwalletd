package common

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/zcash/lightwalletd/parser"
)

type darksideState struct {
	inited            bool
	startHeight       int
	saplingActivation int
	branchID          string
	chainName         string
	// Should always be nonempty. Index 0 is the block at height start_height.
	blocks               [][]byte // full blocks, binary, as from zcashd getblock rpc
	incomingTransactions [][]byte // full transactions, binary, zcashd getrawtransaction txid
}

var state darksideState

func DarksideIsEnabled() bool {
	return state.inited
}

func DarksideInit() {
	state = darksideState{
		inited:               true,
		startHeight:          -1,
		saplingActivation:    -1,
		branchID:             "2bb40e60", // Blossom
		chainName:            "darkside",
		blocks:               make([][]byte, 0),
		incomingTransactions: make([][]byte, 0),
	}
	RawRequest = darksideRawRequest
	f := "testdata/darkside/init-blocks"
	testBlocks, err := os.Open(f)
	if err != nil {
		Log.Warn("Error opening default darksidewalletd blocks file", f)
		return
	}
	if err = readBlocks(testBlocks); err != nil {
		Log.Warn("Error loading default darksidewalletd blocks")
	}
	go func() {
		time.Sleep(30 * time.Minute)
		Log.Fatal("Shutting down darksidewalletd to prevent accidental deployment in production.")
	}()
}

// DarksideAddBlock adds a single block to the blocks list.
func DarksideAddBlock(blockHex string) error {
	if blockHex == "404: Not Found" {
		// special case error (http resource not found, bad pathname)
		return errors.New(blockHex)
	}
	blockData, err := hex.DecodeString(blockHex)
	if err != nil {
		return err
	}
	block := parser.NewBlock()
	rest, err := block.ParseFromSlice(blockData)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return errors.New("block serialization is too long")
	}
	blockHeight := block.GetHeight()
	// first block, add to existing blocks slice if possible
	if blockHeight > state.startHeight+len(state.blocks) {
		// The new block can't contiguously extend the existing
		// range, so we have to drop the existing range.
		state.blocks = state.blocks[:0]
	} else if blockHeight < state.startHeight {
		// This block will replace the entire existing range.
		state.blocks = state.blocks[:0]
	} else {
		// Drop the block that will be overwritten, and its children.
		state.blocks = state.blocks[:blockHeight-state.startHeight]
	}
	if len(state.blocks) == 0 {
		state.startHeight = blockHeight
	} else {
		// Set this block's prevhash.
		prevblock := parser.NewBlock()
		rest, err := prevblock.ParseFromSlice(state.blocks[len(state.blocks)-1])
		if err != nil {
			return err
		}
		if len(rest) != 0 {
			return errors.New("block is too long")
		}
		copy(blockData[4:4+32], prevblock.GetEncodableHash())
	}
	if state.saplingActivation < 0 {
		state.saplingActivation = blockHeight
	}
	state.blocks = append(state.blocks, blockData)
	return nil
}

func readBlocks(src io.Reader) error {
	// some blocks are too large, especially when encoded in hex, for the
	// default buffer size, so set up a larger one; 8mb should be enough.
	scan := bufio.NewScanner(src)
	var scanbuf []byte
	scan.Buffer(scanbuf, 8*1000*1000)
	for scan.Scan() { // each line (block)
		if err := DarksideAddBlock(scan.Text()); err != nil {
			return err
		}
	}
	if scan.Err() != nil {
		return scan.Err()
	}
	return nil
}

func DarksideSetMetaState(sa int32, bi, cn string) error {
	state.saplingActivation = int(sa)
	state.branchID = bi
	state.chainName = cn
	return nil
}

func DarksideGetIncomingTransactions() [][]byte {
	return state.incomingTransactions
}

func DarksideSetBlocksURL(url string) error {
	if strings.HasPrefix(url, "file:") && len(url) >= 6 && url[5] != '/' {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		url = "file:" + dir + string(os.PathSeparator) + url[5:]
	}
	cmd := exec.Command("curl", "--silent", "--show-error", url)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer cmd.Wait()
	err = readBlocks(stdout)
	if err != nil {
		return err
	}
	stderroutstr, err := ioutil.ReadAll(stderr)
	if err != nil {
		return err
	}
	if len(stderroutstr) > 0 {
		return errors.New(string(stderroutstr))
	}
	return nil
}

func DarksideSendTransaction(txHex []byte) ([]byte, error) {
	// Need to parse the transaction to return its hash, plus it's
	// good error checking.
	txbytes, err := hex.DecodeString(string(txHex))
	if err != nil {
		return nil, err
	}
	tx := parser.NewTransaction()
	rest, err := tx.ParseFromSlice(txbytes)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, errors.New("transaction serialization is too long")
	}
	state.incomingTransactions = append(state.incomingTransactions, txbytes)
	return tx.GetDisplayHash(), nil
}

func darksideRawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "getblockchaininfo":
		type upgradeinfo struct {
			// there are other fields that aren't needed here, omit them
			ActivationHeight int `json:"activationheight"`
		}
		type consensus struct {
			Nextblock string `json:"nextblock"`
			Chaintip  string `json:"chaintip"`
		}
		blockchaininfo := struct {
			Chain     string                 `json:"chain"`
			Upgrades  map[string]upgradeinfo `json:"upgrades"`
			Headers   int                    `json:"headers"`
			Consensus consensus              `json:"consensus"`
		}{
			Chain: state.chainName,
			Upgrades: map[string]upgradeinfo{
				"76b809bb": {ActivationHeight: state.saplingActivation},
			},
			Headers:   state.startHeight + len(state.blocks) - 1,
			Consensus: consensus{state.branchID, state.branchID},
		}
		return json.Marshal(blockchaininfo)

	case "getblock":
		var heightStr string
		err := json.Unmarshal(params[0], &heightStr)
		if err != nil {
			return nil, errors.New("failed to parse getblock request")
		}

		height, err := strconv.Atoi(heightStr)
		if err != nil {
			return nil, errors.New("error parsing height as integer")
		}
		index := height - state.startHeight

		const notFoundErr = "-8:"
		if state.saplingActivation < 0 || index == len(state.blocks) {
			// The current ingestor keeps going until it sees this error,
			// meaning it's up to the latest height.
			return nil, errors.New(notFoundErr)
		}

		if index < 0 || index > len(state.blocks) {
			// If an integration test can reach this, it could be a bug, so generate an error.
			Log.Errorf("getblock request made for out-of-range height %d (have %d to %d)",
				height, state.startHeight, state.startHeight+len(state.blocks)-1)
			return nil, errors.New(notFoundErr)
		}
		return []byte("\"" + hex.EncodeToString(state.blocks[index]) + "\""), nil

	case "getaddresstxids":
		// Not required for minimal reorg testing.
		return nil, errors.New("not implemented yet")

	case "getrawtransaction":
		// Not required for minimal reorg testing.
		return darksideGetRawTransaction(params)

	case "sendrawtransaction":
		var rawtx string
		err := json.Unmarshal(params[0], &rawtx)
		if err != nil {
			return nil, errors.New("failed to parse sendrawtransaction JSON")
		}
		txbytes, err := hex.DecodeString(rawtx)
		if err != nil {
			return nil, errors.New("failed to parse sendrawtransaction value as a hex string")
		}
		state.incomingTransactions = append(state.incomingTransactions, txbytes)
		return nil, nil

	default:
		return nil, errors.New("there was an attempt to call an unsupported RPC")
	}
}

func darksideGetRawTransaction(params []json.RawMessage) (json.RawMessage, error) {
	// remove the double-quotes from the beginning and end of the hex txid string
	txbytes, err := hex.DecodeString(string(params[0][1 : 1+64]))
	if err != nil {
		return nil, errors.New("-9: " + err.Error())
	}
	// Linear search for the tx, somewhat inefficient but this is test code
	// and there aren't many blocks. If this becomes a performance problem,
	// we can maintain a map of transactions indexed by txid.
	for _, b := range state.blocks {
		block := parser.NewBlock()
		rest, err := block.ParseFromSlice(b)
		if err != nil {
			// this would be strange; we've already parsed this block
			return nil, errors.New("-9: " + err.Error())
		}
		if len(rest) != 0 {
			return nil, errors.New("-9: block serialization is too long")
		}
		for _, tx := range block.Transactions() {
			if bytes.Equal(tx.GetDisplayHash(), txbytes) {
				reply := struct {
					Hex    string `json:"hex"`
					Height int    `json:"height"`
				}{hex.EncodeToString(tx.Bytes()), block.GetHeight()}
				return json.Marshal(reply)
			}
		}
	}
	return nil, errors.New("-5: No information available about transaction")
}
