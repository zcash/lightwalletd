// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package cmd

import (
	"testing"
)

func TestFileExists(t *testing.T) {
	if fileExists("nonexistent-file") {
		t.Fatal("fileExists unexpected success")
	}
	// If the path exists but is a directory, should return false
	if fileExists(".") {
		t.Fatal("fileExists unexpected success")
	}
	// The following file should exist, it's what's being tested
	if !fileExists("root.go") {
		t.Fatal("fileExists failed")
	}
}
