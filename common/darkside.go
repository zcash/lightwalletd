package common

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zcash/lightwalletd/parser"
	"github.com/zcash/lightwalletd/walletrpc"
)

type darksideState struct {
	resetted    bool
	startHeight int // activeBlocks[0] corresponds to this height
	branchID    string
	chainName   string
	cache       *BlockCache

	// This is the highest (latest) block height currently being presented
	// by the mock zcashd.
	latestHeight int

	// Size of the Sapling commitment tree as of `startHeight - 1`.
	startSaplingTreeSize uint32
	// Size of the Orchard commitment tree as of `startHeight - 1`.
	startOrchardTreeSize uint32

	// These blocks (up to and including tip) are presented by mock zcashd.
	// activeBlocks[0] is the block at height startHeight.
	activeBlocks []*activeBlock // full blocks, binary, as from zcashd getblock rpc

	// Staged blocks are waiting to be applied (by ApplyStaged()) to activeBlocks.
	// They are in order of arrival (not necessarily sorted by height), and are
	// applied in arrival order.
	stagedBlocks [][]byte // full blocks, binary

	// These are full transactions as received from the wallet by SendTransaction().
	// They are conceptually in the mempool. They are not yet available to be fetched
	// by GetTransaction(). They can be fetched by darkside GetIncomingTransaction().
	incomingTransactions [][]byte

	// These transactions come from StageTransactions(); they will be merged into
	// activeBlocks by ApplyStaged() (and this list then cleared).
	stagedTransactions []stagedTx

	// Unordered list of replies
	getAddressUtxos []ZcashdRpcReplyGetaddressutxos

	stagedTreeStates       map[uint64]*DarksideTreeState
	stagedTreeStatesByHash map[string]*DarksideTreeState

	// This is a one-entry cache performance cheat.
	cacheBlockHash  string
	cacheBlockIndex int

	// Cache of artificial z_getsubtreebyindex subtree entries,
	// indexed by protocol (currently, sapling (0) or orchard (1)).
	subtrees map[walletrpc.ShieldedProtocol]darksideProtocolSubtreeRoots
}

var state darksideState

// mutex protects `state`; it's not within `state` because the
// `state` can be reallocated (by Reset()), and that action
// should be protected.
var mutex sync.Mutex

type activeBlock struct {
	bytes           []byte
	saplingTreeSize uint32
	orchardTreeSize uint32
}

type stagedTx struct {
	height         int
	saplingOutputs int
	orchardActions int
	bytes          []byte
}

type DarksideTreeState struct {
	Network     string
	Height      uint64
	Hash        string
	Time        uint32
	SaplingTree string
	OrchardTree string
}

type darksideSubtree struct {
	root      []byte
	endHash   []byte
	endHeight int
}
type darksideProtocolSubtreeRoots struct {
	startIndex uint32
	subtrees   []darksideSubtree
}

// DarksideEnabled is true if --darkside-very-insecure was given on
// the command line.
var DarksideEnabled bool

func darksideSetTxID(tx *parser.Transaction) {
	// SHA256d
	// This is correct for V4 transactions, but not for V5, but in this test
	// environment, it's harmless (the incorrect txid calculation can't be
	// detected). This will be fixed when lightwalletd calculates txids correctly .
	digest := sha256.Sum256(tx.Bytes())
	digest = sha256.Sum256(digest[:])
	tx.SetTxID(digest[:])
}

func darksideSetBlockTxID(block *parser.Block) {
	for _, tx := range block.Transactions() {
		darksideSetTxID(tx)
	}
}

// DarksideInit should be called once at startup in darksidewalletd mode.
func DarksideInit(c *BlockCache, timeout int) {
	Log.Info("Darkside mode running")
	DarksideEnabled = true
	state.cache = c
	RawRequest = darksideRawRequest
	go func() {
		time.Sleep(time.Duration(timeout) * time.Minute)
		Log.Fatal("Shutting down darksidewalletd to prevent accidental deployment in production.")
	}()
}

