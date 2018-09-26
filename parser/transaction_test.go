package parser

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/gtank/ctxd/parser/internal/bytestring"
)

// "Human-readable" version of joinSplit struct defined in transaction.go.
// Remember to update this if the format ever changes.
type joinSplitTestVector struct {
	vpubOld        uint64
	vpubNew        uint64
	anchor         string   // 32
	nullifiers     []string // 64 [N_old][32]byte
	commitments    []string // 64 [N_new][32]byte
	ephemeralKey   string   // 32
	randomSeed     string   // 32
	vmacs          []string // 64 [N_old][32]byte
	proofPHGR13    string   // 296
	proofGroth16   string   // 192
	encCiphertexts []string // 1202 [N_new][601]byte
}

type zip143test struct {
	header, nVersionGroupId, nLockTime, nExpiryHeight string
	vin, vout                                         [][]string
	vJoinSplits                                       []joinSplitTestVector
	joinSplitPubKey, joinSplitSig                     string
}

// https://github.com/zcash/zips/blob/master/zip-0143.rst
var zip143tests = []zip143test{
	{
		// Test vector 1
		header:          "03000080",
		nVersionGroupId: "7082c403",
		nLockTime:       "481cdd86",
		nExpiryHeight:   "b3cc4318",
		vin:             nil,
		vout: [][]string{
			{"8f739811893e0000", "095200ac6551ac636565"},
			{"b1a45a0805750200", "025151"},
		},
	},
	{
		// Test vector 2
		//raw: "we have some raw data for this tx, which this comment is too small to contain",
		header:          "03000080",
		nVersionGroupId: "7082c403",
		nLockTime:       "97b0e4e4",
		nExpiryHeight:   "c705fc05",
		vin: [][]string{
			{"4201cfb1cd8dbf69b8250c18ef41294ca97993db546c1fe01f7e9c8e36d6a5e2", "9d4e30a7", "03ac6a00", "98421c69"},
			{"378af1e40f64e125946f62c2fa7b2fecbcb64b6968912a6381ce3dc166d56a1d", "62f5a8d7", "056363635353", "e8c7203d"},
		},
		vout: [][]string{
			{"6af786387ae60100", "080063656a63ac5200"},
			{"23752997f4ff0400", "0751510053536565"},
		},
		vJoinSplits: []joinSplitTestVector{
			{
				vpubOld: uint64(0),
				vpubNew: uint64(0),
				anchor:  "76495c222f7fba1e31defa3d5a57efc2e1e9b01a035587d5fb1a38e01d94903d",
				nullifiers: []string{
					"3c3e0ad3360c1d3710acd20b183e31d49f25c9a138f49b1a537edcf04be34a98",
					"51a7af9db6990ed83dd64af3597c04323ea51b0052ad8084a8b9da948d320dad",
				},
				commitments: []string{
					"d64f5431e61ddf658d24ae67c22c8d1309131fc00fe7f235734276d38d47f1e1",
					"91e00c7a1d48af046827591e9733a97fa6b679f3dc601d008285edcbdae69ce8",
				},
				ephemeralKey: "fc1be4aac00ff2711ebd931de518856878f73476f21a482ec9378365c8f7393c",
				randomSeed:   "94e2885315eb4671098b79535e790fe53e29fef2b3766697ac32b4f473f468a0",
				vmacs: []string{
					"08e72389fc03880d780cb07fcfaabe3f1a84b27db59a4a153d882d2b21035965",
					"55ed9494c6ac893c49723833ec8926c1039586a7afcf4a0d9c731e985d99589c",
				},
				proofPHGR13: "03b838e8aaf745533ed9e8ae3a1cd074a51a20da8aba18d1dbebbc862ded42435e02476930d069896cff30eb414f727b89e001afa2fb8dc3436d75a4a6f26572504b0b2232ecb9f0c02411e52596bc5e90457e745939ffedbd12863ce71a02af117d417adb3d15cc54dcb1fce467500c6b8fb86b12b56da9c382857deecc40a98d5f2903395ee4762dd21afdbb5d47fa9a6dd984d567db2857b927b7fae2db587105415d0242789d38f50b8dbcc129cab3d17d19f3355bcf73cecb8cb8a5da01307152f13902a270572670dc82d39026c6cb4cd4b0f7f5aa2a4f5a5341ec5dd715406f2fdd2a02733f5f641c8c21862a1bafce2609d9eecfa158cfb5cd79f88008e315dc7d8388036c1782fd2795d18a763624c25fa959cc97489ce75745824b77868c53239cfbdf",
				encCiphertexts: []string{
					"73caec65604037314faaceb56218c6bd30f8374ac13386793f21a9fb80ad03bc0cda4a44946c00e1b1a1df0e5b87b5bece477a709649e950060591394812951e1fe3895b8cc3d14d2cf6556df6ed4b4ddd3d9a69f53357d7767f4f5ccbdbc596631277f8fecd08cb056b95e3025b9792fff7f244fc716269b926d62e9596fa825c6bf21aff9e68625a192440ea06828123d97884806f15fa08da52754a1095e3ff1abd5ce4fddfccfc3a6128aef784a64610a89d1a7099216d0814d3a2d452431c32d411ac1cce82ad0229407bbc48985675e3f874a4533f1d63a84dfa3e0f460fe2f57e34fbc75423c3737f5b2a0615f5722db041a3ef66fa483afd3c2e19e59444a64add6df1d963f5dd5b5010d3d025f0287c4cf19c75f33d51ddddba5d657b43ee8da645443814cc7329f3e9b4e54c236c29af3923101756d9fa4bd0f7d2ddaacb6b0f86a2658e0a07a05ac5b950051cd24c47a88d13d659ba2a46ca1830816d09cd7646f76f716abec5de07fe9b523410806ea6f288f8736c23357c85f45791e1708029d9824d90704607f387a03e49bf9836574431345a7877efaa8a08e73081ef8d62cb780ab6883a50a0d470190dfba10a857f82842d3825b3d6da0573d316eb160dc0b716c48fbd467f75b780149ae8808f4e68f50c0536acddf6f1aeab016b6bc1ec144b4e553acfd670f77e755fc88e0677e31ba459b44e307768958fe3789d41c2b1ff434cb30e15914f01bc6bc2307b488d2556d7b7380ea4ffd712f6b02fe806b94569cd4059f396bf29b99d0a40e5e1711ca944f72d436a102fca4b97693da0b086fe9d2e7162470d02e0f05d4bec9512bf",
					"b3f38327296efaa74328b118c27402c70c3a90b49ad4bbc68e37c0aa7d9b3fe17799d73b841e751713a02943905aae0803fd69442eb7681ec2a05600054e92eed555028f21b6a155268a2dd6640a69301a52a38d4d9f9f957ae35af7167118141ce4c9be0a6a492fe79f1581a155fa3a2b9dafd82e650b386ad3a08cb6b83131ac300b0846354a7eef9c410e4b62c47c5426907dfc6685c5c99b7141ac626ab4761fd3f41e728e1a28f89db89ffdeca364dd2f0f0739f0534556483199c71f189341ac9b78a269164206a0ea1ce73bfb2a942e7370b247c046f8e75ef8e3f8bd821cf577491864e20e6d08fd2e32b555c92c661f19588b72a89599710a88061253ca285b6304b37da2b5294f5cb354a894322848ccbdc7c2545b7da568afac87ffa005c312241c2d57f4b45d6419f0d2e2c5af33ae243785b325cdab95404fc7aed70525cddb41872cfcc214b13232edc78609753dbff930eb0dc156612b9cb434bc4b693392deb87c530435312edcedc6a961133338d786c4a3e103f60110a16b1337129704bf4754ff6ba9fbe65951e610620f71cda8fc877625f2c5bb04cbe1228b1e886f4050afd8fe94e97d2e9e85c6bb748c0042d3249abb1342bb0eebf62058bf3de080d94611a3750915b5dc6c0b3899d41222bace760ee9c8818ded599e34c56d7372af1eb86852f2a732104bdb750739de6c2c6e0f9eb7cb17f1942bfc9f4fd6ebb6b4cdd4da2bca26fac4578e9f543405acc7d86ff59158bd0cba3aef6f4a8472d144d99f8b8d1dedaa9077d4f01d4bb27bbe31d88fbefac3dcd4797563a26b1d61fcd9a464ab21ed550fe6fa09695ba0b2f10e",
				},
			},
			{
				vpubOld: uint64(0),
				vpubNew: uint64(0),
				anchor:  "ea6468cc6e20a66f826e3d14c5006f0563887f5e1289be1b2004caca8d3f34d6",
				nullifiers: []string{
					"e84bf59c1e04619a7c23a996941d889e4622a9b9b1d59d5e319094318cd405ba",
					"27b7e2c084762d31453ec4549a4d97729d033460fcf89d6494f2ffd789e98082",
				},
				commitments: []string{
					"ea5ce9534b3acd60fe49e37e4f666931677319ed89f85588741b3128901a93bd",
					"78e4be0225a9e2692c77c969ed0176bdf9555948cbd5a332d045de6ba6bf4490",
				},
				ephemeralKey: "adfe7444cd467a09075417fcc0062e49f008c51ad4227439c1b4476ccd8e9786",
				randomSeed:   "2dab7be1e8d399c05ef27c6e22ee273e15786e394c8f1be31682a30147963ac8",
				vmacs: []string{
					"da8d41d804258426a3f70289b8ad19d8de13be4eebe3bd4c8a6f55d6e0c373d4",
					"56851879f5fbc282db9e134806bff71e11bc33ab75dd6ca067fb73a043b646a7",
				},
				proofPHGR13: "0339cab4928386786d2f24141ee120fdc34d6764eafc66880ee0204f53cc1167ed02b43a52dea3ca7cff8ef35cd8e6d7c111a68ef44bcd0c1513ad47ca61c659cc5d0a5b440f6b9f59aff66879bb6688fd2859362b182f207b3175961f6411a493bffd048e7d0d87d82fe6f990a2b0a25f5aa0111a6e68f37bf6f3ac2d26b84686e569038d99c1383597fad81193c4c1b16e6a90e2d507cdfe6fbdaa86163e9cf5de310003ca7e8da047b090db9f37952fbfee76af61668190bd52ed490e677b515d0143840307219c7c0ee7fc7bfc79f325644e4df4c0d7db08e9f0bd024943c705abff899403a605cfbc7ed746a7d3f7c37d9e8bdc433b7d79e08a12f738a8f0dbddfef2f26502f3e47d1b0fd11e6a13311fb799c79c641d9da43b33e7ad012e28255398789262",
				encCiphertexts: []string{
					"275f1175be8462c01491c4d842406d0ec4282c9526174a09878fe8fdde33a29604e5e5e7b2a025d6650b97dbb52befb59b1d30a57433b0a351474444099daa371046613260cf3354cfcdada663ece824ffd7e44393886a86165ddddf2b4c41773554c86995269408b11e6737a4c447586f69173446d8e48bf84cbc000a807899973eb93c5e819aad669413f8387933ad1584aa35e43f4ecd1e2d0407c0b1b89920ffdfdb9bea51ac95b557af71b89f903f5d9848f14fcbeb1837570f544d6359eb23faf38a0822da36ce426c4a2fbeffeb0a8a2e297a9d19ba15024590e3329d9fa9261f9938a4032dd34606c9cf9f3dd33e576f05cd1dd6811c6298757d77d9e810abdb226afcaa4346a6560f8932b3181fd355d5d391976183f8d99388839632d6354f666d09d3e5629ea19737388613d38a34fd0f6e50ee5a0cc9677177f50028c141378187bd2819403fc534f80076e9380cb4964d3b6b45819d3b8e9caf54f051852d671bf8c1ffde2d1510756418cb4810936aa57e6965d6fb656a760b7f19adf96c173488552193b147ee58858033dac7cd0eb204c06490bbdedf5f7571acb2ebe76acef3f2a01ee987486dfe6c3f0a5e234c127258f97a28fb5d164a8176be946b8097d0e317287f33bf9c16f9a545409ce29b1f4273725fc0df02a04ebae178b3414fb0a82d50deb09fcf4e6ee9d180ff4f56ff3bc1d3601fc2dc90d814c3256f4967d3a8d64c83fea339c51f5a8e5801fbb97835581b602465dee04b5922c2761b54245bec0c9eef2db97d22b2b3556cc969fbb13d06509765a52b3fac54b93f421bf08e18d52ddd52cc1c8ca8adfaccab7e5cc2",
					"f4573fbbf8239bb0b8aedbf8dad16282da5c9125dba1c059d0df8abf621078f02d6c4bc86d40845ac1d59710c45f07d585eb48b32fc0167ba256e73ca3b9311c62d109497957d8dbe10aa3e866b40c0baa2bc492c19ad1e6372d9622bf163fbffeaeee796a3cd9b6fbbfa4d792f34d7fd6e763cd5859dd26833d21d9bc5452bd19515dff9f4995b35bc0c1f876e6ad11f2452dc9ae85aec01fc56f8cbfda75a7727b75ebbd6bbffb43b63a3b1b671e40feb0db002974a3c3b1a788567231bf6399ff89236981149d423802d2341a3bedb9ddcbac1fe7b6435e1479c72e7089d029e7fbbaf3cf37e9b9a6b776791e4c5e6fda57e8d5f14c8c35a2d270846b9dbe005cda16af4408f3ab06a916eeeb9c9594b70424a4c1d171295b6763b22f47f80b53ccbb904bd68fd65fbd3fbdea1035e98c21a7dbc91a9b5bc7690f05ec317c97f8764eb48e911d428ec8d861b708e8298acb62155145155ae95f0a1d1501034753146e22d05f586d7f6b4fe12dad9a17f5db70b1db96b8d9a83edadc966c8a5466b61fc998c31f1070d9a5c9a6d268d304fe6b8fd3b4010348611abdcbd49fe4f85b623c7828c71382e1034ea67bc8ae97404b0c50b2a04f559e49950afcb0ef462a2ae024b0f0224dfd73684b88c7fbe92d02b68f759c4752663cd7b97a14943649305521326bde085630864629291bae25ff8822a14c4b666a9259ad0dc42a8290ac7bc7f53a16f379f758e5de750f04fd7cad47701c8597f97888bea6fa0bf2999956fbfd0ee68ec36e4688809ae231eb8bc4369f5fe1573f57e099d9c09901bf39caac48dc11956a8ae905ead86954547c448ae43d31",
				},
			},
		},

		joinSplitPubKey: "5e669c4242da565938f417bf43ce7b2b30b1cd4018388e1a910f0fc41fb0877a",
		// This joinSplitSig is (intentionally) invalid random data.
		joinSplitSig: "5925e466819d375b0a912d4fe843b76ef6f223f0f7c894f38f7ab780dfd75f669c8c06cffa43eb47565a50e3b1fa45ad61ce9a1c4727b7aaa53562f523e73952",
	},
}

