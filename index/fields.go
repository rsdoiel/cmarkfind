package index

// Filter files: what the index keeps of a page's front matter. A field with a
// facet, fielded or date role has one filter file, listed in the manifest's
// "filters" with its roles. The file holds, by role, the whole values
// (folded) to document numbers, the words of the values to document numbers, and
// the day number of each document. A clause field:word is answered from the
// words; the rest is for facets and date ranges.

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// FieldSpec says what the build keeps of one front matter field.
type FieldSpec struct {
	Name                 string // lower case: letters, digits, - and _
	Facet, Fielded, Date bool
}

// FilterInfo describes a filter file an index holds.
type FilterInfo struct {
	Name                 string
	Facet, Fielded, Date bool
}

var filterNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// maxFilters is the most filter files a manifest may name.
const maxFilters = 256

// Roles of a filter, as the manifest spells them.
const (
	roleFacet   = "facet"
	roleFielded = "fielded"
	roleDate    = "date"
)

// filterRef is a manifest entry: the file and the roles its field has.
type filterRef struct {
	File  fileRef  `json:"file"`
	Roles []string `json:"roles"`
}

func (r filterRef) has(role string) bool {
	for _, x := range r.Roles {
		if x == role {
			return true
		}
	}
	return false
}

// filterFile is the content of one filter file (a CBOR array).
type filterFile struct {
	_          struct{} `cbor:",toarray"`
	Version    int
	Analyzer   string
	Generation uint64
	Name       string
	Values     map[string][]uint64
	Terms      map[string][]uint64
	Days       []*int64
}

// filters holds the loaded filter files of an index.
type filters struct {
	mu    sync.Mutex
	files map[string]*filterFile
}

// isTextField says whether a clause's field is one of the page's three text
// segments (or no field), as opposed to a front matter field.
func isTextField(f string) bool {
	if f == "" {
		return true
	}
	_, ok := fieldWeight[f]
	return ok
}

/** FilterFields lists the filter files the index holds, by name, with the roles
 * of each.
 *
 * Returns:
 *   []FilterInfo — empty for an index built with no mapped fields
 *
 * Example:
 *   for _, f := range ix.FilterFields() { fmt.Println(f.Name, f.Facet) }
 */