// DarksideReset allows the wallet test code to specify values
// that are returned by GetLightdInfo().
func DarksideReset(sa int, bi, cn string, sst, sot uint32) error {
	Log.Info("DarksideReset(saplingActivation=", sa, ")")
	mutex.Lock()
	defer mutex.Unlock()
	stopIngestor()
	state = darksideState{
		resetted:               true,
		startHeight:            sa,
		latestHeight:           -1,
		branchID:               bi,
		chainName:              cn,
		startSaplingTreeSize:   sst,
		startOrchardTreeSize:   sot,
		cache:                  state.cache,
		activeBlocks:           make([]*activeBlock, 0),
		stagedBlocks:           make([][]byte, 0),
		incomingTransactions:   make([][]byte, 0),
		stagedTransactions:     make([]stagedTx, 0),
		stagedTreeStates:       make(map[uint64]*DarksideTreeState),
		stagedTreeStatesByHash: make(map[string]*DarksideTreeState),
		subtrees:               make(map[walletrpc.ShieldedProtocol]darksideProtocolSubtreeRoots),
	}
	state.cache.Reset(sa)
	return nil
}

// DarksideAddBlock adds a single block to the active blocks list.
func addBlockActive(blockBytes []byte) error {
	block := parser.NewBlock()
	rest, err := block.ParseFromSlice(blockBytes)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return errors.New("block serialization is too long")
	}
	blockHeight := block.GetHeight()
	// first block, add to existing blocks slice if possible
	if blockHeight > state.startHeight+len(state.activeBlocks) {
		return errors.New(fmt.Sprint("adding block at height ", blockHeight,
			" would create a gap in the blockchain"))
	}
	if blockHeight < state.startHeight {
		return errors.New(fmt.Sprint("adding block at height ", blockHeight,
			" is lower than Sapling activation height ", state.startHeight))
	}
	// Determine the Sapling and Orchard commitment tree sizes for the new block.
	countSaplingOutputs := func(block *parser.Block) uint32 {
		var count = 0
		for _, tx := range block.Transactions() {
			count += tx.SaplingOutputsCount()
		}
		return uint32(count)
	}
	countOrchardActions := func(block *parser.Block) uint32 {
		var count = 0
		for _, tx := range block.Transactions() {
			count += tx.OrchardActionsCount()
		}
		return uint32(count)
	}
	var prevSaplingTreeSize uint32
	var prevOrchardTreeSize uint32
	if blockHeight-state.startHeight > 0 {
		// The new block connects to the previous one.
		prevSaplingTreeSize = state.activeBlocks[blockHeight-state.startHeight-1].saplingTreeSize
		prevOrchardTreeSize = state.activeBlocks[blockHeight-state.startHeight-1].orchardTreeSize
	} else {
		// This is the first block.
		prevSaplingTreeSize = state.startSaplingTreeSize
		prevOrchardTreeSize = state.startOrchardTreeSize
	}
	// Drop the block that will be overwritten, and its children, then add block.
	state.activeBlocks = state.activeBlocks[:blockHeight-state.startHeight]
	state.activeBlocks = append(state.activeBlocks,
		&activeBlock{
			bytes:           blockBytes,
			saplingTreeSize: prevSaplingTreeSize + countSaplingOutputs(block),
			orchardTreeSize: prevOrchardTreeSize + countOrchardActions(block),
		})
	return nil
}

// Set missing prev hashes of the blocks in the active chain
func setPrevhash() {
	var prevhash []byte
	for _, activeBlock := range state.activeBlocks {
		// Set this block's prevhash.
		block := parser.NewBlock()
		rest, err := block.ParseFromSlice(activeBlock.bytes)
		if err != nil {
			Log.Fatal(err)
		}
		if len(rest) != 0 {
			Log.Fatal(errors.New("block is too long"))
		}
		if prevhash != nil {
			copy(activeBlock.bytes[4:4+32], prevhash)
		}
		prevhash = block.GetEncodableHash()
		Log.Info("Darkside active block height ", block.GetHeight(), " hash ",
			hex.EncodeToString(block.GetDisplayHash()),
			" txcount ", block.GetTxCount())
	}
}

