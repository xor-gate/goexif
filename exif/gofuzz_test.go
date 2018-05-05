package exif

import (
	"bytes"
	"os"
	"testing"
)

var goFuzzPayloads = make(map[string]string)

// Populate payloads.
func populatePayloads() {

	goFuzzPayloads["3F"] = "II*\x00\b\x00\x00\x00\t\x000000000000" +
		"00000000000000000000" +
		"00000000000000000000" +
		"00000000000000000000" +
		"00000000000000000000" +
		"000000i\x87\x04\x00\x01\x00\x00\x00\xac\x00\x00\x0000" +
		"00000000000000000000" +
		"00000000000000000000" +
		"0000000000000000\x05\x00\x00\x00" +
		"\x00\xe00000"

	goFuzzPayloads["49"] = "MM\x00*\x00\x00\x00\b\x00\a0000000000" +
		"00000000000000000000" +
		"000000000000000000\x87i" +
		"\x00\x04\x00\x00\x00\x0000000000000000" +
		"00000000000000000000" +
		"00000000000000"

	goFuzzPayloads["A5"] = "II*\x00\b\x00\x00\x000000\x05\x00\x00\x00\x00\xa000" +
		"00"

}

// Test for Go-fuzz crashes.
func TestGoFuzzCrashes(t *testing.T) {
	for k, v := range goFuzzPayloads {
		t.Log("Testing gofuzz payload", k)
		v, err := Decode(bytes.NewReader([]byte(v)))
		t.Log("Results:", v, err)
	}
}

func TestMain(m *testing.M) {
	populatePayloads()
	ret := m.Run()
	os.Exit(ret)
}
