package parser

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"log"
	"math/big"

	"github.com/pkg/errors"
	"github.com/zcash-hackworks/lightwalletd/parser/internal/bytestring"
)

const (
	serBlockHeaderMinusEquihashSize = 140  // size of a serialized block header minus the Equihash solution
	equihashSizeMainnet             = 1344 // size of a mainnet / testnet Equihash solution in bytes
)

// A block header as defined in version 2018.0-beta-29 of the Zcash Protocol Spec.
type rawBlockHeader struct {
	// The block version number indicates which set of block validation rules
	// to follow. The current and only defined block version number for Zcash
	// is 4.
	Version int32

	// A SHA-256d hash in internal byte order of the previous block's header. This
	// ensures no previous block can be changed without also changing this block's
	// header.
	HashPrevBlock []byte

	// A SHA-256d hash in internal byte order. The merkle root is derived from
	// the hashes of all transactions included in this block, ensuring that
	// none of those transactions can be modified without modifying the header.
	HashMerkleRoot []byte

	// [Pre-Sapling] A reserved field which should be ignored.
	// [Sapling onward] The root LEBS2OSP_256(rt) of the Sapling note
	// commitment tree corresponding to the final Sapling treestate of this
	// block.
	HashFinalSaplingRoot []byte

	// The block time is a Unix epoch time (UTC) when the miner started hashing
	// the header (according to the miner).
	Time uint32

	// An encoded version of the target threshold this block's header hash must
	// be less than or equal to, in the same nBits format used by Bitcoin.
	NBitsBytes []byte

	// An arbitrary field that miners can change to modify the header hash in
	// order to produce a hash less than or equal to the target threshold.
	Nonce []byte

	// The Equihash solution. In the wire format, this is a
	// CompactSize-prefixed value.
	Solution []byte
}

type blockHeader struct {
	*rawBlockHeader
	cachedHash      []byte
	targetThreshold *big.Int
}

func CompactLengthPrefixedLen(val []byte) int {
	length := len(val)
	if length < 253 {
		return 1 + length
	} else if length < 0xffff {
		return 1 + 2 + length
	} else if length < 0xffff {
		return 1 + 4 + length
	} else {
		return 1 + 8 + length
	}
}

func WriteCompactLengthPrefixed(buf *bytes.Buffer, val []byte) error {
	length := len(val)
	if length < 253 {
		binary.Write(buf, binary.LittleEndian, uint8(length))
		binary.Write(buf, binary.LittleEndian, val)
	} else if length < 0xffff {
		binary.Write(buf, binary.LittleEndian, byte(253))
		binary.Write(buf, binary.LittleEndian, uint16(length))
		binary.Write(buf, binary.LittleEndian, val)
	} else if length < 0xffff {
		binary.Write(buf, binary.LittleEndian, byte(254))
		binary.Write(buf, binary.LittleEndian, uint32(length))
		binary.Write(buf, binary.LittleEndian, val)
	} else {
		binary.Write(buf, binary.LittleEndian, byte(255))
		binary.Write(buf, binary.LittleEndian, uint64(length))
		binary.Write(buf, binary.LittleEndian, val)
	}
	return nil
}

func (hdr *rawBlockHeader) GetSize() int {
	return serBlockHeaderMinusEquihashSize + CompactLengthPrefixedLen(hdr.Solution)
}

func (hdr *rawBlockHeader) MarshalBinary() ([]byte, error) {
	headerSize := hdr.GetSize()
	backing := make([]byte, 0, headerSize)
	buf := bytes.NewBuffer(backing)
	binary.Write(buf, binary.LittleEndian, hdr.Version)
	binary.Write(buf, binary.LittleEndian, hdr.HashPrevBlock)
	binary.Write(buf, binary.LittleEndian, hdr.HashMerkleRoot)
	binary.Write(buf, binary.LittleEndian, hdr.HashFinalSaplingRoot)
	binary.Write(buf, binary.LittleEndian, hdr.Time)
	binary.Write(buf, binary.LittleEndian, hdr.NBitsBytes)
	binary.Write(buf, binary.LittleEndian, hdr.Nonce)
	WriteCompactLengthPrefixed(buf, hdr.Solution)
	return backing[:headerSize], nil
}

