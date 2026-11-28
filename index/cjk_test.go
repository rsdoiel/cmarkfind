package index

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// Chinese and Japanese: one word per character, no dictionary. The sentences
// are from the public domain (Natsume Soseki's 吾輩は猫である, the opening of
// 三國演義) or are plain examples; a reader of these languages has not checked
// them.

func surfaces(s string) []string {
	var out []string
	for _, w := range Words(s) {
		out = append(out, w.Surface)
	}
	return out
}

func TestACJKCharacterIsAWordByItself(t *testing.T) {
	cases := map[string][]string{
		"吾輩は猫である":      {"吾", "輩", "は", "猫", "で", "あ", "る"},
		"日本語のテキスト":     {"日", "本", "語", "の", "テ", "キ", "ス", "ト"},
		"コンピューター":      {"コ", "ン", "ピ", "ュ", "ー", "タ", "ー"}, // the prolonged sound mark is a character
		"佐々木":          {"佐", "々", "木"},                     // the iteration mark too
		"iPhone手机":     {"iPhone", "手", "机"},                // a script change ends a word
		"第2章":          {"第", "2", "章"},
		"Go言語 v1":      {"Go", "言", "語", "v1"},
		"한국어를 좋아요":     {"한국어를", "좋아요"}, // Hangul has spaces: words as before
		"café 東京 Café": {"café", "東", "京", "Café"},
		"":             nil,
		"。、！":          nil, // CJK punctuation ends a word and is not one
	}
	for in, want := range cases {
		if got := surfaces(in); !slices.Equal(got, want) {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestACombiningMarkStaysWithItsCJKCharacter(t *testing.T) {
	// か + U+3099 (combining voiced mark) is one word whose term is the composed が
	ws := Words("がく")
	if len(ws) != 2 || ws[0].Surface != "が" || ws[0].Term != "が" || ws[1].Term != "く" {
		t.Errorf("words %+v", ws)
	}
	// half-width katakana with a voicing mark: ｶﾞ is one word, the term ガ
	ws = Words("ｶﾞｷ")
	if len(ws) != 2 || ws[0].Surface != "ｶﾞ" || ws[0].Term != "ガ" || ws[1].Term != "キ" {
		t.Errorf("half-width: %+v", ws)
	}
	// an ideographic variation selector stays with its character
	ws = Words("邊\U000E0100")
	if len(ws) != 1 {
		t.Errorf("variation selector: %+v", ws)
	}
}

func TestHalfWidthAndFullWidthKatakanaHaveOneTerm(t *testing.T) {
	a, b := Words("ｶﾀｶﾅ"), Words("カタカナ")
	if len(a) != 4 || len(b) != 4 {
		t.Fatalf("%d and %d words", len(a), len(b))
	}
	for i := range a {
		if a[i].Term != b[i].Term {
			t.Errorf("word %d: %q and %q", i, a[i].Term, b[i].Term)
		}
	}
}

func cjkDocs() []Doc {
	return []Doc{
		{Address: "https://example.org/neko.md", Title: "吾輩は猫である", Body: "吾輩は猫である。名前はまだ無い。どこで生れたかとんと見当がつかぬ。"},
		{Address: "https://example.org/sangoku.md", Title: "三國演義", Body: "天下大勢，分久必合，合久必分。"},
		{Address: "https://example.org/tokyo.md", Title: "Tokyo", Body: "東京都に行きます。日本語のテキストです。コンピューターを使う。"},
		{Address: "https://example.org/kyoto.md", Title: "Kyoto", Body: "京都の寺。iPhone手机 review. 한국어를 좋아요."},
	}
}

func TestACJKQueryFindsItsCharactersInARow(t *testing.T) {
	ix := open(t, build(t, cjkDocs(), 0))
	cases := []struct {
		query string
		want  []string
	}{
		{"猫", []string{"neko"}},
		{"名前はまだ", []string{"neko"}},
		{"猫名", nil},                        // both present, not in a row
		{"京都", []string{"kyoto", "tokyo"}}, // 京都 in 京都の寺 and inside 東京都 (substring search)
		{"東京", []string{"tokyo"}},
		{"東京都", []string{"tokyo"}},
		{"分久必合", []string{"sangoku"}},
		{"合久必分", []string{"sangoku"}},
		{"必合久", nil},
		{"テキスト", []string{"tokyo"}},
		{"コンピューター", []string{"tokyo"}},
		{"ｺﾝﾋﾟｭｰﾀｰ", []string{"tokyo"}}, // half-width katakana
		{"手机", []string{"kyoto"}},
		{"iPhone手机", []string{"kyoto"}},
		{"iphone", []string{"kyoto"}},
		{"한국어를", []string{"kyoto"}},
		{"한국어*", []string{"kyoto"}},
		{"title:猫", []string{"neko"}},
		{"title:演義", []string{"sangoku"}},
		{"body:三國", nil},
		{"猫 -名前", nil},
		{"猫 OR 京都", []string{"kyoto", "neko", "tokyo"}},
		{`"日本語"`, []string{"tokyo"}},
	}
	for _, c := range cases {
		hits, err := search(ix, c.query, 0)
		if err != nil {
			t.Fatalf("%q: %v", c.query, err)
		}
		var got []string
		for _, a := range addrs(hits) {
			got = append(got, strings.TrimSuffix(strings.TrimPrefix(a, "https://example.org/"), ".md"))
		}
		if !slices.Equal(sortedCopy(got), sortedCopy(c.want)) {
			t.Errorf("%q: found %v, want %v", c.query, got, c.want)
		}
	}
}

func TestADecomposedKanaFindsTheComposedOne(t *testing.T) {
	docs := []Doc{{Address: "https://example.org/1.md", Body: "がくせい"}, {Address: "https://example.org/2.md", Body: "がくせい"}}
	ix := open(t, build(t, docs, 0))
	for _, q := range []string{"がくせい", "がくせい", "学生"} {
		got := pageSet(t, ix, q)
		if q == "学生" {
			if len(got) != 0 {
				t.Errorf("%q: %v", q, got)
			}
			continue
		}
		if len(got) != 2 {
			t.Errorf("%q: %v", q, got)
		}
	}
}

func TestACJKExcerptHasNoSpacesBetweenCJKWords(t *testing.T) {
	ix := open(t, build(t, cjkDocs(), 0))
	n := mustParse(t, "猫")
	hits, err := ix.Search(n, 0)
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits %v, %v", hits, err)
	}
	e, err := ix.Excerpt(n, hits[0].Doc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Text, "吾輩は猫である") {
		t.Errorf("text %q has spaces inside a Japanese sentence", e.Text)
	}
	if !strings.Contains(e.HTML, "は<mark>猫</mark>で") {
		t.Errorf("html %q", e.HTML)
	}
	// a Latin word beside CJK keeps its space; two Latin words keep theirs
	n = mustParse(t, "review")
	hits, _ = ix.Search(n, 0)
	e, err = ix.Excerpt(n, hits[0].Doc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Text, "京都の寺 iPhone 手机 review") {
		t.Errorf("text %q", e.Text)
	}
}

func TestTheExcerptWindowIsThreeTimesAsLongOverCJKWords(t *testing.T) {
	cjk := strings.Repeat("あいうえお", 40)                       // 200 characters
	latin := strings.TrimSpace(strings.Repeat("word ", 200)) // 200 words
	docs := []Doc{
		{Address: "https://example.org/cjk.md", Body: cjk + "猫" + cjk},
		{Address: "https://example.org/latin.md", Body: latin + " cat " + latin},
	}
	ix := open(t, build(t, docs, 0))
	for q, want := range map[string]int{"猫": 42, "cat": 14} {
		n := mustParse(t, q)
		hits, err := ix.Search(n, 0)
		if err != nil || len(hits) != 1 {
			t.Fatalf("%s: %v %v", q, hits, err)
		}
		e, err := ix.Excerpt(n, hits[0].Doc, 14)
		if err != nil {
			t.Fatal(err)
		}
		if got := e.End - e.Start; got != want {
			t.Errorf("%s: window of %d words, want %d", q, got, want)
		}
	}
}

// cjkCorpus is deterministic text over a small set of characters in which
// の and 的 are on every page and make up a third of it.
func cjkCorpus(n int) []Doc {
	r := rand.New(rand.NewSource(5))
	common := []rune("の的に")
	rare := []rune("京都東猫寺山川花鳥風月雪")
	var docs []Doc
	for i := 0; i < n; i++ {
		var b strings.Builder
		for k := 0; k < 150; k++ {
			if r.Intn(3) == 0 {
				b.WriteRune(common[r.Intn(len(common))])
			} else {
				b.WriteRune(rare[r.Intn(len(rare))])
			}
			if r.Intn(25) == 0 {
				b.WriteString("。")
			}
		}
		d := Doc{Address: fmt.Sprintf("https://example.org/%03d.md", i), Body: b.String()}
		if i%3 == 0 {
			d.Title = string(rare[r.Intn(len(rare))]) + string(common[r.Intn(len(common))]) + string(rare[r.Intn(len(rare))])
		}
		docs = append(docs, d)
	}
	return docs
}

func TestCJKCharactersOnEveryPageAreDocumentOnlyAndPhrasesStayExact(t *testing.T) {
	docs := cjkCorpus(60)
	exact := open(t, build(t, docs, 0))
	dir, rep := buildBudget(t, docs, 900)
	var words []string
	for _, w := range rep.DocumentOnly {
		words = append(words, w.Word)
	}
	for _, w := range []string{"の", "的", "に"} {
		if !slices.Contains(words, w) {
			t.Errorf("%q is not document-only: %v", w, words)
		}
	}
	lean := open(t, dir)
	r := rand.New(rand.NewSource(9))
	var queries []string
	queries = append(queries, "の", "的", "京", "京の", "の京", "京の都", "-の京", "京 -の京", "title:の", "title:京の", "猫 OR の花")
	for i := 0; i < 80; i++ {
		rs := []rune(docs[r.Intn(len(docs))].Body)
		k := 2 + r.Intn(4)
		at := r.Intn(len(rs) - k)
		q := string(rs[at : at+k])
		if strings.ContainsRune(q, '。') {
			continue
		}
		queries = append(queries, q, "京 -"+q, "-"+q)
	}
	for _, q := range queries {
		if got, want := pageSet(t, lean, q), pageSet(t, exact, q); !slices.Equal(got, want) {
			t.Errorf("%s: found %d pages, the exact index %d", q, len(got), len(want))
		}
	}
}

// A fragment holds a CJK character and a space (four bytes), so a page of about
// two million characters does not fit the 8 MiB file limit: the build fails and
// names the page; a million characters gives the half-limit warning.
func TestAHugeCJKPageNamesItselfWhenItDoesNotFit(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 9 MB fragment")
	}
	dir := t.TempDir()
	_, err := BuildWithReport(dir, []Doc{{Address: "https://example.org/novel.md", Body: strings.Repeat("猫", 2_200_000)}}, BuildOptions{Generation: 1, Built: fixedTime})
	if err == nil || !strings.Contains(err.Error(), "https://example.org/novel.md") {
		t.Fatalf("error %v", err)
	}
	rep, err := BuildWithReport(t.TempDir(), []Doc{{Address: "https://example.org/long.md", Body: strings.Repeat("猫", 1_200_000)}}, BuildOptions{Generation: 1, Built: fixedTime})
	if err != nil || len(rep.Warnings) == 0 {
		t.Fatalf("1.2 million characters: %v, warnings %v", err, rep.Warnings)
	}
}
