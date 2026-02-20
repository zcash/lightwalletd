// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Vendored from golang.org/x/crypto@v0.45.0/blake2b with
// personalization support added for ZIP 244 transaction ID
// computation. Only BLAKE2b-256 is retained; ASM, XOF,
// marshal, and larger sizes have been removed.

package blake2b

import (
	"encoding/binary"
	"hash"
)

const (
	// BlockSize is the block size of BLAKE2b in bytes.
	BlockSize = 128
	// Size256 is the hash size of BLAKE2b-256 in bytes.
	Size256 = 32
	// size is the internal full state size (64 bytes).
	size = 64
)

var iv = [8]uint64{
	0x6a09e667f3bcc908, 0xbb67ae8584caa73b, 0x3c6ef372fe94f82b, 0xa54ff53a5f1d36f1,
	0x510e527fade682d1, 0x9b05688c2b3e6c1f, 0x1f83d9abfb41bd6b, 0x5be0cd19137e2179,
}

type digest struct {
	h               [8]uint64
	c               [2]uint64
	sz              int
	block           [BlockSize]byte
	offset          int
	key             [BlockSize]byte
	keyLen          int
	personalization [16]byte
}

// New256Personalized returns a new hash.Hash computing BLAKE2b-256 with the
// given 16-byte personalization string (as required by ZIP 244).
func New256Personalized(personalization [16]byte) hash.Hash {
	d := &digest{
		sz:              Size256,
		personalization: personalization,
	}
	d.Reset()
	return d
}

// Sum256Personalized returns the BLAKE2b-256 checksum of data with the given
// 16-byte personalization string.
func Sum256Personalized(personalization [16]byte, data []byte) [Size256]byte {
	h := iv
	h[0] ^= uint64(Size256) | (1 << 16) | (1 << 24)
	h[6] ^= binary.LittleEndian.Uint64(personalization[:8])
	h[7] ^= binary.LittleEndian.Uint64(personalization[8:16])

	var c [2]uint64

	if length := len(data); length > BlockSize {
		n := length &^ (BlockSize - 1)
		if length == n {
			n -= BlockSize
		}
		hashBlocksGeneric(&h, &c, 0, data[:n])
		data = data[n:]
	}

	var block [BlockSize]byte
	offset := copy(block[:], data)
	remaining := uint64(BlockSize - offset)
	if c[0] < remaining {
		c[1]--
	}
	c[0] -= remaining

	hashBlocksGeneric(&h, &c, 0xFFFFFFFFFFFFFFFF, block[:])

	var sum [Size256]byte
	for i := 0; i < Size256/8; i++ {
		binary.LittleEndian.PutUint64(sum[8*i:], h[i])
	}
	return sum
}

func (d *digest) BlockSize() int { return BlockSize }
func (d *digest) Size() int      { return d.sz }

func (d *digest) Reset() {
	d.h = iv
	d.h[0] ^= uint64(d.sz) | (uint64(d.keyLen) << 8) | (1 << 16) | (1 << 24)
	d.h[6] ^= binary.LittleEndian.Uint64(d.personalization[:8])
	d.h[7] ^= binary.LittleEndian.Uint64(d.personalization[8:16])
	d.offset, d.c[0], d.c[1] = 0, 0, 0
	if d.keyLen > 0 {
		d.block = d.key
		d.offset = BlockSize
	}
}

func (d *digest) Write(p []byte) (n int, err error) {
	n = len(p)

	if d.offset > 0 {
		remaining := BlockSize - d.offset
		if n <= remaining {
			d.offset += copy(d.block[d.offset:], p)
			return
		}
		copy(d.block[d.offset:], p[:remaining])
		hashBlocksGeneric(&d.h, &d.c, 0, d.block[:])
		d.offset = 0
		p = p[remaining:]
	}

	if length := len(p); length > BlockSize {
		nn := length &^ (BlockSize - 1)
		if length == nn {
			nn -= BlockSize
		}
		hashBlocksGeneric(&d.h, &d.c, 0, p[:nn])
		p = p[nn:]
	}

	if len(p) > 0 {
		d.offset += copy(d.block[:], p)
	}

	return
}

func (d *digest) Sum(sum []byte) []byte {
	var hash [size]byte
	d.finalize(&hash)
	return append(sum, hash[:d.sz]...)
}

func (d *digest) finalize(hash *[size]byte) {
	var block [BlockSize]byte
	copy(block[:], d.block[:d.offset])
	remaining := uint64(BlockSize - d.offset)

	c := d.c
	if c[0] < remaining {
		c[1]--
	}
	c[0] -= remaining

	h := d.h
	hashBlocksGeneric(&h, &c, 0xFFFFFFFFFFFFFFFF, block[:])

	for i, v := range h {
		binary.LittleEndian.PutUint64(hash[8*i:], v)
	}
}