// DarksideApplyStaged moves the staging area to the active block list.
// If this returns an error, the state could be weird; perhaps it may
// be better to simply crash.
func DarksideApplyStaged(height int) error {
	mutex.Lock()
	defer mutex.Unlock()
	if !state.resetted {
		return errors.New("please call Reset first")
	}
	Log.Info("DarksideApplyStaged(height=", height, ")")
	if height < state.startHeight {
		return errors.New(fmt.Sprint("height ", height,
			" is less than sapling activation height ", state.startHeight))
	}
	// Move the staged blocks into active list
	stagedBlocks := state.stagedBlocks
	state.stagedBlocks = nil
	for _, blockBytes := range stagedBlocks {
		if err := addBlockActive(blockBytes); err != nil {
			return err
		}
	}
	if len(state.activeBlocks) == 0 {
		return errors.New("no active blocks after applying staged blocks")
	}

	// Add staged transactions into blocks. Note we're not trying to
	// recover to the initial state; maybe it's better to just crash
	// on errors.
	stagedTransactions := state.stagedTransactions
	state.stagedTransactions = nil
	for _, tx := range stagedTransactions {
		if tx.height < state.startHeight {
			return errors.New("transaction height too low")
		}
		if tx.height >= state.startHeight+len(state.activeBlocks) {
			return errors.New("transaction height too high")
		}
		block := state.activeBlocks[tx.height-state.startHeight].bytes
		// The next one or 3 bytes encode the number of transactions to follow,
		// little endian.
		nTxFirstByte := block[1487]
		switch {
		case nTxFirstByte < 252:
			block[1487]++
		case nTxFirstByte == 252:
			// incrementing to 253, requires "253" followed by 2-byte length,
			// extend the block by two bytes, shift existing transaction bytes
			block = append(block, 0, 0)
			copy(block[1490:], block[1488:len(block)-2])
			block[1487] = 253
			block[1488] = 253
			block[1489] = 0
		case nTxFirstByte == 253:
			block[1488]++
			if block[1488] == 0 {
				// wrapped around
				block[1489]++
			}
		default:
			// no need to worry about more than 64k transactions
			Log.Fatal("unexpected compact transaction count ", nTxFirstByte,
				", can't support more than 64k transactions in a block")
		}
		block[68]++ // hack HashFinalSaplingRoot to mod the block hash
		block = append(block, tx.bytes...)
		state.activeBlocks[tx.height-state.startHeight].bytes = block
		// Now increment this and every subsequent block's commitment tree sizes.
		for _, b := range state.activeBlocks[tx.height-state.startHeight:] {
			b.saplingTreeSize += uint32(tx.saplingOutputs)
			b.orchardTreeSize += uint32(tx.orchardActions)
		}
	}
	maxHeight := state.startHeight + len(state.activeBlocks) - 1
	if height > maxHeight {
		height = maxHeight
	}
	setPrevhash()
	state.latestHeight = height
	Log.Info("darkside: active blocks from ", state.startHeight,
		" to ", state.startHeight+len(state.activeBlocks)-1,
		", latest presented height ", state.latestHeight)

	// The block ingestor can only run if there are blocks
	if len(state.activeBlocks) > 0 {
		startIngestor(state.cache)
	} else {
		stopIngestor()
	}
	return nil
}

// DarksideGetIncomingTransactions returns all transactions we're
// received via SendTransaction().
func DarksideGetIncomingTransactions() [][]byte {
	var r [][]byte
	mutex.Lock()
	r = append(r, state.incomingTransactions...)
	mutex.Unlock()
	return r
}

// Add the serialized block to the staging list, but do some sanity checks first.
func darksideStageBlock(caller string, b []byte) error {
	block := parser.NewBlock()
	rest, err := block.ParseFromSlice(b)
	if err != nil {
		Log.Error("stage block error: ", err)
		return err
	}
	if len(rest) != 0 {
		return errors.New("block serialization is too long")
	}
	Log.Info(caller, "DarksideStageBlock(height=", block.GetHeight(), ")")
	if block.GetHeight() < state.startHeight {
		return errors.New(fmt.Sprint("block height ", block.GetHeight(),
			" is less than sapling activation height ", state.startHeight))
	}
	state.stagedBlocks = append(state.stagedBlocks, b)
	return nil
}

