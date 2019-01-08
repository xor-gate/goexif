package mknote

import (
	"testing"

	"github.com/xor-gate/goexif2/tiff"
)

var tiffMakerNoteValueTests = []struct {
	makerNoteValue []byte
	expectedOutput bool
	testContent    string
}{
	{[]byte("Nikon\000"), true, "Valid Nikon value"},
	{[]byte{0, 0, 0, 0, 0}, false, "Too small - 5"},
	{nil, false, "Nil"},
	{[]byte{0, 0, 0, 0, 0, 0}, false, "Right size"},
	{[]byte("Canon\000"), false, "Not equal to Nikon"},
	{[]byte("Nikon\00042"), true, "Contains Nikon"},
}

func TestNikonV3hasValidMakerNoteValue(t *testing.T) {
	var tiffMakerNote *tiff.Tag
	var isValid bool
	for _, tt := range tiffMakerNoteValueTests {
		tiffMakerNote = &tiff.Tag{
			Val: tt.makerNoteValue,
		}
		isValid = NikonV3.hasValidMakerNoteValue(tiffMakerNote)
		if isValid != tt.expectedOutput {
			t.Errorf("Expected the output '%t' has '%t' - Test type: %s", tt.expectedOutput, isValid, tt.testContent)
		}
	}
}