func TestSproutTransactionParser(t *testing.T) {
	// The raw data are stored in a separate file because they're large enough
	// to make the test table difficult to scroll through. They are in the same
	// order as the test table above. If you update the test table without
	// adding a line to the raw file, this test will panic due to index
	// misalignment.
	testData, err := os.Open("testdata/zip143_raw_tx")
	if err != nil {
		t.Fatal(err)
	}
	defer testData.Close()

	// Parse the raw transactions file
	rawTxData := make([][]byte, len(zip143tests))
	scan := bufio.NewScanner(testData)
	count := 0
	for scan.Scan() {
		dataLine := scan.Text()
		// Skip the comments
		if strings.HasPrefix(dataLine, "#") {
			continue
		}

		txData, err := hex.DecodeString(dataLine)
		if err != nil {
			t.Fatal(err)
		}

		rawTxData[count] = txData
		count++
	}

	for i, tt := range zip143tests {
		tx := newTransaction()

		rest, err := tx.ParseFromSlice(rawTxData[i])
		if err != nil {
			t.Errorf("Test %d: %v", i, err)
			continue
		}

		if len(rest) != 0 {
			t.Errorf("Test %d: did not consume entire buffer", i)
			continue
		}

		// Transaction metadata
		if ok := subTestCommonBlockMeta(&tt, tx, t, i); !ok {
			continue
		}

		// Transparent inputs and outputs
		if ok := subTestTransparentInputs(tt.vin, tx.transparentInputs, t, i); !ok {
			continue
		}

		if ok := subTestTransparentOutputs(tt.vout, tx.transparentOutputs, t, i); !ok {
			continue
		}

		// JoinSplits
		if ok := subTestJoinSplits(tt.vJoinSplits, tx.joinSplits, t, i); !ok {
			continue
		}

		testJSPubKey, _ := hex.DecodeString(tt.joinSplitPubKey)
		if !bytes.Equal(testJSPubKey, tx.joinSplitPubKey) {
			t.Errorf("Test %d: jsPubKey mismatch %x %x", i, testJSPubKey, tx.joinSplitPubKey)
			continue
		}

		testJSSig, _ := hex.DecodeString(tt.joinSplitSig)
		if !bytes.Equal(testJSSig, tx.joinSplitSig) {
			t.Errorf("Test %d: jsSig mismatch %x %x", i, testJSSig, tx.joinSplitSig)
			continue
		}
	}
}