// DarksideStageBlocks opens and reads blocks from the given URL and
// adds them to the staging area.
func DarksideStageBlocks(url string) error {
	mutex.Lock()
	defer mutex.Unlock()
	if !state.resetted {
		return errors.New("please call Reset first")
	}
	Log.Info("DarksideStageBlocks(url=", url, ")")
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// some blocks are too large, especially when encoded in hex, for the
	// default buffer size, so set up a larger one; 8mb should be enough.
	scan := bufio.NewScanner(resp.Body)
	var scanbuf []byte
	scan.Buffer(scanbuf, 8*1000*1000)
	for scan.Scan() { // each line (block)
		blockHex := scan.Text()
		if blockHex == "404: Not Found" {
			// special case error (http resource not found, bad pathname)
			return errors.New(blockHex)
		}
		blockBytes, err := hex.DecodeString(blockHex)
		if err != nil {
			return err
		}
		if err = darksideStageBlock("DarksideStageBlocks", blockBytes); err != nil {
			return err
		}
	}
	return scan.Err()
}

// DarksideStageBlockStream adds the block to the staging area
func DarksideStageBlockStream(blockHex string) error {
	mutex.Lock()
	defer mutex.Unlock()
	if !state.resetted {
		return errors.New("please call Reset first")
	}
	Log.Info("DarksideStageBlocksStream()")
	blockBytes, err := hex.DecodeString(blockHex)
	if err != nil {
		return err
	}
	if err = darksideStageBlock("DarksideStageBlockStream", blockBytes); err != nil {
		return err
	}
	return nil
}

// DarksideStageBlocksCreate creates empty blocks and adds them to the staging area.
func DarksideStageBlocksCreate(height int32, nonce int32, count int32) error {
	mutex.Lock()
	defer mutex.Unlock()
	if !state.resetted {
		return errors.New("please call Reset first")
	}
	Log.Info("DarksideStageBlocksCreate(height=", height, ", nonce=", nonce, ", count=", count, ")")
	for i := 0; i < int(count); i++ {

		fakeCoinbase := "0400008085202f890100000000000000000000000000000000000000000000000000" +
			"00000000000000ffffffff2a03d12c0c00043855975e464b8896790758f824ceac97836" +
			"22c17ed38f1669b8a45ce1da857dbbe7950e2ffffffff02a0ebce1d000000001976a914" +
			"7ed15946ec14ae0cd8fa8991eb6084452eb3f77c88ac405973070000000017a914e445cf" +
			"a944b6f2bdacefbda904a81d5fdd26d77f8700000000000000000000000000000000000000"

		// This coinbase transaction was pulled from block 797905, whose
		// little-endian encoding is 0xD12C0C00. Replace it with the block
		// number we want.
		fakeCoinbase = strings.Replace(fakeCoinbase, "d12c0c00",
			fmt.Sprintf("%02x", height&0xFF)+
				fmt.Sprintf("%02x", (height>>8)&0xFF)+
				fmt.Sprintf("%02x", (height>>16)&0xFF)+
				fmt.Sprintf("%02x", (height>>24)&0xFF), 1)
		fakeCoinbaseBytes, err := hex.DecodeString(fakeCoinbase)
		if err != nil {
			Log.Fatal(err)
		}

		hashOfTxnsAndHeight := sha256.Sum256([]byte(string(nonce) + "#" + string(height)))
		blockHeader := &parser.BlockHeader{
			RawBlockHeader: &parser.RawBlockHeader{
				Version:              4,                      // start: 0
				HashPrevBlock:        make([]byte, 32),       // start: 4
				HashMerkleRoot:       hashOfTxnsAndHeight[:], // start: 36
				HashFinalSaplingRoot: make([]byte, 32),       // start: 68
				Time:                 1,                      // start: 100
				NBitsBytes:           make([]byte, 4),        // start: 104
				Nonce:                make([]byte, 32),       // start: 108
				Solution:             make([]byte, 1344),     // starts: 140, 143
			}, // length: 1487
		}

		headerBytes, err := blockHeader.MarshalBinary()
		if err != nil {
			Log.Fatal(err)
		}
		blockBytes := make([]byte, 0)
		blockBytes = append(blockBytes, headerBytes...)
		blockBytes = append(blockBytes, byte(1))
		blockBytes = append(blockBytes, fakeCoinbaseBytes...)
		if err = darksideStageBlock("DarksideStageBlockCreate", blockBytes); err != nil {
			// This should never fail since we created the block ourselves.
			return err
		}
		height++
	}
	return nil
}