func (ix *Index) FilterFields() []FilterInfo {
	var out []FilterInfo
	for name, r := range ix.m.Filters {
		out = append(out, FilterInfo{Name: name, Facet: r.has(roleFacet), Fielded: r.has(roleFielded), Date: r.has(roleDate)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

/** QueryFields lists the field names a query may name for this index: title,
 * description and body, then each front matter field a clause can search (the
 * ones with the fielded role), by name. A caller that searches several indexes
 * gives ParseQuery the union.
 *
 * Returns:
 *   []string — field names
 *
 * Example:
 *   n, err := index.ParseQuery(q, ix.QueryFields())
 */
func (ix *Index) QueryFields() []string {
	out := append([]string(nil), defaultFields...)
	var names []string
	for name, r := range ix.m.Filters {
		if r.has(roleFielded) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return append(out, names...)
}

// filter loads, checks and keeps the filter file of a field.
func (ix *Index) filter(name string) (*filterFile, error) {
	ix.flt.mu.Lock()
	defer ix.flt.mu.Unlock()
	if ff, ok := ix.flt.files[name]; ok {
		return ff, nil
	}
	ref, ok := ix.m.Filters[name]
	if !ok {
		return nil, nil
	}
	raw, err := readFile(ix.src, "x", ix.m.Compression, ref.File)
	if err != nil {
		return nil, err
	}
	var ff filterFile
	if err := decMode.Unmarshal(raw, &ff); err != nil {
		return nil, dataf("filter file %s: %v", ref.File.Digest, err)
	}
	if ff.Version != 1 || ff.Analyzer != Analyzer || ff.Generation != ix.m.Generation || ff.Name != name {
		return nil, dataf("filter file %s: header differs from the manifest", ref.File.Digest)
	}
	n := ix.m.Documents.Count
	check := func(m map[string][]uint64) error {
		for k, docs := range m {
			for i, d := range docs {
				if d >= n || (i > 0 && d <= docs[i-1]) {
					return dataf("filter file %s: the documents of %q are not in order or are out of range", ref.File.Digest, k)
				}
			}
		}
		return nil
	}
	if err := check(ff.Values); err != nil {
		return nil, err
	}
	if err := check(ff.Terms); err != nil {
		return nil, err
	}
	if ref.has(roleDate) {
		if uint64(len(ff.Days)) != n {
			return nil, dataf("filter file %s: %d days for %d documents", ref.File.Digest, len(ff.Days), n)
		}
	} else if len(ff.Days) != 0 {
		return nil, dataf("filter file %s: days for a field that is not a date", ref.File.Digest)
	}
	if ix.flt.files == nil {
		ix.flt.files = map[string]*filterFile{}
	}
	ix.flt.files[name] = &ff
	return &ff, nil
}

// fieldDocs returns the documents whose field has every word of the leaf n in
// its values (the last as a prefix when n says so). A field the index has no
// terms for matches nothing.
func (ix *Index) fieldDocs(n *Node) (map[int]bool, error) {
	out := map[int]bool{}
	ref, ok := ix.m.Filters[n.Field]
	if !ok || !ref.has(roleFielded) {
		return out, nil
	}
	ff, err := ix.filter(n.Field)
	if err != nil || ff == nil {
		return out, err
	}
	var docs map[int]bool
	for i, w := range n.Words {
		cur := map[int]bool{}
		if n.Prefix && i == len(n.Words)-1 {
			for term, list := range ff.Terms {
				if strings.HasPrefix(term, w) {
					for _, d := range list {
						cur[int(d)] = true
					}
				}
			}
		} else {
			for _, d := range ff.Terms[w] {
				cur[int(d)] = true
			}
		}
		if docs == nil {
			docs = cur
			continue
		}
		for d := range docs {
			if !cur[d] {
				delete(docs, d)
			}
		}
	}
	if docs == nil {
		return out, nil
	}
	return docs, nil
}

// fieldLeaf answers a clause on a front matter field: the pages that match, none
// scored (the clause selects; the text clauses rank).
func (ev *evaluator) fieldLeaf(n *Node) (map[int]float64, error) {
	docs, err := ev.ix.fieldDocs(n)
	if err != nil {
		return nil, err
	}
	out := make(map[int]float64, len(docs))
	for d := range docs {
		out[d] = 0
	}
	return out, nil
}

// fieldTerms reads a fragment's value of a front matter field as terms.
func fieldTerms(fr *fragmentRec, name string) []string {
	var out []string
	add := func(s string) {
		for _, w := range Words(s) {
			if len(w.Term) <= MaxTermBytes {
				out = append(out, w.Term)
			}
		}
	}
	switch v := fr.Fields[name].(type) {
	case string:
		add(v)
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok {
				add(s)
			}
		}
	}
	return out
}

// fieldMatches is the page's own answer to a clause on a front matter field,
// from the values its fragment keeps: the same rule as the index's.
func fieldMatches(fr *fragmentRec, n *Node) bool {
	have := fieldTerms(fr, n.Field)
	for i, w := range n.Words {
		found := false
		for _, t := range have {
			if t == w || (n.Prefix && i == len(n.Words)-1 && strings.HasPrefix(t, w)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return len(n.Words) > 0
}

// fieldValues is what the build keeps of one field across the documents.
type fieldValues struct {
	spec   FieldSpec
	values map[string][]uint64
	terms  map[string][]uint64
	days   map[int]int64
}

func newFieldValues(s FieldSpec) *fieldValues {
	return &fieldValues{spec: s, values: map[string][]uint64{}, terms: map[string][]uint64{}, days: map[int]int64{}}
}

func appendDoc(m map[string][]uint64, key string, doc int) {
	if l := m[key]; len(l) > 0 && l[len(l)-1] == uint64(doc) {
		return
	}
	m[key] = append(m[key], uint64(doc))
}

// add takes the values a document has for the field.
func (f *fieldValues) add(doc int, vals []string) {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if f.spec.Facet && !f.spec.Date { // a date field's facet is its years, from the days
			appendDoc(f.values, term(v), doc)
		}
		if f.spec.Fielded {
			for _, w := range Words(v) {
				if len(w.Term) <= MaxTermBytes {
					appendDoc(f.terms, w.Term, doc)
				}
			}
		}
		if f.spec.Date {
			if _, ok := f.days[doc]; !ok {
				if first, _, ok := ParseDay(v); ok {
					f.days[doc] = int64(first)
				}
			}
		}
	}
}

func (f *fieldValues) empty() bool {
	return len(f.values) == 0 && len(f.terms) == 0 && len(f.days) == 0
}

// roles lists the field's roles in the manifest's order.
func (f *fieldValues) roles() []string {
	var r []string
	if f.spec.Facet {
		r = append(r, roleFacet)
	}
	if f.spec.Fielded {
		r = append(r, roleFielded)
	}
	if f.spec.Date {
		r = append(r, roleDate)
	}
	return r
}

// file is the filter file for documents numbered 0 to docs-1.
func (f *fieldValues) file(generation uint64, docs int) filterFile {
	ff := filterFile{Version: 1, Analyzer: Analyzer, Generation: generation, Name: f.spec.Name}
	if f.spec.Facet {
		ff.Values = f.values
	} else {
		ff.Values = map[string][]uint64{}
	}
	if f.spec.Fielded {
		ff.Terms = f.terms
	} else {
		ff.Terms = map[string][]uint64{}
	}
	ff.Days = []*int64{}
	if f.spec.Date {
		ff.Days = make([]*int64, docs)
		for d, v := range f.days {
			v := v
			ff.Days[d] = &v
		}
	}
	return ff
}

// maxFilterPairs is the most keys a filter file's maps may have (the decoder's
// limit on a map).
const maxFilterPairs = 100000

// nonEmpty is the values with something in them, space trimmed.
func nonEmpty(vals []string) []string {
	var out []string
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// fieldValuesFor checks the specs a build was given and makes a collector for
// each, in name order.
func fieldValuesFor(specs []FieldSpec) ([]*fieldValues, error) {
	seen := map[string]bool{}
	var out []*fieldValues
	for _, s := range specs {
		if !filterNameRE.MatchString(s.Name) || fieldWeight[s.Name] != 0 {
			return nil, dataf("%q cannot name a field of the index", s.Name)
		}
		if seen[s.Name] {
			return nil, dataf("the field %q is given twice", s.Name)
		}
		seen[s.Name] = true
		if !s.Facet && !s.Fielded && !s.Date {
			continue
		}
		out = append(out, newFieldValues(s))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].spec.Name < out[j].spec.Name })
	return out, nil
}

/** DateFields lists the fields with the date role, by name.
 *
 * Returns:
 *   []string — sorted field names
 *
 * Example:
 *   n, err := index.ParseQueryFields(q, ix.QueryFields(), ix.DateFields())
 */
func (ix *Index) DateFields() []string { return ix.fieldsWith(roleDate) }

/** FacetFields lists the fields with the facet role, by name. A date field with
 * the facet role is a facet of years (YearCounts); any other is a facet of
 * values (FacetCounts).
 *
 * Returns:
 *   []string — sorted field names
 *
 * Example:
 *   for _, f := range ix.FacetFields() { _ = f }
 */
func (ix *Index) FacetFields() []string { return ix.fieldsWith(roleFacet) }

func (ix *Index) fieldsWith(role string) []string {
	var out []string
	for name, r := range ix.m.Filters {
		if r.has(role) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

/** FacetCount is one value of a facet and the documents that have it.
 *
 * Fields:
 *   Value (string) — the value, folded
 *   Count (int)    — documents of the set asked about that have it
 *
 * Example:
 *   c := index.FacetCount{Value: "go", Count: 12}
 */
type FacetCount struct {
	Value string
	Count int
}

/** FacetCounts counts, for each value of a facet field, the documents of docs
 * that have it, most first and then by value. A value no document of docs has is
 * left out. A field that is not a facet of values (unknown, a date, no facet role)
 * has none.
 *
 * Parameters:
 *   field (string)       — the field's name
 *   docs (map[int]bool)  — the document numbers to count, usually a search's hits
 *
 * Returns:
 *   []FacetCount — the values with their counts
 *   error        — ErrData when the filter file is wrong
 *
 * Example:
 *   counts, err := ix.FacetCounts("keywords", hitDocs)
 */
func (ix *Index) FacetCounts(field string, docs map[int]bool) ([]FacetCount, error) {
	ref, ok := ix.m.Filters[field]
	if !ok || !ref.has(roleFacet) || ref.has(roleDate) {
		return nil, nil
	}
	ff, err := ix.filter(field)
	if err != nil || ff == nil {
		return nil, err
	}
	var out []FacetCount
	for value, list := range ff.Values {
		n := 0
		for _, d := range list {
			if docs[int(d)] {
				n++
			}
		}
		if n > 0 {
			out = append(out, FacetCount{Value: value, Count: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	return out, nil
}

/** YearCounts counts the documents of docs by the year of a date field. A
 * document with no date in the field is not counted.
 *
 * Parameters:
 *   field (string)       — a date field's name
 *   docs (map[int]bool)  — the document numbers to count
 *
 * Returns:
 *   map[int]int — documents for each year
 *   error       — ErrData when the filter file is wrong
 *
 * Example:
 *   years, err := ix.YearCounts("datepublished", hitDocs)
 */
func (ix *Index) YearCounts(field string, docs map[int]bool) (map[int]int, error) {
	out := map[int]int{}
	ref, ok := ix.m.Filters[field]
	if !ok || !ref.has(roleDate) {
		return out, nil
	}
	ff, err := ix.filter(field)
	if err != nil || ff == nil {
		return out, err
	}
	for d := range docs {
		if d >= 0 && d < len(ff.Days) && ff.Days[d] != nil {
			out[time.Unix(*ff.Days[d]*86400, 0).UTC().Year()]++
		}
	}
	return out, nil
}

// rangeDocs returns the documents whose date in the field falls in the range.
func (ix *Index) rangeDocs(n *Node) (map[int]bool, error) {
	out := map[int]bool{}
	ref, ok := ix.m.Filters[n.Field]
	if !ok || !ref.has(roleDate) {
		return out, nil
	}
	ff, err := ix.filter(n.Field)
	if err != nil || ff == nil {
		return out, err
	}
	for d, day := range ff.Days {
		if day != nil && int(*day) >= n.Lo && int(*day) <= n.Hi {
			out[d] = true
		}
	}
	return out, nil
}

// rangeLeaf answers a range clause: the pages dated in it, none scored.
func (ev *evaluator) rangeLeaf(n *Node) (map[int]float64, error) {
	docs, err := ev.ix.rangeDocs(n)
	if err != nil {
		return nil, err
	}
	out := make(map[int]float64, len(docs))
	for d := range docs {
		out[d] = 0
	}
	return out, nil
}

// rangeMatches is the page's own answer to a range clause, from the values its
// fragment keeps: the first that is a date, as the build takes it.
func rangeMatches(fr *fragmentRec, n *Node) bool {
	var vals []string
	switch v := fr.Fields[n.Field].(type) {
	case string:
		vals = []string{v}
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok {
				vals = append(vals, s)
			}
		}
	}
	for _, v := range vals {
		if first, _, ok := ParseDay(strings.TrimSpace(v)); ok {
			return first >= n.Lo && first <= n.Hi
		}
	}
	return false
}