func subTestCommonBlockMeta(tt *zip143test, tx *transaction, t *testing.T, caseNum int) bool {
	headerBytes, _ := hex.DecodeString(tt.header)
	header := binary.LittleEndian.Uint32(headerBytes)
	if (header >> 31) == 1 != tx.fOverwintered {
		t.Errorf("Test %d: unexpected fOverwintered", caseNum)
		return false
	}
	if (header & 0x7FFFFFFF) != tx.version {
		t.Errorf("Test %d: unexpected tx version", caseNum)
		return false
	}

	versionGroupBytes, _ := hex.DecodeString(tt.nVersionGroupId)
	versionGroup := binary.LittleEndian.Uint32(versionGroupBytes)
	if versionGroup != tx.nVersionGroupId {
		t.Errorf("Test %d: unexpected versionGroupId", caseNum)
		return false
	}

	lockTimeBytes, _ := hex.DecodeString(tt.nLockTime)
	lockTime := binary.LittleEndian.Uint32(lockTimeBytes)
	if lockTime != tx.nLockTime {
		t.Errorf("Test %d: unexpected nLockTime", caseNum)
		return false
	}

	expiryHeightBytes, _ := hex.DecodeString(tt.nExpiryHeight)
	expiryHeight := binary.LittleEndian.Uint32(expiryHeightBytes)
	if expiryHeight != tx.nExpiryHeight {
		t.Errorf("Test %d: unexpected nExpiryHeight", caseNum)
		return false
	}

	return true
}

