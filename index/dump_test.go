package index

import (
	"os"
	"testing"
)

// CMARKFIND_DUMP_DIR: write the fixture index there for the conformance script.
func TestDumpFixture(t *testing.T) {
	d := os.Getenv("CMARKFIND_DUMP_DIR")
	if d == "" {
		t.Skip("set CMARKFIND_DUMP_DIR to write the fixture index")
	}
	if err := Build(d, fixture(), BuildOptions{ChunkEntries: 5, Generation: 2, Built: fixedTime}); err != nil {
		t.Fatal(err)
	}
}
