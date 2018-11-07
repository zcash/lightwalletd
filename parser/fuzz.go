// +build gofuzz

package parser

func Fuzz(data []byte) int {
	block := NewBlock()
	_, err := block.ParseFromSlice(data)
	if err != nil {
		return 0
	}
	return 1
}
