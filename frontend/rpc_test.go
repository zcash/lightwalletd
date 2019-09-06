package frontend

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// a well-formed raw transaction
const coinbaseTxHex = "0400008085202f89010000000000000000000000000000000000000" +
	"000000000000000000000000000ffffffff03580101ffffffff0200ca9a3b000000001976a9146b" +
	"9ae8c14e917966b0afdf422d32dbac40486d3988ac80b2e60e0000000017a9146708e6670db0b95" +
	"0dac68031025cc5b63213a4918700000000000000000000000000000000000000"

func TestSendTransaction(t *testing.T) {
	client, err := NewZRPCFromCreds("127.0.0.1:8232", "user", "password")
	if err != nil {
		t.Fatalf("Couldn't init JSON-RPC client: %v", err)
	}

	params := make([]json.RawMessage, 1)
	params[0] = json.RawMessage("\"" + coinbaseTxHex + "\"")
	_, err = client.RawRequest("sendrawtransaction", params)
	if err == nil {
		t.Fatal("somehow succeeded at sending a coinbase tx")
	}

	errParts := strings.SplitN(err.Error(), ":", 2)
	errCode, err := strconv.ParseInt(errParts[0], 10, 64)
	if err != nil {
		t.Errorf("couldn't parse error code: %v, zcashd running?", err)
	}
	errMsg := strings.TrimSpace(errParts[1])

	if errCode != -26 || errMsg != "16: coinbase" {
		t.Error("got the wrong errors")
	}
}
