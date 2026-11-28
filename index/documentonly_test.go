package index

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"slices"
	"strings"
	"testing"
)

// Tests for the document-only rule: an entry over the budget is written
// without positions; the manifest lists the word and requires the feature.

// stopDocs is a deterministic corpus in which a few words ("the", "of", "to",
// "and") are very common and the rest are spread thin.
func stopDocs(n int) []Doc {
	r := rand.New(rand.NewSource(7))
	common := []string{"the", "of", "to", "and"}
	rare := []string{}
	for i := 0; i < 40; i++ {
		rare = append(rare, fmt.Sprintf("w%02d", i))
	}
	words := func(k int) string {
		var out []string
		for i := 0; i < k; i++ {
			if r.Intn(10) < 4 {
				out = append(out, common[r.Intn(len(common))])
			} else {
				out = append(out, rare[r.Intn(len(rare))])
			}
		}
		return strings.Join(out, " ")
	}
	var docs []Doc
	for i := 0; i < n; i++ {
		d := Doc{Address: fmt.Sprintf("https://example.org/%03d.md", i), Body: words(120)}
		if i%3 == 0 {
			d.Title = words(4)
		}
		if i%4 == 0 {
			d.Description = words(8)
		}
		docs = append(docs, d)
	}
	return docs
}

const smallBudget = 600

func buildBudget(t *testing.T, docs []Doc, budget int) (string, *BuildReport) {
	t.Helper()
	dir := t.TempDir()
	rep, err := BuildWithReport(dir, docs, BuildOptions{Generation: 1, Built: fixedTime, EntryBytes: budget})
	if err != nil {
		t.Fatal(err)
	}
	return dir, rep
}