// DarksideClearIncomingTransactions empties the incoming transaction list.
func DarksideClearIncomingTransactions() {
	mutex.Lock()
	state.incomingTransactions = make([][]byte, 0)
	mutex.Unlock()
}

func darksideRawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
	mutex.Lock()
	defer mutex.Unlock()
	switch method {
	case "getblockchaininfo":
		if len(state.activeBlocks) == 0 {
			return nil, errors.New("GetLightdInfo requires at least one block, " +
				"please stage and apply one or more blocks.")
		}
		index := state.latestHeight - state.startHeight
		block := parser.NewBlock()
		block.ParseFromSlice(state.activeBlocks[index].bytes)
		hash := hex.EncodeToString(block.GetDisplayHash())
		blockchaininfo := &ZcashdRpcReplyGetblockchaininfo{
			Chain: state.chainName,
			Upgrades: map[string]Upgradeinfo{
				"76b809bb": {ActivationHeight: state.startHeight},
			},
			Blocks:        state.latestHeight,
			Consensus:     ConsensusInfo{state.branchID, state.branchID},
			BestBlockHash: hash,
		}
		return json.Marshal(blockchaininfo)

	case "getinfo":
		info := &ZcashdRpcReplyGetinfo{
			Build:      "darksidewallet-build",
			Subversion: "darksidewallet-subversion",
		}
		return json.Marshal(info)

	case "getblock":
		var heightOrHashStr string
		err := json.Unmarshal(params[0], &heightOrHashStr)
		if err != nil {
			return nil, errors.New("failed to parse getblock request")
		}
		var blockIndex int
		if len(heightOrHashStr) < 64 {
			// argument is a height
			height, err := strconv.Atoi(heightOrHashStr)
			if err != nil {
				return nil, errors.New("error parsing height as integer")
			}
			const notFoundErr = "-8:"
			if len(state.activeBlocks) == 0 {
				return nil, errors.New(notFoundErr)
			}
			if height > state.latestHeight {
				return nil, errors.New(notFoundErr)
			}
			if height < state.startHeight {
				return nil, errors.New(fmt.Sprint("getblock: requesting height ", height,
					" is less than sapling activation height"))
			}
			blockIndex = height - state.startHeight
			if blockIndex >= len(state.activeBlocks) {
				return nil, errors.New(notFoundErr)
			}
		} else {
			// argument is a block hash
			if state.cacheBlockHash == heightOrHashStr {
				// There is a good chance we'll take this path, much faster than
				// iterating the activeBlocks list.
				blockIndex = state.cacheBlockIndex
			} else {
				var b *activeBlock
				for blockIndex, b = range state.activeBlocks {
					block := parser.NewBlock()
					block.ParseFromSlice(b.bytes)
					if heightOrHashStr == hex.EncodeToString(block.GetDisplayHash()) {
						break
					}
				}
				if blockIndex >= len(state.activeBlocks) {
					return nil, errors.New(fmt.Sprint("getblock: hash ", heightOrHashStr,
						" not found"))
				}
			}
		}
		if len(params) > 1 && string(params[1]) == "1" {
			// verbose mode, all that's currently needed is txid
			block := parser.NewBlock()
			block.ParseFromSlice(state.activeBlocks[blockIndex].bytes)
			darksideSetBlockTxID(block)
			var r struct {
				Tx    []string `json:"tx"`
				Hash  string   `json:"hash"`
				Trees struct {
					Sapling struct {
						Size uint32
					}
					Orchard struct {
						Size uint32
					}
				}
			}
			r.Tx = make([]string, 0)
			for _, tx := range block.Transactions() {
				r.Tx = append(r.Tx, hex.EncodeToString(tx.GetDisplayHash()))
			}
			r.Hash = hex.EncodeToString(block.GetDisplayHash())
			r.Trees.Sapling.Size = state.activeBlocks[blockIndex].saplingTreeSize
			r.Trees.Orchard.Size = state.activeBlocks[blockIndex].orchardTreeSize
			state.cacheBlockHash = r.Hash
			state.cacheBlockIndex = blockIndex
			return json.Marshal(r)
		}
		return json.Marshal(hex.EncodeToString(state.activeBlocks[blockIndex].bytes))

	case "getbestblockhash":
		if len(state.activeBlocks) == 0 {
			Log.Fatal("getbestblockhash: no blocks")
		}
		index := state.latestHeight - state.startHeight
		block := parser.NewBlock()
		block.ParseFromSlice(state.activeBlocks[index].bytes)
		hash := hex.EncodeToString(block.GetDisplayHash())
		return json.Marshal(hash)

	case "getaddresstxids":
		// Not required for minimal reorg testing.
		return nil, errors.New("not implemented yet")

	case "getrawtransaction":
		return darksideGetRawTransaction(params)

	case "sendrawtransaction":
		var rawtx string
		err := json.Unmarshal(params[0], &rawtx)
		if err != nil {
			return nil, errors.New("failed to parse sendrawtransaction JSON")
		}
		txBytes, err := hex.DecodeString(rawtx)
		if err != nil {
			return nil, errors.New("failed to parse sendrawtransaction value as a hex string")
		}
		// Parse the transaction to get its hash (txid).
		tx := parser.NewTransaction()
		rest, err := tx.ParseFromSlice(txBytes)
		if err != nil {
			return nil, err
		}
		if len(rest) != 0 {
			return nil, errors.New("transaction serialization is too long")
		}
		darksideSetTxID(tx)
		state.incomingTransactions = append(state.incomingTransactions, txBytes)

		return []byte(hex.EncodeToString(tx.GetDisplayHash())), nil

	case "getrawmempool":
		reply := make([]string, 0)
		addTxToReply := func(txBytes []byte) {
			ctx := parser.NewTransaction()
			ctx.ParseFromSlice(txBytes)
			darksideSetTxID(ctx)
			reply = append(reply, hex.EncodeToString(ctx.GetDisplayHash()))
		}
		for _, blockBytes := range state.stagedBlocks {
			block := parser.NewBlock()
			block.ParseFromSlice(blockBytes)
			for _, tx := range block.Transactions() {
				addTxToReply(tx.Bytes())
			}
		}
		for _, tx := range state.stagedTransactions {
			addTxToReply(tx.bytes)
		}
		return json.Marshal(reply)

	case "getaddressutxos":
		var req ZcashdRpcRequestGetaddressutxos
		err := json.Unmarshal(params[0], &req)
		if err != nil {
			return nil, errors.New("failed to parse getaddressutxos JSON")
		}
		utxosReply := make([]ZcashdRpcReplyGetaddressutxos, 0)
		for _, utxo := range state.getAddressUtxos {
			for _, a := range req.Addresses {
				if a == utxo.Address {
					utxosReply = append(utxosReply, utxo)
					break
				}
			}
		}
		return json.Marshal(utxosReply)

	case "z_gettreestate":
		var heightOrHashStr string
		err := json.Unmarshal(params[0], &heightOrHashStr)
		if err != nil {
			return nil, errors.New("failed to parse z_gettreestate request")
		}
		var treeState *DarksideTreeState
		if len(heightOrHashStr) < 64 {
			// argument is a height
			height, err := strconv.Atoi(heightOrHashStr)
			if err != nil {
				return nil, errors.New("error parsing height as integer")
			}
			treeState = state.stagedTreeStates[uint64(height)]
		} else {
			treeState = state.stagedTreeStatesByHash[heightOrHashStr]
		}
		if treeState == nil {
			return nil, errors.New(fmt.Sprint(
				"No TreeState exists for the given height or block hash. " +
					"Stage it using AddTreeState() first"))
		}

		zcashdTreeState := &ZcashdRpcReplyGettreestate{}

		zcashdTreeState.Hash = treeState.Hash
		zcashdTreeState.Height = int(treeState.Height)
		zcashdTreeState.Time = treeState.Time
		zcashdTreeState.Sapling.Commitments.FinalState = treeState.SaplingTree

		if treeState.OrchardTree != "" {
			zcashdTreeState.Orchard.Commitments.FinalState = treeState.OrchardTree
		}

		return json.Marshal(zcashdTreeState)

	case "z_getsubtreesbyindex":
		// This is implemented by DarksideGetSubtreeRoots().
		return nil, errors.New("z_getsubtreesbyindex should never be called")

	default:
		return nil, errors.New("there was an attempt to call an unsupported RPC: " + method)
	}
}

