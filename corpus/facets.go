package corpus

// Facets of a result: for each facet a set defines, the values (or, for a date
// field, the years) of the pages the search found, with how many pages have each,
// and the query that searches again restricted to one.

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/rsdoiel/cmarkfind/index"
)

// maxFacetValues is the most values of one facet a result lists.
const maxFacetValues = 20

// setHits are the pages of one set a search found, by document number.
type setHits struct {
	site string
	ix   *index.Index
	docs map[int]bool
}

type facetAcc struct {
	label  string
	date   bool
	counts map[string]int
}

// facetClause is the clause of the query that asks for one value of a facet: a
// year of a date field, or a value of any other field as a phrase. A quote or a
// backslash in a value is left out of the clause, since it would end the phrase.
func facetClause(field string, date bool, value string) string {
	if date {
		return field + ":[" + value + " TO " + value + "]"
	}
	v := strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' {
			return ' '
		}
		return r
	}, value)
	return field + `:"` + strings.Join(strings.Fields(v), " ") + `"`
}

// facetsOf counts the facets the sets searched define over the pages each found.
// A set that defines no facet, or a facet no found page has a value for, adds
// nothing; the result lists the facets by field name.
func (d *DB) facetsOf(ctx context.Context, sets []setHits, query string) ([]FacetField, error) {
	acc := map[string]*facetAcc{}
	for _, s := range sets {
		fields := s.ix.FacetFields()
		if len(fields) == 0 || len(s.docs) == 0 {
			continue
		}
		m, err := d.FieldMapping(ctx, s.site)
		if err != nil {
			return nil, err
		}
		dates := map[string]bool{}
		for _, f := range s.ix.DateFields() {
			dates[f] = true
		}
		for _, f := range fields {
			counts := map[string]int{}
			if dates[f] {
				years, err := s.ix.YearCounts(f, s.docs)
				if err != nil {
					return nil, err
				}
				for y, n := range years {
					counts[strconv.Itoa(y)] += n
				}
			} else {
				vals, err := s.ix.FacetCounts(f, s.docs)
				if err != nil {
					return nil, err
				}
				for _, v := range vals {
					counts[v.Value] += v.Count
				}
			}
			if len(counts) == 0 {
				continue
			}
			a := acc[f]
			if a == nil {
				a = &facetAcc{label: m.Label(f), date: dates[f], counts: map[string]int{}}
				if a.label == "" {
					a.label = f
				}
				acc[f] = a
			}
			for v, n := range counts {
				a.counts[v] += n
			}
		}
	}
	names := make([]string, 0, len(acc))
	for f := range acc {
		names = append(names, f)
	}
	sort.Strings(names)
	base := strings.TrimSpace(query)
	var out []FacetField
	for _, f := range names {
		a := acc[f]
		vals := make([]FacetValue, 0, len(a.counts))
		for v, n := range a.counts {
			vals = append(vals, FacetValue{Value: v, Count: n})
		}
		sort.Slice(vals, func(i, j int) bool {
			if vals[i].Count != vals[j].Count {
				return vals[i].Count > vals[j].Count
			}
			return vals[i].Value < vals[j].Value
		})
		if len(vals) > maxFacetValues {
			vals = vals[:maxFacetValues]
		}
		for i := range vals {
			vals[i].Query = "(" + base + ") AND " + facetClause(f, a.date, vals[i].Value)
		}
		out = append(out, FacetField{Field: f, Label: a.label, Values: vals})
	}
	return out, nil
}
