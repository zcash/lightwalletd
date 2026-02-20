// Copyright (c) 2025 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .

package blake2b

import (
	"encoding/hex"
	"testing"
)

func TestSum256NoPersonalization(t *testing.T) {
	// BLAKE2b-256 of empty string (no personalization = all zeros).
	// Verified with Python: hashlib.blake2b(b"", digest_size=32).hexdigest()
	got := Sum256Personalized([16]byte{}, []byte{})
	want := "0e5751c026e543b2e8ab2eb06099daa1d1e5df47778f7787faab45cdf12fe3a8"
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("empty hash mismatch:\n  got  %s\n  want %s", hex.EncodeToString(got[:]), want)
	}
}

func TestSum256Abc(t *testing.T) {
	// BLAKE2b-256 of "abc".
	// Verified with Python: hashlib.blake2b(b"abc", digest_size=32).hexdigest()
	got := Sum256Personalized([16]byte{}, []byte("abc"))
	want := "bddd813c634239723171ef3fee98579b94964e3bb1cb3e427262c8c068d52319"
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("abc hash mismatch:\n  got  %s\n  want %s", hex.EncodeToString(got[:]), want)
	}
}

func TestNew256PersonalizedStreaming(t *testing.T) {
	// Verify streaming mode matches one-shot for "abc".
	h := New256Personalized([16]byte{})
	h.Write([]byte("a"))
	h.Write([]byte("bc"))
	got := h.Sum(nil)
	want := "bddd813c634239723171ef3fee98579b94964e3bb1cb3e427262c8c068d52319"
	if hex.EncodeToString(got) != want {
		t.Fatalf("streaming abc hash mismatch:\n  got  %s\n  want %s", hex.EncodeToString(got), want)
	}
}

func TestPersonalizedHash(t *testing.T) {
	// Verified with Python:
	//   hashlib.blake2b(b"", digest_size=32,
	//     person=b"ZcashTxHash_\xbb\x09\xb8\x76").hexdigest()
	person := [16]byte{
		'Z', 'c', 'a', 's', 'h', 'T', 'x', 'H',
		'a', 's', 'h', '_', 0xbb, 0x09, 0xb8, 0x76,
	}
	got := Sum256Personalized(person, []byte{})
	want := "da5ea35a7ceb9507dbdd7a1dd0c1c2bf5d61f12781704e5613c8c8d3226f6e26"
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("personalized empty hash mismatch:\n  got  %s\n  want %s", hex.EncodeToString(got[:]), want)
	}
}

func TestPersonalizedHashWithData(t *testing.T) {
	// Verified with Python:
	//   hashlib.blake2b(b"Zcash", digest_size=32,
	//     person=b"ZTxIdHeadersHash").hexdigest()
	person := [16]byte{
		'Z', 'T', 'x', 'I', 'd', 'H', 'e', 'a',
		'd', 'e', 'r', 's', 'H', 'a', 's', 'h',
	}
	got := Sum256Personalized(person, []byte("Zcash"))
	want := "1a9162a394083a3a8020bff265625864f9a4cb7f8a28038822f78c6a17bc4f45"
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("personalized data hash mismatch:\n  got  %s\n  want %s", hex.EncodeToString(got[:]), want)
	}
}

func TestReset(t *testing.T) {
	person := [16]byte{
		'Z', 'T', 'x', 'I', 'd', 'H', 'e', 'a',
		'd', 'e', 'r', 's', 'H', 'a', 's', 'h',
	}
	h := New256Personalized(person)
	h.Write([]byte("garbage"))
	h.Reset()
	h.Write([]byte("Zcash"))
	got := h.Sum(nil)
	want := "1a9162a394083a3a8020bff265625864f9a4cb7f8a28038822f78c6a17bc4f45"
	if hex.EncodeToString(got) != want {
		t.Fatalf("reset hash mismatch:\n  got  %s\n  want %s", hex.EncodeToString(got), want)
	}
}
