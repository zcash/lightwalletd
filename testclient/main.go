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

// Package main implements a client for Greeter service.
package main

import (
	"context"
	"log"
	"sync"
	"time"

	pb "github.com/zcash/lightwalletd/walletrpc"
	"google.golang.org/grpc"
)

const (
	address = "localhost:9067"
)

func main() {
	// Set up a connection to the server.
	conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewCompactTxStreamerClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 1000*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	simultaneous := 2
	wg.Add(simultaneous)
	for i := 0; i < simultaneous; i++ {
		go func() {
			r, err := c.GetLightdInfo(ctx, &pb.Empty{})
			if err != nil {
				log.Fatalf("could not greet: %v", err)
			}
			log.Printf("LightdInfo: %v", r)
			wg.Done()
		}()
	}
	wg.Wait()
}
