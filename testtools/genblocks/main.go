// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
//
// This tool reads a set of files, each containing a list of transactions
// (one per line, can be empty), and writes to stdout a list of blocks,
// one per input file, in hex format (same as zcash-cli getblock 12345 0),
// each on a separate line. Each fake block contains a fake coinbase
// transaction and all of the transactions in the corresponding file.

// The default start height is 1000, so the program expects to find
// files blocks/1000.txt, blocks/1001.txt, ...
//
// Typical way to run this program to create 6 blocks, all empty except
// for the fifth, which contains one transaction:
//     $ mkdir blocks
//     $ touch blocks/{1000,1001,1002,1003,1004,1005}.txt
//     $ echo "0400008085202f8901950521a79e89ed418a4b506f42e9829739b1ca516d4c590bddb4465b4b347bb2000000006a4730440220142920f2a9240c5c64406668c9a16d223bd01db33a773beada7f9c9b930cf02b0220171cbee9232f9c5684eb918db70918e701b86813732871e1bec6fbfb38194f53012102975c020dd223263d2a9bfff2fa6004df4c07db9f01c531967546ef941e2fcfbffeffffff026daf9b00000000001976a91461af073e7679f06677c83aa48f205e4b98feb8d188ac61760356100000001976a91406f6b9a7e1525ee12fd77af9b94a54179785011b88ac4c880b007f880b000000000000000000000000" > blocks/1004.txt
//     $ go run testtools/genblocks/main.go >testdata/default-darkside-blocks
//
// Alternative way to create the empty files:
//     $ seq 1000 1005 | while read i; do touch blocks/$i.txt; done

package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/zcash/lightwalletd/parser"
)

type Options struct {
	startHeight int
	blocksDir   string
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
		fake_coinbase = strings.Replace(fake_coinbase, "d12c0c00",
			fmt.Sprintf("%02x", cur_height&0xFF)+
				fmt.Sprintf("%02x", (cur_height>>8)&0xFF)+
				fmt.Sprintf("%02x", (cur_height>>16)&0xFF)+
				fmt.Sprintf("%02x", (cur_height>>24)&0xFF), 1)

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

		cur_height++
		prevhash = block_header.GetEncodableHash()
	}
}