func subTestJoinSplits(testJoinSplits []joinSplitTestVector, txJoinSplits []*joinSplit, t *testing.T, caseNum int) bool {
	if testJoinSplits == nil && txJoinSplits != nil {
		t.Errorf("Test %d: non-zero joinSplits when expected empty vector", caseNum)
		return false
	}
	if len(testJoinSplits) != len(txJoinSplits) {
		t.Errorf("Test %d: joinSplit vector lengths mismatch", caseNum)
		return false
	}

	success := true

JoinSplitLoop:
	for idx, test := range testJoinSplits {
		tx := txJoinSplits[idx]

		if test.vpubOld != tx.vpubOld {
			t.Errorf("Test %d js %d: vpubOld %d %d", caseNum, idx, test.vpubOld, tx.vpubOld)
			success = false
			continue
		}
		if test.vpubNew != tx.vpubNew {
			t.Errorf("Test %d js %d: vpubNew %d %d", caseNum, idx, test.vpubNew, tx.vpubNew)
			success = false
			continue
		}

		anchor, _ := hex.DecodeString(test.anchor)
		if !bytes.Equal(anchor, tx.anchor) {
			t.Errorf("Test %d js %d: anchor %x %x", caseNum, idx, anchor, tx.anchor)
			success = false
			continue
		}

		if len(test.nullifiers) != len(tx.nullifiers) {
			t.Errorf("Test %d js %d: nf len mismatch %d %d", caseNum, idx, len(test.nullifiers), len(tx.nullifiers))
			success = false
			continue
		}

		for j := 0; j < len(test.nullifiers); j++ {
			nf, _ := hex.DecodeString(test.nullifiers[j])
			if !bytes.Equal(nf, tx.nullifiers[j]) {
				t.Errorf("Test %d js %d: nf mismatch %x %x", caseNum, idx, nf, tx.nullifiers[j])
				success = false
				continue JoinSplitLoop
			}
		}

		if len(test.commitments) != len(tx.commitments) {
			t.Errorf("Test %d js %d: cm len mismatch %d %d", caseNum, idx, len(test.commitments), len(tx.commitments))
			success = false
			continue
		}

		for j := 0; j < len(test.commitments); j++ {
			cm, _ := hex.DecodeString(test.commitments[j])
			if !bytes.Equal(cm, tx.commitments[j]) {
				t.Errorf("Test %d js %d: commit mismatch %x %x", caseNum, idx, cm, tx.commitments[j])
				success = false
				continue JoinSplitLoop
			}
		}

		ephemeralKey, _ := hex.DecodeString(test.ephemeralKey)
		if !bytes.Equal(ephemeralKey, tx.ephemeralKey) {
			t.Errorf("Test %d js %d: ephemeralKey %x %x", caseNum, idx, ephemeralKey, tx.ephemeralKey)
			success = false
			continue
		}

		randomSeed, _ := hex.DecodeString(test.randomSeed)
		if !bytes.Equal(randomSeed, tx.randomSeed) {
			t.Errorf("Test %d js %d: randomSeed %x %x", caseNum, idx, randomSeed, tx.randomSeed)
			success = false
			continue
		}

		if len(test.vmacs) != len(tx.vmacs) {
			t.Errorf("Test %d js %d: mac len mismatch %d %d", caseNum, idx, len(test.vmacs), len(tx.vmacs))
			success = false
			continue
		}

		for j := 0; j < len(test.vmacs); j++ {
			mac, _ := hex.DecodeString(test.vmacs[j])
			if !bytes.Equal(mac, tx.vmacs[j]) {
				t.Errorf("Test %d js %d: mac mismatch %x %x", caseNum, idx, mac, tx.vmacs[j])
				success = false
				continue JoinSplitLoop
			}
		}

		// This should not be possible.
		if tx.proofPHGR13 != nil && tx.proofGroth16 != nil {
			t.Errorf("Test %d js %d: parsed tx had both PHGR and Groth proofs defined", caseNum, idx)
			success = false
			continue
		}

		if test.proofPHGR13 != "" {
			zkproof, _ := hex.DecodeString(test.proofPHGR13)
			if !bytes.Equal(zkproof, tx.proofPHGR13) {
				t.Errorf("Test %d js %d: zkproof %x %x", caseNum, idx, zkproof, tx.proofPHGR13)
				success = false
				continue
			}
		}

		if test.proofGroth16 != "" {
			zkproof, _ := hex.DecodeString(test.proofGroth16)
			if !bytes.Equal(zkproof, tx.proofGroth16) {
				t.Errorf("Test %d js %d: zkproof %x %x", caseNum, idx, zkproof, tx.proofGroth16)
				success = false
				continue
			}
		}

		if len(test.encCiphertexts) != len(tx.encCiphertexts) {
			t.Errorf("Test %d js %d: enc len mismatch %d %d", caseNum, idx, len(test.encCiphertexts), len(tx.encCiphertexts))
			success = false
			continue
		}

		for j := 0; j < len(test.encCiphertexts); j++ {
			ct, _ := hex.DecodeString(test.encCiphertexts[j])
			if !bytes.Equal(ct, tx.encCiphertexts[j]) {
				t.Errorf("Test %d js %d: ct mismatch %x %x", caseNum, idx, ct, tx.encCiphertexts[j])
				success = false
				continue JoinSplitLoop
			}
		}
	}

	return success
}

