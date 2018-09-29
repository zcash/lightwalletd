package main

import (
	"encoding/binary"
	"log"

	"github.com/gtank/ctxd/parser"
	zmq "github.com/pebbe/zmq4"
	"github.com/pkg/errors"
)

const (
	PORT = 28332
)

func main() {
	ctx, err := zmq.NewContext()
	if err != nil {
		log.Fatal(err)
	}
	defer ctx.Term()

	// WARNING: The Socket is not thread safe. This means that you cannot
	// access the same Socket from different goroutines without using something
	// like a mutex.
	sock, err := ctx.NewSocket(zmq.SUB)
	if err != nil {
		log.Fatal(errors.Wrap(err, "creating socket"))
	}
	err = sock.SetSubscribe("rawblock")
	if err != nil {
		log.Fatal(errors.Wrap(err, "subscribing"))
	}
	err = sock.Connect("tcp://127.0.0.1:28332")
	if err != nil {
		log.Fatal(errors.Wrap(err, "connecting"))
	}
	defer sock.Close()

	for {
		msg, err := sock.RecvMessageBytes(0)
		if err != nil {
			log.Println(errors.Wrap(err, "on message receipt"))
			continue
		}

		if len(msg) < 3 {
			log.Printf("got unknown msg: %v", msg)
			continue
		}

		topic, body := msg[0], msg[1]

		var sequence int
		if len(msg[2]) == 4 {
			sequence = int(binary.LittleEndian.Uint32(msg[len(msg)-1]))
		}

		switch string(topic) {
		case "rawblock":
			log.Printf("got block (%d): %x\n", sequence, body[:80])
			go handleBlock(sequence, body)
		default:
			log.Printf("unexpected topic: %s (%d)", topic, sequence)
		}
	}
}

func handleBlock(sequence int, blockData []byte) {
	block := parser.NewBlock()
	rest, err := block.ParseFromSlice(blockData)
	if err != nil {
		log.Println("Error parsing block (%d): %v", err)
		return
	}
	if len(rest) != 0 {
		log.Println("Received overlong message:\n%x", rest)
		return
	}

	log.Printf("Received a version %d block with %d transactions.", block.GetVersion(), block.GetTxCount())
}
