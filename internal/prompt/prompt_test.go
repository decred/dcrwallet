package prompt

import (
	"bufio"
	"strings"
	"testing"
)

func TestSeedReturnsScannerError(t *testing.T) {
	input := "yes\n" + strings.Repeat("x", 70*1024) + "\n"

	_, _, err := Seed(bufio.NewReader(strings.NewReader(input)))
	if err == nil {
		t.Fatal("Seed() error = nil, want scanner error")
	}
}