func TestAnEntryOverTheBudgetIsWrittenDocumentOnly(t *testing.T) {
	dir, rep := buildBudget(t, stopDocs(60), smallBudget)
	var words []string
	for _, w := range rep.DocumentOnly {
		words = append(words, w.Word)
		if w.Pages <= 0 || w.Positions < w.Pages {
			t.Errorf("report %+v", w)
		}
	}
	for _, w := range []string{"the", "of", "to", "and"} {
		if !slices.Contains(words, w) {
			t.Errorf("%q is not document-only: %v", w, words)
		}
	}
	for _, w := range words {
		if strings.HasPrefix(w, "w") {
			t.Errorf("a thin word %q is document-only", w)
		}
	}
	ix := open(t, dir)
	got := ix.DocumentOnly()
	if !slices.Equal(sortedCopy(got), sortedCopy(words)) {
		t.Errorf("index lists %v, report %v", got, words)
	}
	m := manifestOf(t, dir)
	if !slices.Contains(m.Required, FeatureDocumentOnly) {
		t.Errorf("required %v", m.Required)
	}
	if !slices.Equal(sortedCopy(m.DocumentOnly), sortedCopy(words)) {
		t.Errorf("manifest lists %v", m.DocumentOnly)
	}
	if err := Verify(dir); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestAnIndexWithNoDocumentOnlyWordRequiresNothing(t *testing.T) {
	dir := build(t, fixture(), 0)
	m := manifestOf(t, dir)
	if len(m.Required) != 0 || len(m.DocumentOnly) != 0 {
		t.Errorf("required %v, documentOnly %v", m.Required, m.DocumentOnly)
	}
	if got := open(t, dir).DocumentOnly(); len(got) != 0 {
		t.Errorf("DocumentOnly() = %v", got)
	}
}

func TestADocumentOnlyBuildIsDeterministic(t *testing.T) {
	a, _ := buildBudget(t, stopDocs(60), smallBudget)
	b, _ := buildBudget(t, stopDocs(60), smallBudget)
	if !sameTree(t, a, b) {
		t.Error("the same pages and budget gave different bytes")
	}
}

func pageSet(t *testing.T, ix *Index, q string) []string {
	t.Helper()
	hits, err := search(ix, q, 0)
	if err != nil {
		t.Fatalf("%q: %v", q, err)
	}
	return sortedCopy(addrs(hits))
}

func TestDocumentOnlySearchesFindTheSamePagesAsTheExactIndex(t *testing.T) {
	docs := stopDocs(80)
	exact := open(t, build(t, docs, 0))
	dir, rep := buildBudget(t, docs, smallBudget)
	if len(rep.DocumentOnly) < 3 {
		t.Fatalf("only %d words are document-only: the test would prove nothing", len(rep.DocumentOnly))
	}
	lean := open(t, dir)
	queries := []string{"the", "of", "th*", "t*", "w01", "w0*",
		"the of", "the OR w05", "the -of", "of -w03", "the w07", "(the OR to) AND w12", "-the",
		"title:the", "title:of", "body:to", "body:the", "description:and", "description:the", "title:w03",
		"title:the body:of", "title:t* w01"}
	for _, q := range queries {
		if got, want := pageSet(t, lean, q), pageSet(t, exact, q); !slices.Equal(got, want) {
			t.Errorf("%q: document-only index finds %d pages, exact index %d\n got  %v\n want %v", q, len(got), len(want), got, want)
		}
	}
}

// phraseSamples returns adjacent word runs of the bodies, two to four words
// long, so that some contain document-only words.
func phraseSamples(docs []Doc, n int) []string {
	r := rand.New(rand.NewSource(11))
	var out []string
	for len(out) < n {
		d := docs[r.Intn(len(docs))]
		ws := strings.Fields(d.Body)
		k := 2 + r.Intn(3)
		i := r.Intn(len(ws) - k)
		out = append(out, `"`+strings.Join(ws[i:i+k], " ")+`"`)
	}
	return out
}

func TestAPhraseThroughADocumentOnlyWordNeverMissesAPage(t *testing.T) {
	docs := stopDocs(80)
	exact := open(t, build(t, docs, 0))
	dir, _ := buildBudget(t, docs, smallBudget)
	lean := open(t, dir)
	extra := 0
	for _, q := range phraseSamples(docs, 150) {
		want, got := pageSet(t, exact, q), pageSet(t, lean, q)
		for _, a := range want {
			if !slices.Contains(got, a) {
				t.Errorf("%s: the document-only index misses %s", q, a)
			}
		}
		// a page that is only in the lean result holds every word of the phrase in one field
		words := strings.Fields(strings.Trim(q, `"`))
		for _, a := range got {
			if slices.Contains(want, a) {
				continue
			}
			extra++
			ok := false
			for _, f := range []string{"title", "description", "body"} {
				var clauses []string
				for _, w := range words {
					clauses = append(clauses, f+":"+w)
				}
				if slices.Contains(pageSet(t, exact, strings.Join(clauses, " ")), a) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s: %s is a false hit that does not even hold the words in one field", q, a)
			}
		}
	}
	t.Logf("%d false hits over 150 phrases", extra)
}

func TestAPhraseStillDemandsTheOffsetsOfTheWordsThatKeepTheirPositions(t *testing.T) {
	// "to" is the only document-only word; "or not" must stay adjacent
	filler := strings.Repeat("to to to to to to to to to to to to ", 40)
	docs := []Doc{
		{Address: "https://example.org/1.md", Body: "be or not to be " + filler},
		{Address: "https://example.org/2.md", Body: "or not be " + filler},
		{Address: "https://example.org/3.md", Body: "or be not " + filler},
		{Address: "https://example.org/4.md", Body: "be " + filler},
	}
	dir, rep := buildBudget(t, docs, 400)
	if len(rep.DocumentOnly) != 1 || rep.DocumentOnly[0].Word != "to" {
		t.Fatalf("document-only %+v", rep.DocumentOnly)
	}
	ix := open(t, dir)
	got := pageSet(t, ix, `"or not to be"`)
	if !slices.Contains(got, "https://example.org/1.md") {
		t.Errorf("the page with the phrase is missing: %v", got)
	}
	for _, bad := range []string{"https://example.org/3.md", "https://example.org/4.md"} {
		if slices.Contains(got, bad) {
			t.Errorf("%s matched although 'or not' or 'be' is not where the phrase puts it: %v", bad, got)
		}
	}
}

func TestAPhraseOfOnlyDocumentOnlyWordsNeedsThemInOneField(t *testing.T) {
	filler := strings.Repeat("the to ", 200)
	docs := []Doc{
		{Address: "https://example.org/body.md", Body: filler},
		{Address: "https://example.org/split.md", Title: "the", Body: strings.Repeat("to ", 400)},
	}
	dir, rep := buildBudget(t, docs, 150)
	if len(rep.DocumentOnly) < 2 {
		t.Fatalf("document-only %+v", rep.DocumentOnly)
	}
	got := pageSet(t, open(t, dir), `"the to"`)
	if !slices.Contains(got, "https://example.org/body.md") {
		t.Errorf("both words in the body: %v", got)
	}
	if slices.Contains(got, "https://example.org/split.md") {
		t.Errorf("'the' in the title and 'to' in the body is not one field: %v", got)
	}
}

func TestAFieldClauseOnADocumentOnlyWordMissesNoPage(t *testing.T) {
	docs := []Doc{
		{Address: "https://example.org/both.md", Title: "the end", Body: strings.Repeat("the quick ", 200)},
		{Address: "https://example.org/desc.md", Description: "the sea", Body: strings.Repeat("the slow ", 200)},
		{Address: "https://example.org/body.md", Body: strings.Repeat("the other ", 200)},
	}
	dir, rep := buildBudget(t, docs, 300)
	if len(rep.DocumentOnly) == 0 || rep.DocumentOnly[0].Word != "the" {
		t.Fatalf("document-only %+v", rep.DocumentOnly)
	}
	ix := open(t, dir)
	want := map[string][]string{
		"title:the":       {"both.md"},
		"description:the": {"desc.md"},
		"body:the":        {"both.md", "desc.md", "body.md"},
	}
	for q, w := range want {
		var full []string
		for _, f := range w {
			full = append(full, "https://example.org/"+f)
		}
		if got := pageSet(t, ix, q); !slices.Equal(got, sortedCopy(full)) {
			t.Errorf("%s: %v, want %v", q, got, full)
		}
	}
}

func TestADocumentOnlyWordIsMarkedFromThePageText(t *testing.T) {
	docs := []Doc{{Address: "https://example.org/1.md", Body: strings.Repeat("the ", 300) + "zebra stripes " + strings.Repeat("the ", 300)}}
	dir, rep := buildBudget(t, docs, 200)
	if len(rep.DocumentOnly) != 1 {
		t.Fatalf("document-only %+v", rep.DocumentOnly)
	}
	ix := open(t, dir)
	hits, err := search(ix, "the zebra", 0)
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits %v, %v", hits, err)
	}
	marks, err := ix.Marks(mustParse(t, "the zebra"), hits[0].Doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 601 { // 600 "the" and "zebra"
		t.Errorf("%d marks, want 601", len(marks))
	}
	if _, err := ix.Excerpt(mustParse(t, "the zebra"), hits[0].Doc, 10); err != nil {
		t.Errorf("excerpt: %v", err)
	}
}

func TestAReaderRefusesFeaturesItDoesNotKnow(t *testing.T) {
	dir, _ := buildBudget(t, stopDocs(60), smallBudget)
	m := manifestOf(t, dir)
	// the feature it knows, listed: fine (the build wrote it)
	if _, err := Open(dir); err != nil {
		t.Fatalf("known feature: %v", err)
	}
	// an unknown feature beside it: refused, naming the feature
	m2 := m
	m2.Required = append(slices.Clone(m.Required), "future-thing/9")
	writeManifest(t, dir, m2)
	if _, err := Open(dir); err == nil || !IsDataError(err) || !strings.Contains(err.Error(), "future-thing/9") {
		t.Errorf("unknown feature: %v", err)
	}
	// document-only words listed but the feature not required: wrong, refused
	m3 := m
	m3.Required = nil
	writeManifest(t, dir, m3)
	if _, err := Open(dir); err == nil || !IsDataError(err) {
		t.Errorf("words listed without the feature: %v", err)
	}
}

func TestATokenOverTheLimitIsNotIndexedButStillCounts(t *testing.T) {
	long := strings.Repeat("x", MaxTermBytes+1)
	ok := strings.Repeat("y", MaxTermBytes)
	docs := []Doc{
		{Address: "https://example.org/1.md", Body: "alpha " + long + " beta " + ok + " gamma"},
	}
	dir := t.TempDir()
	rep, err := BuildWithReport(dir, docs, BuildOptions{Generation: 1, Built: fixedTime})
	if err != nil {
		t.Fatal(err)
	}
	if rep.LongTokens != 1 {
		t.Errorf("LongTokens = %d", rep.LongTokens)
	}
	ix := open(t, dir)
	if got := pageSet(t, ix, long); len(got) != 0 {
		t.Errorf("a token over the limit was found: %v", got)
	}
	if got := pageSet(t, ix, ok); len(got) != 1 {
		t.Errorf("a token at the limit was not found: %v", got)
	}
	// positions did not move: "alpha beta" are not adjacent, "beta" is still the third word
	if got := pageSet(t, ix, `"alpha beta"`); len(got) != 0 {
		t.Errorf("the skipped token vanished from the positions: %v", got)
	}
	if got := pageSet(t, ix, `"beta `+ok+`"`); len(got) != 1 {
		t.Errorf("words after the skipped token moved: %v", got)
	}
	fr, err := ix.Fragment(0)
	if err != nil {
		t.Fatal(err)
	}
	if fr.WordCount != 5 || !strings.Contains(fr.Text, long) {
		t.Errorf("fragment: %d words, long token kept: %v", fr.WordCount, strings.Contains(fr.Text, long))
	}
}

// limitDocs has one word, "the", whose entry is the biggest file a build
// writes: ten pages of 500 of it (a fragment is 2000 bytes, the entry over 5000).
func limitDocs() []Doc {
	var docs []Doc
	for i := 0; i < 10; i++ {
		docs = append(docs, Doc{Address: fmt.Sprintf("https://example.org/%d.md", i), Body: strings.Repeat("the ", 500) + fmt.Sprintf("w%d", i)})
	}
	return docs
}

func TestAFileOverTheLimitNamesTheWordAndWhatItIs(t *testing.T) {
	docs := limitDocs()
	// one word per chunk so that the file that is too big is one word's
	probe := t.TempDir()
	if err := Build(probe, docs, BuildOptions{ChunkEntries: 1, Generation: 1, Built: fixedTime, EntryBytes: 1 << 30}); err != nil {
		t.Fatal(err)
	}
	biggest := int64(0)
	for _, c := range manifestOf(t, probe).Chunks {
		biggest = max(biggest, c.File.Size)
	}
	dir := t.TempDir()
	_, err := BuildWithReport(dir, docs, BuildOptions{ChunkEntries: 1, Generation: 1, Built: fixedTime, EntryBytes: 1 << 30, FileLimit: int(biggest) - 1})
	if err == nil {
		t.Fatal("a file over the limit was written")
	}
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("error %v is not ErrTooLarge", err)
	}
	if !strings.Contains(err.Error(), `"the"`) || !strings.Contains(err.Error(), "10 pages") {
		t.Errorf("the error does not name the word and its page count: %v", err)
	}
	if _, serr := os.Stat(dir + "/manifest.json"); serr == nil {
		t.Error("a manifest was left")
	}
}

