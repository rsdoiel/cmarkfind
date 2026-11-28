package corpus

// Field mapping: how a set reads the front matter of its pages. The built-in
// default maps the common fields; a set's overrides (set fields NAME FIELD=ROLE)
// replace the roles of a field, or with none turn it off. The overrides are kept
// as one canonical JSON blob in the sets table, which the digest of the set's
// inputs includes, so a change makes the next build replace its index.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/rsdoiel/cmarkfind/index"
)

// ErrBadFieldMapping says a role, a field name or an assignment is not usable
// (exit class usage, 2).
var ErrBadFieldMapping = errors.New("corpus: not a usable field mapping")

const (
	roleFacet = "facet"
	roleDate  = "date"
)

// The four roles of a field, in the order they are listed.
var fieldRoles = []string{"facet", "fielded", "date", "text"}

// BuiltInField is what Roles returns for title, description and body: always
// searched and fielded, not mapped.
const BuiltInField = "built-in"

var builtInFields = []string{"title", "description", "body"}

// defaultFields is the built-in mapping: what a query can use, and nothing that
// shows in the output. No field is a facet until a set says so.
var defaultFields = map[string][]string{
	"keywords":      {"fielded"},
	"author":        {"fielded"},
	"date":          {"date"},
	"datepublished": {"date"},
	"datecreated":   {"date"},
	"datemodified":  {"date"},
	"pubdate":       {"date"},
	"created":       {"date"},
}

var fieldNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

/** Mapping is a set's effective field mapping: the built-in default with the set's
 * overrides applied. Field names are lower case; a page's front matter keys are
 * matched without regard to case.
 *
 * Example:
 *   m, _ := db.FieldMapping(ctx, site)
 *   roles := m.Roles("author") // ["facet", "fielded"]
 */
type Mapping struct {
	roles  map[string][]string
	labels map[string]string
}

// fieldEntry is one override in the stored blob: the roles ([] turns a field off)
// and a facet's label. The first form of the blob held the roles alone, as a list.
type fieldEntry struct {
	Roles []string `json:"roles"`
	Label string   `json:"label,omitempty"`
}

func (e *fieldEntry) UnmarshalJSON(b []byte) error {
	var list []string
	if json.Unmarshal(b, &list) == nil {
		e.Roles, e.Label = list, ""
		return nil
	}
	type plain fieldEntry
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*e = fieldEntry(p)
	return nil
}

// mappingBlob is the stored form of a set's overrides.
type mappingBlob struct {
	Fields map[string]fieldEntry `json:"fields"`
}

func orderRoles(set map[string]bool) []string {
	var out []string
	for _, r := range fieldRoles {
		if set[r] {
			out = append(out, r)
		}
	}
	return out
}