// Normally we would implement this functionality in the darksideRawRequest(), but this
// gRPC handler requires calling GetBlock, and we don't have a good way to fake that.
func DarksideGetSubtreeRoots(arg *walletrpc.GetSubtreeRootsArg, resp walletrpc.CompactTxStreamer_GetSubtreeRootsServer) error {
	mutex.Lock()
	defer mutex.Unlock()
	subtrees := state.subtrees[arg.ShieldedProtocol]
	if arg.StartIndex < subtrees.startIndex {
		return errors.New("startIndex too low")
	}
	sliceIndex := arg.StartIndex - subtrees.startIndex
	var limit int = len(subtrees.subtrees) - int(sliceIndex)
	if limit > int(arg.MaxEntries) {
		limit = int(arg.MaxEntries)
	}
	for i := 0; i < limit; i++ {
		s := subtrees.subtrees[int(sliceIndex)+i]
		r := walletrpc.SubtreeRoot{
			RootHash:              s.root,
			CompletingBlockHash:   s.endHash,
			CompletingBlockHeight: uint64(s.endHeight),
		}
		err := resp.Send(&r)
		if err != nil {
			return err
		}
	}
	return nil
}

func darksideGetRawTransaction(params []json.RawMessage) (json.RawMessage, error) {
	if !state.resetted {
		return nil, errors.New("please call Reset first")
	}
	var rawtx string
	err := json.Unmarshal(params[0], &rawtx)
	if err != nil {
		return nil, errors.New("failed to parse getrawtransaction JSON")
	}
	txidBigEndian, err := hex.DecodeString(rawtx)
	if err != nil {
		return nil, errors.New("-9: " + err.Error())
	}
	marshalReply := func(tx *parser.Transaction, height int) []byte {
		switch string(params[1]) {
		case "0":
			txJSON, _ := json.Marshal(hex.EncodeToString(tx.Bytes()))
			return txJSON
		case "1":
			reply := struct {
				Hex    string
				Height int
			}{hex.EncodeToString(tx.Bytes()), height}
			txVerboseJSON, _ := json.Marshal(reply)
			return txVerboseJSON
		default:
			Log.Fatal("darkside only recognizes verbose 0 or 1")
			return nil

		}
	}
	// Linear search for the tx, somewhat inefficient but this is test code
	// and there aren't many blocks. If this becomes a performance problem,
	// we can maintain a map of transactions indexed by txid.
	findTxInBlock := func(b []byte) json.RawMessage {
		block := parser.NewBlock()
		_, _ = block.ParseFromSlice(b)
		darksideSetBlockTxID(block)
		for _, tx := range block.Transactions() {
			if bytes.Equal(tx.GetDisplayHash(), txidBigEndian) {
				return marshalReply(tx, block.GetHeight())
			}
		}
		return nil
	}
	findTxInActiveBlocks := func(blocks []*activeBlock) json.RawMessage {
		for _, b := range blocks {
			ret := findTxInBlock(b.bytes)
			if ret != nil {
				return ret
			}
		}
		return nil
	}
	findTxInBlocks := func(blocks [][]byte) json.RawMessage {
		for _, b := range blocks {
			ret := findTxInBlock(b)
			if ret != nil {
				return ret
			}
		}
		return nil
	}
	// Search for the transaction (by txid) in the 3 places it could be.
	reply := findTxInActiveBlocks(state.activeBlocks)
	if reply != nil {
		return reply, nil
	}
	reply = findTxInBlocks(state.stagedBlocks)
	if reply != nil {
		return reply, nil
	}
	for _, stx := range state.stagedTransactions {
		tx := parser.NewTransaction()
		_, _ = tx.ParseFromSlice(stx.bytes)
		darksideSetTxID(tx)
		if bytes.Equal(tx.GetDisplayHash(), txidBigEndian) {
			return marshalReply(tx, 0), nil
		}
	}
	return nil, errors.New("-5: No information available about transaction")
}