func NewBlockHeader() *blockHeader {
	return &blockHeader{
		rawBlockHeader: new(rawBlockHeader),
	}
}

// ParseFromSlice parses the block header struct from the provided byte slice,
// advancing over the bytes read. If successful it returns the rest of the
// slice, otherwise it returns the input slice unaltered along with an error.
func (hdr *blockHeader) ParseFromSlice(in []byte) (rest []byte, err error) {
	s := bytestring.String(in)

	// Primary parsing layer: sort the bytes into things

	if ok := s.ReadInt32(&hdr.Version); !ok {
		return in, errors.New("could not read header version")
	}

	if ok := s.ReadBytes(&hdr.HashPrevBlock, 32); !ok {
		return in, errors.New("could not read HashPrevBlock")
	}

	if ok := s.ReadBytes(&hdr.HashMerkleRoot, 32); !ok {
		return in, errors.New("could not read HashMerkleRoot")
	}

	if ok := s.ReadBytes(&hdr.HashFinalSaplingRoot, 32); !ok {
		return in, errors.New("could not read HashFinalSaplingRoot")
	}

	if ok := s.ReadUint32(&hdr.Time); !ok {
		return in, errors.New("could not read timestamp")
	}

	if ok := s.ReadBytes(&hdr.NBitsBytes, 4); !ok {
		return in, errors.New("could not read NBits bytes")
	}

	if ok := s.ReadBytes(&hdr.Nonce, 32); !ok {
		return in, errors.New("could not read Nonce bytes")
	}

	if ok := s.ReadCompactLengthPrefixed((*bytestring.String)(&hdr.Solution)); !ok {
		return in, errors.New("could not read CompactSize-prefixed Equihash solution")
	}

	// TODO: interpret the bytes
	//hdr.targetThreshold = parseNBits(hdr.NBitsBytes)

	return []byte(s), nil
}

func parseNBits(b []byte) *big.Int {
	byteLen := int(b[0])

	targetBytes := make([]byte, byteLen)
	copy(targetBytes, b[1:])

	// If high bit set, return a negative result. This is in the Bitcoin Core
	// test vectors even though Bitcoin itself will never produce or interpret
	// a difficulty lower than zero.
	if b[1]&0x80 != 0 {
		targetBytes[0] &= 0x7F
		target := new(big.Int).SetBytes(targetBytes)
		target.Neg(target)
		return target
	}

	return new(big.Int).SetBytes(targetBytes)
}

// GetDisplayHash returns the bytes of a block hash in big-endian order.
func (hdr *blockHeader) GetDisplayHash() []byte {
	if hdr.cachedHash != nil {
		return hdr.cachedHash
	}

	serializedHeader, err := hdr.MarshalBinary()
	if err != nil {
		log.Fatalf("error marshaling block header: %v", err)
		return nil
	}

	// SHA256d
	digest := sha256.Sum256(serializedHeader)
	digest = sha256.Sum256(digest[:])

	// Reverse byte order
	for i := 0; i < len(digest)/2; i++ {
		j := len(digest) - 1 - i
		digest[i], digest[j] = digest[j], digest[i]
	}

	hdr.cachedHash = digest[:]
	return hdr.cachedHash
}

// GetEncodableHash returns the bytes of a block hash in little-endian wire order.
func (hdr *blockHeader) GetEncodableHash() []byte {
	serializedHeader, err := hdr.MarshalBinary()

	if err != nil {
		log.Fatalf("error marshaling block header: %v", err)
		return nil
	}

	// SHA256d
	digest := sha256.Sum256(serializedHeader)
	digest = sha256.Sum256(digest[:])

	return digest[:]
}
