/*
 *
 * Copyright 2015 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */
// Package main implements a gRPC test client for lightwalletd.
// This file adapted from:
//   https://github.com/grpc/grpc-go/blob/master/examples/helloworld/greeter_client/main.go
// For now at least, all it does is generate a load for performance and stress testing.
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"strconv"
	"sync"
	"time"

	pb "github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/grpc"
)

const (
	address = "localhost:9067"
)

type Options struct {
	concurrency int    `json:"concurrency"`
	iterations  int    `json:"iterations"`
	op          string `json:"op"`
	verbose     *bool  `json:"v"`
}

func main() {
	opts := &Options{}
	flag.IntVar(&opts.concurrency, "concurrency", 1, "number of threads")
	flag.IntVar(&opts.iterations, "iterations", 1, "number of iterations")
	flag.StringVar(&opts.op, "op", "ping", "operation(ping|getlightdinfo|getblock|getblockrange)")
	opts.verbose = flag.Bool("v", false, "verbose (print operation results)")
	flag.Parse()

	// Remaining args are all integers (at least for now)
	args := make([]int64, flag.NArg())
	for i := 0; i < flag.NArg(); i++ {
		var err error
		if args[i], err = strconv.ParseInt(flag.Arg(i), 10, 64); err != nil {
			log.Fatalf("argument %v is not an int64: %v", flag.Arg(i), err)
		}
	}

	// Set up a connection to the server.
	conn, err := grpc.Dial(address, grpc.WithInsecure(),
		grpc.WithConnectParams(grpc.ConnectParams{MinConnectTimeout: 30 * time.Second}))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewCompactTxStreamerClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 100000*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(opts.concurrency)
	for i := 0; i < opts.concurrency; i++ {
		go func(i int) {
			for j := 0; j < opts.iterations; j++ {
				switch opts.op {
				case "ping":
					var a pb.Duration
					a.IntervalUs = 8 * 1000 * 1000 // default 8 seconds
					if len(args) > 0 {
						a.IntervalUs = args[0]
					}
					r, err := c.Ping(ctx, &a)
					if err != nil {
						log.Fatalf("Ping failed: %v", err)
					}
					if *opts.verbose {
						log.Println("thr:", i, "entry:", r.Entry, "exit:", r.Exit)
					}
				case "getlightdinfo":
					r, err := c.GetLightdInfo(ctx, &pb.Empty{})
					if err != nil {
						log.Fatalf("GetLightwalletdInfo failed: %v", err)
					}
					if *opts.verbose {
						log.Println("thr:", i, r)
					}
				case "getblock":
					blockid := &pb.BlockID{Height: 748400} // default (arbitrary)
					if len(args) > 0 {
						blockid.Height = uint64(args[0])
					}
					r, err := c.GetBlock(ctx, blockid)
					if err != nil {
						log.Fatalf("GetLightwalletdInfo failed: %v", err)
					}
					// Height is enough to see if it's working
					if *opts.verbose {
						log.Println("thr:", i, r.Height)
					}
				case "getblockrange":
					blockrange := &pb.BlockRange{ // defaults (arbitrary)
						Start: &pb.BlockID{Height: 738100},
						End:   &pb.BlockID{Height: 738199},
					}
					if len(args) > 0 {
						blockrange.Start.Height = uint64(args[0])
						blockrange.End.Height = uint64(args[1])
					}
					stream, err := c.GetBlockRange(ctx, blockrange)
					if err != nil {
						log.Fatalf("GetLightwalletdInfo failed: %v", err)
					}
					for {
						// each call to Recv returns a compact block
						r, err := stream.Recv()
						if err == io.EOF {
							break
						}
						if err != nil {
							log.Fatal(err)
						}
						// Height is enough to see if it's working
						if *opts.verbose {
							log.Println("thr:", i, r.Height)
						}
					}
				default:
					log.Fatalf("unknown op %s", opts.op)
				}
			}
			wg.Done()
		}(i)
	}
	wg.Wait()
}