// DarksideStageTransaction adds the given transaction to the staging area.
func stageTransaction(height int, txBytes []byte) error {
	if !state.resetted {
		return errors.New("please call Reset first")
	}
	Log.Info("DarksideStageTransaction(height=", height, ")")
	tx := parser.NewTransaction()
	rest, err := tx.ParseFromSlice(txBytes)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return errors.New("transaction serialization is too long")
	}
	state.stagedTransactions = append(state.stagedTransactions,
		stagedTx{
			height:         height,
			saplingOutputs: tx.SaplingOutputsCount(),
			orchardActions: tx.OrchardActionsCount(),
			bytes:          txBytes,
		})
	return nil
}

// DarksideStageTransaction adds the given transaction to the staging area.
func DarksideStageTransaction(height int, txBytes []byte) error {
	mutex.Lock()
	defer mutex.Unlock()
	return stageTransaction(height, txBytes)
}

// DarksideStageTransactionsURL reads a list of transactions (hex-encoded, one
// per line) from the given URL, and associates them with the given height.
func DarksideStageTransactionsURL(height int, url string) error {
	mutex.Lock()
	defer mutex.Unlock()
	if !state.resetted {
		return errors.New("please call Reset first")
	}
	Log.Info("DarksideStageTransactionsURL(height=", height, ", url=", url, ")")
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// some blocks are too large, especially when encoded in hex, for the
	// default buffer size, so set up a larger one; 8mb should be enough.
	scan := bufio.NewScanner(resp.Body)
	var scanbuf []byte
	scan.Buffer(scanbuf, 8*1000*1000)
	for scan.Scan() { // each line (transaction)
		transactionHex := scan.Text()
		if transactionHex == "404: Not Found" {
			// special case error (http resource not found, bad pathname)
			return errors.New(transactionHex)
		}
		transactionBytes, err := hex.DecodeString(transactionHex)
		if err != nil {
			return err
		}
		if err = stageTransaction(height, transactionBytes); err != nil {
			return err
		}
	}
	return scan.Err()
}