func TestAFileOverHalfTheLimitIsWarnedAbout(t *testing.T) {
	docs := limitDocs()
	probe := t.TempDir()
	if err := Build(probe, docs, BuildOptions{ChunkEntries: 1, Generation: 1, Built: fixedTime, EntryBytes: 1 << 30}); err != nil {
		t.Fatal(err)
	}
	biggest := int64(0)
	for _, c := range manifestOf(t, probe).Chunks {
		biggest = max(biggest, c.File.Size)
	}
	opt := BuildOptions{ChunkEntries: 1, Generation: 1, Built: fixedTime, EntryBytes: 1 << 30}
	opt.FileLimit = int(biggest) * 3 / 2 // the biggest file is over half, under the limit
	rep, err := BuildWithReport(t.TempDir(), docs, opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Warnings) == 0 {
		t.Error("no warning for a file over half the limit")
	}
	opt.FileLimit = int(biggest) * 4 // under half
	rep, err = BuildWithReport(t.TempDir(), docs, opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("warnings with room to spare: %v", rep.Warnings)
	}
}

// A phrase through a document-only word gives a superset of the pages that
// hold it, so NOT must not take its complement: the pages that have the words
// of the phrase but not the phrase are answers to `web -"to the"`. Red until
// the verification record (cmarkfind DR-0012) is built.
func TestNotOfAPhraseThroughADocumentOnlyWordMissesNoPage(t *testing.T) {
	filler := strings.Repeat("the ", 300) + strings.Repeat("to ", 300)
	docs := []Doc{
		{Address: "https://example.org/phrase.md", Body: "web to the web " + filler},
		{Address: "https://example.org/apart.md", Body: "web " + filler + " only"},
	}
	exact := open(t, build(t, docs, 0))
	dir, rep := buildBudget(t, docs, 200)
	if len(rep.DocumentOnly) < 2 {
		t.Fatalf("document-only %+v", rep.DocumentOnly)
	}
	lean := open(t, dir)
	for _, q := range []string{`web -"to the"`, `-"to the"`, `web NOT "to the"`, `(web -"to the") OR only`} {
		if got, want := pageSet(t, lean, q), pageSet(t, exact, q); !slices.Equal(got, want) {
			t.Errorf("%s: found %v, the exact index finds %v", q, got, want)
		}
	}
}
