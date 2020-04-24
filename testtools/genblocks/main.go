package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/zcash/lightwalletd/parser"
	"os"
	"path"
	"strconv"
	"strings"
)

type Options struct {
	startHeight int    `json:"start_height,omitempty"`
	blocksDir   string `json:"start_height,omitempty"`
}

func main() {
	opts := &Options{}
	flag.IntVar(&opts.startHeight, "start-height", 1000, "generated blocks start at this height")
	flag.StringVar(&opts.blocksDir, "blocks-dir", "./blocks", "directory containing <N>.txt for each block height <N>, with one hex-encoded transaction per line")
	flag.Parse()

	prevhash := make([]byte, 32)
	cur_height := opts.startHeight

	// Keep opening <cur_height>.txt and incrementing until the file doesn't exist.
	for {
		testBlocks, err := os.Open(path.Join(opts.blocksDir, strconv.Itoa(cur_height)+".txt"))
		if err != nil {
			break
		}
		scan := bufio.NewScanner(testBlocks)

		fake_coinbase := "0400008085202f890100000000000000000000000000000000000000000000000000" +
			"00000000000000ffffffff2a03d12c0c00043855975e464b8896790758f824ceac97836" +
			"22c17ed38f1669b8a45ce1da857dbbe7950e2ffffffff02a0ebce1d000000001976a914" +
			"7ed15946ec14ae0cd8fa8991eb6084452eb3f77c88ac405973070000000017a914e445cf" +
			"a944b6f2bdacefbda904a81d5fdd26d77f8700000000000000000000000000000000000000"

		// This coinbase transaction was pulled from block 797905, whose
		// little-endian encoding is 0xD12C0C00. Replace it with the block
		// number we want.
		fake_coinbase = strings.ReplaceAll(fake_coinbase, "d12c0c00",
			fmt.Sprintf("%02x", cur_height&0xFF)+
				fmt.Sprintf("%02x", (cur_height>>8)&0xFF)+
				fmt.Sprintf("%02x", (cur_height>>16)&0xFF)+
				fmt.Sprintf("%02x", (cur_height>>24)&0xFF))

		num_transactions := 1 // coinbase
		all_transactions_hex := ""
		for scan.Scan() { // each line (hex-encoded transaction)
			transaction := scan.Bytes()
			all_transactions_hex += string(transaction)
			num_transactions += 1
		}

		hash_of_txns_and_height := sha256.Sum256([]byte(all_transactions_hex + "#" + string(cur_height)))

		block_header := parser.BlockHeaderFromParts(
			4,
			prevhash,
			// These fields do not need to be valid for the lightwalletd/wallet stack to work.
			// The lightwalletd/wallet stack rely on the miners to validate these.

			// Make the block header depend on height + all transactions (in an incorrect way)
			hash_of_txns_and_height[:],

			make([]byte, 32),
			1,
			make([]byte, 4),
			make([]byte, 32),
			make([]byte, 1344))

		header_bytes, err := block_header.MarshalBinary()

		// After the header, there's a compactsize representation of the number of transactions.
		if num_transactions >= 253 {
			panic("Sorry, this tool doesn't support more than 253 transactions per block.")
		}
		compactsize := make([]byte, 1)
		compactsize[0] = byte(num_transactions)

		fmt.Println(hex.EncodeToString(header_bytes) + hex.EncodeToString(compactsize) + fake_coinbase + all_transactions_hex)

		cur_height += 1
		prevhash = block_header.GetEncodableHash()
	}
}