func DarksideAddAddressUtxo(arg ZcashdRpcReplyGetaddressutxos) error {
	mutex.Lock()
	state.getAddressUtxos = append(state.getAddressUtxos, arg)
	mutex.Unlock()
	return nil
}

func DarksideClearAddressUtxos() error {
	mutex.Lock()
	state.getAddressUtxos = nil
	mutex.Unlock()
	return nil
}

func DarksideClearAllTreeStates() error {
	mutex.Lock()
	state.stagedTreeStates = make(map[uint64]*DarksideTreeState)
	mutex.Unlock()
	return nil
}

func DarksideAddTreeState(arg DarksideTreeState) error {
	mutex.Lock()
	defer mutex.Unlock()
	if !state.resetted || state.stagedTreeStates == nil {
		return errors.New("please call Reset first")
	}

	state.stagedTreeStates[arg.Height] = &arg
	state.stagedTreeStatesByHash[arg.Hash] = &arg
	return nil
}

func DarksideRemoveTreeState(arg *walletrpc.BlockID) error {
	mutex.Lock()
	defer mutex.Unlock()
	if !state.resetted || state.stagedTreeStates == nil {
		return errors.New("please call Reset first")
	}
	if arg.Height > 0 {
		treestate := state.stagedTreeStates[arg.Height]
		delete(state.stagedTreeStatesByHash, treestate.Hash)
		delete(state.stagedTreeStates, treestate.Height)
	} else {
		h := hex.EncodeToString(arg.Hash)
		treestate := state.stagedTreeStatesByHash[h]
		delete(state.stagedTreeStatesByHash, treestate.Hash)
		delete(state.stagedTreeStates, treestate.Height)
	}
	return nil
}

func DarksideSetSubtreeRoots(arg_subtrees *walletrpc.DarksideSubtreeRoots) error {
	mutex.Lock()
	defer mutex.Unlock()
	if !state.resetted {
		return errors.New("please call Reset first")
	}
	state.subtrees[arg_subtrees.ShieldedProtocol] = darksideProtocolSubtreeRoots{
		startIndex: arg_subtrees.StartIndex,
		subtrees:   make([]darksideSubtree, len(arg_subtrees.SubtreeRoots)),
	}
	for i := 0; i < len(arg_subtrees.SubtreeRoots); i++ {
		s := &state.subtrees[arg_subtrees.ShieldedProtocol].subtrees[i]
		arg := arg_subtrees.SubtreeRoots[i]
		s.root = arg.RootHash
		s.endHeight = int(arg.CompletingBlockHeight)
		s.endHash = arg.CompletingBlockHash
	}
	return nil
}