func subTestTransparentInputs(testInputs [][]string, txInputs []*txIn, t *testing.T, caseNum int) bool {
	if testInputs == nil && txInputs != nil {
		t.Errorf("Test %d: non-zero vin when expected zero", caseNum)
		return false
	}

	if len(testInputs) != len(txInputs) {
		t.Errorf("Test %d: vins have mismatched lengths", caseNum)
		return false
	}

	success := true
	le := binary.LittleEndian

	// 4201cfb1cd8dbf69b8250c18ef41294ca97993db546c1fe01f7e9c8e36d6a5e2 9d4e30a7 03ac6a00 98421c69
	for idx, ti := range testInputs {
		txInput := txInputs[idx]

		testPrevTxHash, _ := hex.DecodeString(ti[0])
		if eq := bytes.Equal(testPrevTxHash, txInput.PrevTxHash); !eq {
			t.Errorf("Test %d tin %d: prevhash mismatch %x %x", caseNum, idx, testPrevTxHash, txInput.PrevTxHash)
			success = false
			continue
		}

		testPrevTxOutIndexBytes, _ := hex.DecodeString(ti[1])
		testPrevTxOutIndex := le.Uint32(testPrevTxOutIndexBytes)
		if testPrevTxOutIndex != txInput.PrevTxOutIndex {
			t.Errorf("Test %d tin %d: prevout index mismatch %d %d", caseNum, idx, testPrevTxOutIndex, txInput.PrevTxOutIndex)
			success = false
			continue
		}

		// Decode scriptSig and correctly consume own CompactSize field
		testScriptSig, _ := hex.DecodeString(ti[2])
		ok := (*bytestring.String)(&testScriptSig).ReadCompactLengthPrefixed((*bytestring.String)(&testScriptSig))
		if !ok {
			t.Errorf("Test %d, tin %d: couldn't strip size from script", caseNum, idx)
			success = false
			continue
		}

		if eq := bytes.Equal(testScriptSig, txInput.ScriptSig); !eq {
			t.Errorf("Test %d tin %d: scriptsig mismatch %x %x", caseNum, idx, testScriptSig, txInput.ScriptSig)
			success = false
			continue
		}

		testSeqNumBytes, _ := hex.DecodeString(ti[3])
		testSeqNum := le.Uint32(testSeqNumBytes)
		if testSeqNum != txInput.SequenceNumber {
			t.Errorf("Test %d tin %d: seq mismatch %d %d", caseNum, idx, testSeqNum, txInput.SequenceNumber)
			success = false
			continue
		}
	}
	return success
}

func subTestTransparentOutputs(testOutputs [][]string, txOutputs []*txOut, t *testing.T, caseNum int) bool {
	if testOutputs == nil && txOutputs != nil {
		t.Errorf("Test %d: non-zero vout when expected zero", caseNum)
		return false
	}

	if len(testOutputs) != len(txOutputs) {
		t.Errorf("Test %d: vout have mismatched lengths", caseNum)
		return false
	}

	success := true
	le := binary.LittleEndian

	for idx, testOutput := range testOutputs {
		txOutput := txOutputs[idx]

		// Parse tx out value from test
		testValueBytes, _ := hex.DecodeString(testOutput[0])
		testValue := le.Uint64(testValueBytes)

		if testValue != txOutput.Value {
			t.Errorf("Test %d, tout %d: value mismatch %d %d", caseNum, idx, testValue, txOutput.Value)
			success = false
			continue
		}

		// Parse script from test
		testScript, _ := hex.DecodeString(testOutput[1])
		// Correctly consume own CompactSize field
		ok := (*bytestring.String)(&testScript).ReadCompactLengthPrefixed((*bytestring.String)(&testScript))
		if !ok {
			t.Errorf("Test %d, tout %d: couldn't strip size from script", caseNum, idx)
			success = false
			continue
		}

		if !bytes.Equal(testScript, txOutput.Script) {
			t.Errorf("Test %d, tout %d: script mismatch %x %x", caseNum, idx, testScript, txOutput.Script)
			success = false
			continue
		}
	}
	return success
}

type spendTestVector struct {
	cv           string // 32
	anchor       string // 32
	nullifier    string // 32
	rk           string // 32
	zkproof      string // 192
	spendAuthSig string // 64
}

type outputTestVector struct {
	cv            string // 32
	cmu           string // 32
	ephemeralKey  string // 32
	encCiphertext string // 580
	outCiphertext string // 80
	zkproof       string // 192
}