func equalRoles(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

func newMapping(over map[string]fieldEntry) *Mapping {
	m := &Mapping{roles: map[string][]string{}, labels: map[string]string{}}
	for k, v := range defaultFields {
		m.roles[k] = v
	}
	for k, v := range over {
		if len(v.Roles) == 0 {
			delete(m.roles, k)
			continue
		}
		m.roles[k] = v.Roles
		if v.Label != "" {
			m.labels[k] = v.Label
		}
	}
	return m
}

/** Fields lists the mapped fields by name, the built-in title, description and
 * body among them.
 *
 * Returns:
 *   []string — sorted field names
 *
 * Example:
 *   for _, f := range m.Fields() { fmt.Println(f, m.Roles(f)) }
 */
func (m *Mapping) Fields() []string {
	out := append([]string(nil), builtInFields...)
	for k := range m.roles {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

/** Roles lists a field's roles in the order facet, fielded, date, text. The
 * built-in fields (title, description, body) have the one role BuiltInField; an
 * unmapped field has none.
 *
 * Parameters:
 *   field (string) — the field's name, any case
 *
 * Returns:
 *   []string — the roles; nil for an unmapped field
 *
 * Example:
 *   roles := m.Roles("Keywords") // ["facet", "fielded"]
 */
func (m *Mapping) Roles(field string) []string {
	f := strings.ToLower(field)
	for _, b := range builtInFields {
		if f == b {
			return []string{BuiltInField}
		}
	}
	return append([]string(nil), m.roles[f]...)
}

// has reports whether a field has a role.
func (m *Mapping) has(field, role string) bool {
	for _, r := range m.roles[field] {
		if r == role {
			return true
		}
	}
	return false
}

/** DateFields lists the fields with the date role, by name.
 *
 * Returns:
 *   []string — lower case field names
 *
 * Example:
 *   for _, f := range m.DateFields() { _ = f }
 */
func (m *Mapping) DateFields() []string {
	var out []string
	for f := range m.roles {
		if m.has(f, "date") {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func (d *DB) setOverrides(ctx context.Context, site string) (map[string]fieldEntry, error) {
	var blob string
	err := d.sql.QueryRowContext(ctx, `SELECT fields FROM sets WHERE site = ?`, site).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrNoSet, site)
	}
	if err != nil {
		return nil, classify(err)
	}
	if blob == "" {
		return map[string]fieldEntry{}, nil
	}
	var b mappingBlob
	if err := json.Unmarshal([]byte(blob), &b); err != nil {
		return nil, fmt.Errorf("%w: the field mapping of %s is not readable: %v", ErrSchema, site, err)
	}
	if b.Fields == nil {
		b.Fields = map[string]fieldEntry{}
	}
	return b.Fields, nil
}

/** FieldMapping returns a set's effective field mapping.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the query
 *   site (string)         — the set's site key, as ResolveSet returns it
 *
 * Returns:
 *   *Mapping — the default with the set's overrides
 *   error    — ErrNoSet; ErrSchema when the stored overrides cannot be read
 *
 * Example:
 *   m, err := db.FieldMapping(ctx, site)
 */
func (d *DB) FieldMapping(ctx context.Context, site string) (*Mapping, error) {
	if err := d.ensureSets(ctx); err != nil {
		return nil, err
	}
	over, err := d.setOverrides(ctx, site)
	if err != nil {
		return nil, err
	}
	return newMapping(over), nil
}

func bad(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrBadFieldMapping, fmt.Sprintf(format, a...))
}

// assignment is one parsed FIELD=ROLE[,ROLE]...[,LABEL].
type assignment struct {
	name   string   // lower case
	typed  string   // as it was typed, the label when none is given
	roles  []string // facet, fielded, date, text in that order; none for a field turned off
	label  string   // "" when none was given
	hasLbl bool
}

// splitItems splits the roles and label on commas, outside quotes. A quoted item
// is a whole item and ends at its closing quote.
func splitItems(s string) ([]string, []bool, error) {
	var items []string
	var quoted []bool
	i := 0
	for i <= len(s) {
		if i < len(s) && s[i] == '"' {
			end := strings.IndexByte(s[i+1:], '"')
			if end < 0 {
				return nil, nil, bad("a quote is not closed in %q", s)
			}
			j := i + 1 + end + 1
			if j < len(s) && s[j] != ',' {
				return nil, nil, bad("text follows a closing quote in %q", s)
			}
			items = append(items, s[i+1:i+1+end])
			quoted = append(quoted, true)
			i = j + 1
			continue
		}
		j := strings.IndexByte(s[i:], ',')
		if j < 0 {
			items = append(items, strings.TrimSpace(s[i:]))
			quoted = append(quoted, false)
			break
		}
		items = append(items, strings.TrimSpace(s[i:i+j]))
		quoted = append(quoted, false)
		i += j + 1
	}
	return items, quoted, nil
}

func validLabel(l string) error {
	if strings.TrimSpace(l) == "" {
		return bad("a label cannot be empty")
	}
	if len([]rune(l)) > 60 {
		return bad("a label is at most 60 characters")
	}
	for _, r := range l {
		if unicode.IsControl(r) {
			return bad("a label has a control character")
		}
	}
	return nil
}

// parseAssignment reads FIELD=ROLE[,ROLE]...[,LABEL]: the roles first, then an
// optional label as the last item (quoted when it is more than one word), valid
// only with the facet role.
func parseAssignment(a string) (assignment, error) {
	name, rhs, ok := strings.Cut(a, "=")
	if !ok {
		return assignment{}, bad("%q is not FIELD=ROLE", a)
	}
	typed := strings.TrimSpace(name)
	as := assignment{name: strings.ToLower(typed), typed: typed}
	if !fieldNameRE.MatchString(as.name) {
		return assignment{}, bad("%q is not a field name (letters, digits, - and _)", name)
	}
	for _, b := range append(builtInFields, "abstract") {
		if as.name == b {
			return assignment{}, bad("%s is built in and cannot be mapped", as.name)
		}
	}
	rhs = strings.TrimSpace(rhs)
	if rhs == "" || rhs == "none" {
		return as, nil
	}
	items, quoted, err := splitItems(rhs)
	if err != nil {
		return assignment{}, err
	}
	have := map[string]bool{}
	for i, it := range items {
		isRole := false
		for _, known := range fieldRoles {
			isRole = isRole || (!quoted[i] && it == known)
		}
		switch {
		case isRole:
			have[it] = true
		case !quoted[i] && it == "none":
			return assignment{}, bad("none cannot be combined with other roles")
		default: // the label: the last item, with the facet role before it
			if i != len(items)-1 {
				return assignment{}, bad("%q is not a role (facet, fielded, date, text); a label is the last item", it)
			}
			if !have[roleFacet] {
				return assignment{}, bad("%q is not a role (facet, fielded, date, text), and only a facet has a label", it)
			}
			if err := validLabel(it); err != nil {
				return assignment{}, err
			}
			as.label, as.hasLbl = it, true
		}
	}
	if len(have) == 0 {
		return assignment{}, bad("%q gives no role", rhs)
	}
	as.roles = orderRoles(have)
	if have[roleFacet] && !as.hasLbl {
		as.label = as.typed // the field's name as it was typed
	}
	return as, nil
}

/** AssignFields applies FIELD=ROLE[,ROLE]...[,LABEL] assignments to a set's
 * mapping. Roles are facet, fielded, date and text, kept in that order whatever
 * order they are given in; FIELD=none (or FIELD= with no role) turns a field off,
 * including a default one. The label is the last item, quoted when it is more
 * than one word, valid only with the facet role; with none the facet is labelled
 * with the field's name as typed. An assignment that equals the default removes
 * the override. All assignments are checked before any is applied. The set's
 * index is out of date afterwards when anything changed.
 *
 * Parameters:
 *   ctx (context.Context)  — cancels the update
 *   site (string)          — the set's site key, as ResolveSet returns it
 *   assignments ([]string) — each FIELD=ROLE,ROLE or FIELD=facet,"A label"
 *
 * Returns:
 *   error — ErrBadFieldMapping for a bad role, label, field name or built-in
 *           field; ErrNoSet; database errors
 *
 * Example:
 *   err := db.AssignFields(ctx, site, []string{`datePublished=date,facet,"Pub Date"`, "keywords=none"})
 */
func (d *DB) AssignFields(ctx context.Context, site string, assignments []string) error {
	if err := d.ensureSets(ctx); err != nil {
		return err
	}
	if err := d.refuseRemoteSet(ctx, site); err != nil {
		return err
	}
	var parsed []assignment
	for _, a := range assignments {
		as, err := parseAssignment(a)
		if err != nil {
			return err
		}
		parsed = append(parsed, as)
	}
	over, err := d.setOverrides(ctx, site)
	if err != nil {
		return err
	}
	for _, as := range parsed {
		def, isDefault := defaultFields[as.name]
		switch {
		case isDefault && as.label == "" && equalRoles(def, as.roles):
			delete(over, as.name)
		case !isDefault && len(as.roles) == 0:
			delete(over, as.name)
		default:
			over[as.name] = fieldEntry{Roles: append([]string{}, as.roles...), Label: as.label} // [] is "off"
		}
	}
	if len(over) == 0 {
		return d.SetFields(ctx, site, 0, "")
	}
	b, err := json.Marshal(mappingBlob{Fields: over})
	if err != nil {
		return err
	}
	return d.SetFields(ctx, site, 2, string(b))
}

/** Label is a facet's label: the one the set gave it, else the field's name as it
 * was typed. A field that is not a facet has none.
 *
 * Parameters:
 *   field (string) — the field's name, any case
 *
 * Returns:
 *   string — the label; "" for a field that is not a facet
 *
 * Example:
 *   label := m.Label("datepublished") // "Pub Date"
 */
func (m *Mapping) Label(field string) string {
	f := strings.ToLower(field)
	if !m.has(f, roleFacet) {
		return ""
	}
	if l := m.labels[f]; l != "" {
		return l
	}
	return f
}

/** FacetDef is a field a set facets on.
 *
 * Fields:
 *   Field (string) — the front matter field, lower case
 *   Label (string) — what a reader sees
 *   Date (bool)    — true for a facet of years, false for a facet of values
 *
 * Example:
 *   f := corpus.FacetDef{Field: "keywords", Label: "Topics"}
 */
type FacetDef struct {
	Field, Label string
	Date         bool
}

/** Facets lists the facets of the mapping, by field name.
 *
 * Returns:
 *   []FacetDef — empty when the set defines no facet
 *
 * Example:
 *   for _, f := range m.Facets() { fmt.Println(f.Field, f.Label) }
 */
func (m *Mapping) Facets() []FacetDef {
	var out []FacetDef
	for f := range m.roles {
		if m.has(f, roleFacet) {
			out = append(out, FacetDef{Field: f, Label: m.Label(f), Date: m.has(f, roleDate)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

// commaFields are the fields whose value, given as one string, is a list
// separated by commas; any other field's string is one value.
var commaFields = map[string]bool{"keywords": true, "tags": true}

// specs lists what the index keeps of each mapped field with a facet, fielded
// or date role, by name.
func (m *Mapping) specs() []index.FieldSpec {
	var out []index.FieldSpec
	for name, roles := range m.roles {
		s := index.FieldSpec{Name: name}
		for _, r := range roles {
			s.Facet = s.Facet || r == "facet"
			s.Fielded = s.Fielded || r == "fielded"
			s.Date = s.Date || r == "date"
		}
		if s.Facet || s.Fielded || s.Date {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// pageFields reads a page's front matter (the JSON of pages.meta) as the values
// of the fields the mapping names: the fields for the index, and the words of
// the fields with the text role, for the page's description. The keys are
// matched without regard to case.
func (m *Mapping) pageFields(meta string) (fields map[string][]string, text []string) {
	if meta == "" || meta == "{}" {
		return nil, nil
	}
	var raw map[string]any
	if json.Unmarshal([]byte(meta), &raw) != nil {
		return nil, nil
	}
	lower := map[string]any{}
	for k, v := range raw {
		lower[strings.ToLower(k)] = v
	}
	fields = map[string][]string{}
	for name, roles := range m.roles {
		v, ok := lower[name]
		if !ok {
			continue
		}
		var vals []string
		switch x := v.(type) {
		case string:
			if commaFields[name] {
				for _, part := range strings.Split(x, ",") {
					vals = append(vals, part)
				}
			} else {
				vals = []string{x}
			}
		case []any:
			for _, e := range x {
				if s, ok := e.(string); ok {
					vals = append(vals, s)
				}
			}
		}
		var kept []string
		for _, v := range vals {
			if v = strings.TrimSpace(v); v != "" {
				kept = append(kept, v)
			}
		}
		if len(kept) == 0 {
			continue
		}
		fields[name] = kept
		for _, r := range roles {
			if r == "text" {
				text = append(text, kept...)
			}
		}
	}
	return fields, text
}
