package index

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// manifestOf reads manifest.json of dir.
func manifestOf(t *testing.T, dir string) manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func writeManifest(t *testing.T, dir string, m manifest) {
	t.Helper()
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// gz compresses raw.
func gz(raw []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(raw)
	zw.Close()
	return buf.Bytes()
}

// plant stores raw as a gzip file of kind, named by the digest of raw, and
// returns its manifest entry with the given size.
func plant(t *testing.T, dir, kind string, raw []byte, size int64) fileRef {
	t.Helper()
	ref := fileRef{Digest: sum(raw), Size: size}
	p := filepath.Join(dir, filepath.FromSlash(compressedName(kind, ref.Digest, "gzip")))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, gz(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	return ref
}

// swapFragment makes document doc's fragment the bytes raw: the fragment file,
// the document table that names it and the manifest that names the table are
// all rewritten, so every digest is true and only the content is wrong.
func swapFragment(t *testing.T, dir string, doc int, raw []byte) {
	t.Helper()
	m := manifestOf(t, dir)
	r := m.Documents.Ranges[0]
	tableRaw, err := readFile(os.DirFS(dir), "d", m.Compression, r.File)
	if err != nil {
		t.Fatal(err)
	}
	var dt documentTable
	if err := decMode.Unmarshal(tableRaw, &dt); err != nil {
		t.Fatal(err)
	}
	ref := plant(t, dir, "f", raw, int64(len(raw)))
	digest, _ := hexDecode(ref.Digest)
	dt.Rows[doc].Fragment = digest
	newTable, err := encodeFile(dir, "d", m.Compression, dt)
	if err != nil {
		t.Fatal(err)
	}
	m.Documents.Ranges[0].File = newTable
	writeManifest(t, dir, m)
}

func TestFileOverTheLimitIsRefused(t *testing.T) {
	dir := build(t, fixture(), 0)
	m := manifestOf(t, dir)
	// the manifest claims the biggest size allowed; the file holds one byte more
	big := bytes.Repeat([]byte{0}, maxFileSize+1)
	m.Chunks[0].File = plant(t, dir, "w", big, maxFileSize)
	writeManifest(t, dir, m)
	ix := open(t, dir)
	_, err := ix.chunk(0)
	if err == nil || !IsDataError(err) {
		t.Fatalf("oversize chunk: %v", err)
	}
	if err := Verify(dir); err == nil || !IsDataError(err) {
		t.Fatalf("Verify: %v", err)
	}
}

func TestManifestSizeOverTheLimitIsRefused(t *testing.T) {
	dir := build(t, fixture(), 0)
	m := manifestOf(t, dir)
	m.Chunks[0].File.Size = maxFileSize + 1
	writeManifest(t, dir, m)
	if _, err := Open(dir); err == nil || !IsDataError(err) {
		t.Fatalf("size over the limit: %v", err)
	}
}

func TestCompressionBombStopsAtTheLimit(t *testing.T) {
	dir := build(t, fixture(), 0)
	m := manifestOf(t, dir)
	// 64 MiB of zeros is about 64 KiB of gzip; the manifest tells the truth about the digest only
	bomb := bytes.Repeat([]byte{0}, 64<<20)
	m.Chunks[0].File = plant(t, dir, "w", bomb, 0)
	writeManifest(t, dir, m)
	ix := open(t, dir)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := ix.chunk(0)
	runtime.ReadMemStats(&after)
	if err == nil || !IsDataError(err) {
		t.Fatalf("bomb with size 0: %v", err)
	}
	// reading stops at the limit: far less than the 64 MiB the file expands to
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 40<<20 {
		t.Fatalf("a bomb made the reader allocate %d MiB", grew>>20)
	}
	// and with a small honest-looking size: more bytes than the manifest says
	m.Chunks[0].File = plant(t, dir, "w", bomb, 100)
	writeManifest(t, dir, m)
	ix = open(t, dir)
	if _, err := ix.chunk(0); err == nil || !IsDataError(err) {
		t.Fatalf("bomb with size 100: %v", err)
	}
}

func TestDuplicateCBORKeyIsRefused(t *testing.T) {
	dir := build(t, fixture(), 0)
	// {0: "a", 1: "t", 2: 1, 0: "y"}: key 0 twice
	raw := []byte{0xa4, 0x00, 0x61, 'a', 0x01, 0x61, 't', 0x02, 0x01, 0x00, 0x61, 'y'}
	// the same bytes with a distinct fourth key are a good fragment, so the
	// duplicate is the only fault
	good := append([]byte(nil), raw...)
	good[9] = 0x07
	swapFragment(t, dir, 0, good)
	if _, err := open(t, dir).Fragment(0); err != nil {
		t.Fatalf("control fragment: %v", err)
	}
	swapFragment(t, dir, 0, raw)
	ix := open(t, dir)
	if _, err := ix.Fragment(0); err == nil || !IsDataError(err) {
		t.Fatalf("duplicate key: %v", err)
	}
	if err := Verify(dir); err == nil || !IsDataError(err) {
		t.Fatalf("Verify: %v", err)
	}
}

func TestIndefiniteLengthCBORIsRefused(t *testing.T) {
	dir := build(t, fixture(), 0)
	// {_ 0: "a", 1: "t", 2: 1}: an indefinite-length map
	raw := []byte{0xbf, 0x00, 0x61, 'a', 0x01, 0x61, 't', 0x02, 0x01, 0xff}
	swapFragment(t, dir, 0, raw)
	ix := open(t, dir)
	if _, err := ix.Fragment(0); err == nil || !IsDataError(err) {
		t.Fatalf("indefinite length: %v", err)
	}
}

func TestSwapFragmentHelperKeepsAGoodFragmentGood(t *testing.T) {
	// the helper itself must not be what makes the tests above fail
	dir := build(t, fixture(), 0)
	ix := open(t, dir)
	f, _ := ix.Fragment(0)
	raw, err := encMode.Marshal(fragmentRec{Address: f.Address, Text: f.Text, WordCount: uint64(f.WordCount)})
	if err != nil {
		t.Fatal(err)
	}
	swapFragment(t, dir, 0, raw)
	if _, err := open(t, dir).Fragment(0); err != nil {
		t.Fatalf("a well-formed swapped fragment: %v", err)
	}
}

func TestManifestDuplicateJSONKeyIsRefused(t *testing.T) {
	for name, edit := range map[string]func(string) string{
		"top level": func(s string) string {
			return strings.Replace(s, `"generation": 1,`, `"generation": 1, "generation": 99,`, 1)
		},
		"nested": func(s string) string {
			return strings.Replace(s, `"documents": {`, `"documents": {"count": 7,`, 1)
		},
	} {
		dir := build(t, fixture(), 0)
		p := filepath.Join(dir, "manifest.json")
		data, _ := os.ReadFile(p)
		bad := edit(string(data))
		if bad == string(data) {
			t.Fatalf("%s: edit did not apply to %s", name, data)
		}
		os.WriteFile(p, []byte(bad), 0o644)
		if _, err := Open(dir); err == nil || !IsDataError(err) || !strings.Contains(err.Error(), "duplicate") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestManifestWithoutDuplicatesStillOpens(t *testing.T) {
	// the check must not trip on the same key in two different objects
	dir := build(t, fixture(), 0)
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
}