// https://github.com/zcash/zips/blob/master/zip-0243.rst
var zip243tests = []struct {
	header, nVersionGroupId, nLockTime, nExpiryHeight string
	vin, vout                                         [][]string
	valueBalance                                      string // encoded int64
	spends                                            []spendTestVector
	outputs                                           []outputTestVector
	vJoinSplits                                       []joinSplitTestVector
	joinSplitPubKey, joinSplitSig                     string
	bindingSig                                        string
}{
	{
		header:          "04000080",
		nVersionGroupId: "85202f89",
		vin:             nil,
		vout: [][]string{
			{"e7719811893e0000", "095200ac6551ac636565"},
			{"b2835a0805750200", "025151"},
		},
		nLockTime:     "481cdd86",
		nExpiryHeight: "b3cc4318",
		valueBalance:  "442117623ceb0500",
		spends: []spendTestVector{
			{
				cv:           "1b3d1a027c2c40590958b7eb13d742a997738c46a458965baf276ba92f272c72",
				anchor:       "1fe01f7e9c8e36d6a5e29d4e30a73594bf5098421c69378af1e40f64e125946f",
				nullifier:    "62c2fa7b2fecbcb64b6968912a6381ce3dc166d56a1d62f5a8d7551db5fd9313",
				rk:           "e8c7203d996af7d477083756d59af80d06a745f44ab023752cb5b406ed8985e1",
				zkproof:      "8130ab33362697b0e4e4c763ccb8f676495c222f7fba1e31defa3d5a57efc2e1e9b01a035587d5fb1a38e01d94903d3c3e0ad3360c1d3710acd20b183e31d49f25c9a138f49b1a537edcf04be34a9851a7af9db6990ed83dd64af3597c04323ea51b0052ad8084a8b9da948d320dadd64f5431e61ddf658d24ae67c22c8d1309131fc00fe7f235734276d38d47f1e191e00c7a1d48af046827591e9733a97fa6b679f3dc601d008285edcbdae69ce8fc1be4aac00ff2711ebd931de518856878",
				spendAuthSig: "f73476f21a482ec9378365c8f7393c94e2885315eb4671098b79535e790fe53e29fef2b3766697ac32b4f473f468a008e72389fc03880d780cb07fcfaabe3f1a",
			},
			{
				cv:           "15825b7acb4d6b57a61bc68f242b52e4fbf85cf1a09cc45b6d6bb3a391578f49",
				anchor:       "9486a7afd04a0d9c74c2995d96b4de37b36046a1ef6d190b916b1111c9288731",
				nullifier:    "1a20da8aba18d1dbebbc862ded42435e92476930d069896cff30eb414f727b89",
				rk:           "e001afa2fb8dc3436d75a4a6f26572504b192232ecb9f0c02411e52596bc5e90",
				zkproof:      "457e745939ffedbd12863ce71a02af117d417adb3d15cc54dcb1fce467500c6b8fb86b12b56da9c382857deecc40a98d5f2935395ee4762dd21afdbb5d47fa9a6dd984d567db2857b927b7fae2db587105415d4642789d38f50b8dbcc129cab3d17d19f3355bcf73cecb8cb8a5da01307152f13936a270572670dc82d39026c6cb4cd4b0f7f5aa2a4f5a5341ec5dd715406f2fdd2afa733f5f641c8c21862a1bafce2609d9eecfa158cfb5cd79f88008e315dc7d8388e76c1782fd2795d18a76",
				spendAuthSig: "3624c25fa959cc97489ce75745824b77868c53239cfbdf73caec65604037314faaceb56218c6bd30f8374ac13386793f21a9fb80ad03bc0cda4a44946c00e1b1",
			},
			{
				cv:           "02c78f11876b7065212183199fb5979ca77d2c24c738fe5145f02602053bb4c2",
				anchor:       "f6556df6ed4b4ddd3d9a69f53357d7767f4f5ccbdbc596631277f8fecd08cb05",
				nullifier:    "6b95e3025b9792fff7f244fc716269b926d62e9596fa825c6bf21aff9e68625a",
				rk:           "192440ea06828123d97884806f15fa08da52754a1095e3ff1abd5ce4fddfccfc",
				zkproof:      "3a6128aef784a64610a89d1a7099216d0814d3a2d452431c32d411ac1cce82ad0229407bbc48985675e3f874a4533f1d63a84dfa3e0f460fe2f57e34fbc75423c3737f5b2a0615f5722db041a3ef66fa483afd3c2e19e59444a64add6df1d963f5dd5b5010d3d025f0287c4cf19c75f33d51ddddba5d657b43ee8da645443814cc7329f3e9b4e54c236c29af3923101756d9fa4bd0f7d2ddaacb6b0f86a2658e0a07a05ac5b950051cd24c47a88d13d659ba2a46ca1830816d09cd7646f76f71",
				spendAuthSig: "6abec5de07fe9b523410806ea6f288f8736c23357c85f45791e1708029d9824d90704607f387a03e49bf9836574431345a7877efaa8a08e73081ef8d62cb780a",
			},
		},
		outputs: []outputTestVector{
			{
				cv:            "0fa3207ee2f0408097d563da1b2146819edf88d33e7753664fb71d122a6e3699",
				cmu:           "8fbd467f75b780149ae8808f4e68f50c0536acddf6f1aeab016b6bc1ec144b4e",
				ephemeralKey:  "59aeb77eef49d00e5fbb67101cdd41e6bc9cf641a52fca98be915f8440a410d7",
				encCiphertext: "4cb30e15914f01bc6bc2307b488d2556d7b7380ea4ffd712f6b02fe806b94569cd4059f396bf29b99d0a40e5e1711ca944f72d436a102fca4b97693da0b086fe9d2e7162470d02e0f05d4bec9512bfb3f38327296efaa74328b118c27402c70c3a90b49ad4bbc68e37c0aa7d9b3fe17799d73b841e751713a02943905aae0803fd69442eb7681ec2a05600054e92eed555028f21b6a155268a2dd6640a69301a52a38d4d9f9f957ae35af7167118141ce4c9be0a6a492fe79f1581a155fa3a2b9dafd82e650b386ad3a08cb6b83131ac300b0846354a7eef9c410e4b62c47c5426907dfc6685c5c99b7141ac626ab4761fd3f41e728e1a28f89db89ffdeca364dd2f0f0739f0534556483199c71f189341ac9b78a269164206a0ea1ce73bfb2a942e7370b247c046f8e75ef8e3f8bd821cf577491864e20e6d08fd2e32b555c92c661f19588b72a89599710a88061253ca285b6304b37da2b5294f5cb354a894322848ccbdc7c2545b7da568afac87ffa005c312241c2d57f4b45d6419f0d2e2c5af33ae243785b325cdab95404fc7aed70525cddb41872cfcc214b13232edc78609753dbff930eb0dc156612b9cb434bc4b693392deb87c530435312edcedc6a961133338d786c4a3e103f60110a16b1337129704bf4754ff6ba9fbe65951e610620f71cda8fc877625f2c5bb04cbe1228b1e886f4050afd8fe94e97d2e9e85c6bb748c0042d3249abb1342bb0eebf62058bf3de080d94611a3750915b5dc6c0b3899d41222bace760ee9c8818ded599e34c56d7372af1eb86852f2a732104bdb750739",
				outCiphertext: "de6c2c6e0f9eb7cb17f1942bfc9f4fd6ebb6b4cdd4da2bca26fac4578e9f543405acc7d86ff59158bd0cba3aef6f4a8472d144d99f8b8d1dedaa9077d4f01d4bb27bbe31d88fbefac3dcd4797563a26b",
				zkproof:       "1d61fcd9a464ab21ed550fe6fa09695ba0b2f10eea6468cc6e20a66f826e3d14c5006f0563887f5e1289be1b2004caca8d3f34d6e84bf59c1e04619a7c23a996941d889e4622a9b9b1d59d5e319094318cd405ba27b7e2c084762d31453ec4549a4d97729d033460fcf89d6494f2ffd789e98082ea5ce9534b3acd60fe49e37e4f666931677319ed89f85588741b3128901a93bd78e4be0225a9e2692c77c969ed0176bdf9555948cbd5a332d045de6ba6bf4490adfe7444cd467a09075417fc",
			},
		},
		vJoinSplits: []joinSplitTestVector{
			{
				vpubOld: uint64(0),
				vpubNew: uint64(0),
				anchor:  "062e49f008c51ad4227439c1b4476ccd8e97862dab7be1e8d399c05ef27c6e22",
				nullifiers: []string{
					"ee273e15786e394c8f1be31682a30147963ac8da8d41d804258426a3f70289b8",
					"ad19d8de13be4eebe3bd4c8a6f55d6e0c373d456851879f5fbc282db9e134806",
				},
				commitments: []string{
					"bff71e11bc33ab75dd6ca067fb73a043b646a7cf39cab4928386786d2f24141e",
					"e120fdc34d6764eafc66880ee0204f53cc1167ed20b43a52dea3ca7cff8ef35c",
				},
				ephemeralKey: "d8e6d7c111a68ef44bcd0c1513ad47ca61c659cc5d325b440f6b9f59aff66879",
				randomSeed:   "bb6688fd2859362b182f207b3175961f6411a493bffd048e7d0d87d82fe6f990",
				vmacs: []string{
					"a2b0a25f5aa0111a6e68f37bf6f3ac2d26b84686e569d58d99c1383597fad811",
					"93c4c1b16e6a90e2d507cdfe6fbdaa86163e9cf5de3100fbca7e8da047b090db",
				},
				proofGroth16: "9f37952fbfee76af61668190bd52ed490e677b515d014384af07219c7c0ee7fc7bfc79f325644e4df4c0d7db08e9f0bd024943c705abff8994bfa605cfbc7ed746a7d3f7c37d9e8bdc433b7d79e08a12f738a8f0dbddfef2f2657ef3e47d1b0fd11e6a13311fb799c79c641d9da43b33e7ad012e28255398789262275f1175be8462c01491c4d842406d0ec4282c9526174a09878fe8fdde33a29604e5e5e7b2a025d6650b97dbb52befb59b1d30a57433b0a351474444099daa371046613260",
				encCiphertexts: []string{
					"cf3354cfcdada663ece824ffd7e44393886a86165ddddf2b4c41773554c86995269408b11e6737a4c447586f69173446d8e48bf84cbc000a807899973eb93c5e819aad669413f8387933ad1584aa35e43f4ecd1e2d0407c0b1b89920ffdfdb9bea51ac95b557af71b89f903f5d9848f14fcbeb1837570f544d6359eb23faf38a0822da36ce426c4a2fbeffeb0a8a2e297a9d19ba15024590e3329d9fa9261f9938a4032dd34606c9cf9f3dd33e576f05cd1dd6811c6298757d77d9e810abdb226afcaa4346a6560f8932b3181fd355d5d391976183f8d99388839632d6354f666d09d3e5629ea19737388613d38a34fd0f6e50ee5a0cc9677177f50028c141378187bd2819403fc534f80076e9380cb4964d3b6b45819d3b8e9caf54f051852d671bf8c1ffde2d1510756418cb4810936aa57e6965d6fb656a760b7f19adf96c173488552193b147ee58858033dac7cd0eb204c06490bbdedf5f7571acb2ebe76acef3f2a01ee987486dfe6c3f0a5e234c127258f97a28fb5d164a8176be946b8097d0e317287f33bf9c16f9a545409ce29b1f4273725fc0df02a04ebae178b3414fb0a82d50deb09fcf4e6ee9d180ff4f56ff3bc1d3601fc2dc90d814c3256f4967d3a8d64c83fea339c51f5a8e5801fbb97835581b602465dee04b5922c2761b54245bec0c9eef2db97d22b2b3556cc969fbb13d06509765a52b3fac54b93f421bf08e18d52ddd52cc1c8ca8adfaccab7e5cc2f4573fbbf8239bb0b8aedbf8dad16282da5c9125dba1c059d0df8abf621078f02d6c4bc86d40845ac1d59710c45f07d585eb48b32fc0167ba256e73ca3b9311c62d1094979",
					"57d8dbe10aa3e866b40c0baa2bc492c19ad1e6372d9622bf163fbffeaeee796a3cd9b6fbbfa4d792f34d7fd6e763cd5859dd26833d21d9bc5452bd19515dff9f4995b35bc0c1f876e6ad11f2452dc9ae85aec01fc56f8cbfda75a7727b75ebbd6bbffb43b63a3b1b671e40feb0db002974a3c3b1a788567231bf6399ff89236981149d423802d2341a3bedb9ddcbac1fe7b6435e1479c72e7089d029e7fbbaf3cf37e9b9a6b776791e4c5e6fda57e8d5f14c8c35a2d270846b9dbe005cda16af4408f3ab06a916eeeb9c9594b70424a4c1d171295b6763b22f47f80b53ccbb904bd68fd65fbd3fbdea1035e98c21a7dbc91a9b5bc7690f05ec317c97f8764eb48e911d428ec8d861b708e8298acb62155145155ae95f0a1d1501034753146e22d05f586d7f6b4fe12dad9a17f5db70b1db96b8d9a83edadc966c8a5466b61fc998c31f1070d9a5c9a6d268d304fe6b8fd3b4010348611abdcbd49fe4f85b623c7828c71382e1034ea67bc8ae97404b0c50b2a04f559e49950afcb0ef462a2ae024b0f0224dfd73684b88c7fbe92d02b68f759c4752663cd7b97a14943649305521326bde085630864629291bae25ff8822a14c4b666a9259ad0dc42a8290ac7bc7f53a16f379f758e5de750f04fd7cad47701c8597f97888bea6fa0bf2999956fbfd0ee68ec36e4688809ae231eb8bc4369f5fe1573f57e099d9c09901bf39caac48dc11956a8ae905ead86954547c448ae43d315e669c4242da565938f417bf43ce7b2b30b1cd4018388e1a910f0fc41fb0877a5925e466819d375b0a912d4fe843b76ef6f223f0f7c894f38f7ab780dfd75f669c8c06cffa",
				},
			},
			{
				vpubOld: uint64(0),
				vpubNew: uint64(0),
				anchor:  "43eb47565a50e3b1fa45ad61ce9a1c4727b7aaa53562f523e73952bbf33d8a41",
				nullifiers: []string{
					"04078ade3eaaa49699a69fdf1c5ac7732146ee5e1d6b6ca9b9180f964cc9d087",
					"8ae1373524d7d510e58227df6de9d30d271867640177b0f1856e28d5c8afb095",
				},
				commitments: []string{
					"ef6184fed651589022eeaea4c0ce1fa6f085092b04979489172b3ef8194a798d",
					"f5724d6b05f1ae000013a08d612bca8a8c31443c10346dbf61de8475c0bbec51",
				},
				ephemeralKey: "04b47556af3d514458e2321d146071789d2335934a680614e83562f82dfd405b",
				randomSeed:   "54a45eb32c165448d4d5d61ca2859585369f53f1a137e9e82b67b8fdaf01bda5",
				vmacs: []string{
					"4a317311896ae10280a032440c420a421e944d1e952b70d5826cd3b08b7db963",
					"0fe4fd5f22125de840fcc40b98038af11d55be25432597b4b65b9ec1c7a8bbfd",
				},
				proofGroth16: "052cbf7e1c1785314934b262d5853754f1f17771cfb7503072655753fa3f54ecc587e9f83b581916092df26e63e18994cb0db91a0bbdc7b6119b32222adf5e61d8d8ae89dae4954b54813bb33f08d562ba513fee1b09c0fcd516055419474dd7fda038a89c84ea7b9468287f0eb0c10c4b132520194d3d8d5351fc10d09c15c8cc101aa1663bbf17b84111f38bb439f07353bdea3596d15e713e1e2e7d3f1c383135b47fa7f81f46df7a902a404699ec912f5656c35b85763e4de583aecaa1df",
				encCiphertexts: []string{
					"d5d2677d9c8ffee877f63f40a5ca0d67f6e554124739f805af876aeede53aa8b0f8e5604a73c30cbd09dad963d6f8a5dcc40def40797342113ba206fae8ebe4f3bc3caf69259e462eff9ba8b3f4bfaa1300c26925a8729cd32915bfc966086f0d5560bbe32a598c22adfb48cef72ba5d4287c0cefbacfd8ce195b4963c34a94bba7a175dae4bbe3ef4863d53708915090f47a068e227433f9e49d3aa09e356d8d66d0c0121e91a3c4aa3f27fa1b63396e2b41db908fdab8b18cc7304e94e970568f9421c0dbbbaf84598d972b0534f48a5e52670436aaa776ed2482ad703430201e53443c36dcfd34a0cb6637876105e79bf3bd58ec148cb64970e3223a91f71dfcfd5a04b667fbaf3d4b3b908b9828820dfecdd753750b5f9d2216e56c615272f854464c0ca4b1e85aedd038292c4e1a57744ebba010b9ebfbb011bd6f0b78805025d27f3c17746bae116c15d9f471f0f6288a150647b2afe9df7cccf01f5cde5f04680bbfed87f6cf429fb27ad6babe791766611cf5bc20e48bef119259b9b8a0e39c3df28cb9582ea338601cdc481b32fb82adeebb3dade25d1a3df20c37e712506b5d996c49a9f0f30ddcb91fe9004e1e83294a6c9203d94e8dc2cbb449de4155032604e47997016b304fd437d8235045e255a19b743a0a9f2e336b44cae307bb3987bd3e4e777fbb34c0ab8cc3d67466c0a88dd4ccad18a07a8d1068df5b629e5718d0f6df5c957cf71bb00a5178f175caca944e635c5159f738e2402a2d21aa081e10e456afb00b9f62416c8b9c0f7228f510729e0be3f305313d77f7379dc2af24869c6c74ee4471498861d192f0ff0f508285dab6b",
					"6a36ccf7d12256cc76b95503720ac672d08268d2cf7773b6ba2a5f664847bf707f2fc10c98f2f006ec22ccb5a8c8b7c40c7c2d49a6639b9f2ce33c25c04bc461e744dfa536b00d94baddf4f4d14044c695a33881477df124f0fcf206a9fb2e65e304cdbf0c4d2390170c130ab849c2f22b5cdd3921640c8cf1976ae1010b0dfd9cb2543e45f99749cc4d61f2e8aabfe98bd905fa39951b33ea769c45ab9531c57209862ad12fd76ba4807e65417b6cd12fa8ec916f013ebb8706a96effeda06c4be24b04846392e9d1e6930eae01fa21fbd700583fb598b92c8f4eb8a61aa6235db60f2841cf3a1c6ab54c67066844711d091eb931a1bd6281aedf2a0e8fab18817202a9be06402ed9cc720c16bfe881e4df4255e87afb7fc62f38116bbe03cd8a3cb11a27d568414782f47b1a44c97c680467694bc9709d32916c97e8006cbb07ba0e4180a3738038c374c4cce8f32959afb25f303f5815c4533124acf9d18940e77522ac5dc4b9570aae8f47b7f57fd8767bea1a24ae7bed65b4afdc8f1278c30e2db98fd172730ac6bbed4f1127cd32b04a95b205526cfcb4c4e1cc955175b3e8de1f5d81b18669692350aaa1a1d797617582e54d7a5b57a683b32fb1098062dad7b0c2eb518f6862e83db25e3dbaf7aed504de932acb99d735992ce62bae9ef893ff6acc0ffcf8e3483e146b9d49dd8c7835f43a37dca0787e3ec9f6605223d5ba7ae0ab9025b73bc03f7fac36c009a56d4d95d1e81d3b3ebca7e54cc1a12d127b57c8138976e791013b015f06a624f521b6ee04ec980893c7e5e01a336203594094f82833d7445fe2d09130f63511da54832de9136b39",
				},
			},
		},
		joinSplitPubKey: "f4599f5aa5dfbb45da60cdceab7eefde89be63f3f7c0d2324847cce1405def7c",
		joinSplitSig:    "469b0e272494e5df54f568656cb9c8818d92b72b8bc34db7bb3112487e746eefe4e808bbb287d99bf07d00dabededc5e5f074ffeae0cba7da3a516c173be1c51",
		bindingSig:      "3323e119f635e8209a074b216b7023fadc2d25949c90037e71e3e550726d210a2c688342e52440635e9cc14afe10102621a9c9accb782e9e4a5fa87f0a956f5b",
	},
}

func TestSaplingTransactionParser(t *testing.T) {
	// TODO: test Sapling transaction parsing; ZIP243
}
