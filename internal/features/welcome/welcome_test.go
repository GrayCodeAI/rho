package welcome

import (
	"io"
	"testing"
)

func TestEmitIsBestEffort(t *testing.T) {
	if err := Emit(io.Discard); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}
}
