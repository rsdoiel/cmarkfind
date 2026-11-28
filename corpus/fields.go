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
)

// ErrBadFieldMapping says a role, a field name or an assignment is not usable
// (exit class usage, 2).
var ErrBadFieldMapping = errors.New("corpus: not a usable field mapping")

// The four roles of a field, in the order they are listed.
var fieldRoles = []string{"facet", "fielded", "date", "text"}

// BuiltInField is what Roles returns for title, description and body: always
// searched and fielded, not mapped.
const BuiltInField = "built-in"

var builtInFields = []string{"title", "description", "body"}

// defaultFields is the built-in mapping. Date fields are listed in the order a
// page's date is chosen from them.
var defaultFields = map[string][]string{
	"keywords":      {"facet", "fielded"},
	"author":        {"facet", "fielded"},
	"date":          {"date"},
	"datepublished": {"date"},
	"datemodified":  {"date"},
	"pubdate":       {"date"},
	"created":       {"date"},
}

var defaultDateOrder = []string{"date", "datepublished", "datemodified", "pubdate", "created"}

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
	roles map[string][]string
}

// mappingBlob is the stored form of a set's overrides.
type mappingBlob struct {
	Fields map[string][]string `json:"fields"`
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

func newMapping(over map[string][]string) *Mapping {
	m := &Mapping{roles: map[string][]string{}}
	for k, v := range defaultFields {
		m.roles[k] = v
	}
	for k, v := range over {
		if len(v) == 0 {
			delete(m.roles, k)
		} else {
			m.roles[k] = v
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

/** DateFields lists the fields with the date role in the order a page's date is
 * taken from them: the default fields first in their order, then any others by
 * name.
 *
 * Returns:
 *   []string — lower case field names
 *
 * Example:
 *   for _, f := range m.DateFields() { _ = f }
 */
func (m *Mapping) DateFields() []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range defaultDateOrder {
		if m.has(f, "date") {
			out = append(out, f)
			seen[f] = true
		}
	}
	var rest []string
	for f := range m.roles {
		if m.has(f, "date") && !seen[f] {
			rest = append(rest, f)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func (d *DB) setOverrides(ctx context.Context, site string) (map[string][]string, error) {
	var blob string
	err := d.sql.QueryRowContext(ctx, `SELECT fields FROM sets WHERE site = ?`, site).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrNoSet, site)
	}
	if err != nil {
		return nil, classify(err)
	}
	if blob == "" {
		return map[string][]string{}, nil
	}
	var b mappingBlob
	if err := json.Unmarshal([]byte(blob), &b); err != nil {
		return nil, fmt.Errorf("%w: the field mapping of %s is not readable: %v", ErrSchema, site, err)
	}
	if b.Fields == nil {
		b.Fields = map[string][]string{}
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

// parseAssignment reads FIELD=ROLE[,ROLE]... into a lower case field name and
// its roles (empty for none).
func parseAssignment(a string) (string, []string, error) {
	name, roles, ok := strings.Cut(a, "=")
	if !ok {
		return "", nil, bad("%q is not FIELD=ROLE", a)
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if !fieldNameRE.MatchString(name) {
		return "", nil, bad("%q is not a field name (letters, digits, - and _)", name)
	}
	for _, b := range append(builtInFields, "abstract") {
		if name == b {
			return "", nil, bad("%s is built in and cannot be mapped", name)
		}
	}
	roles = strings.TrimSpace(roles)
	if roles == "" || roles == "none" {
		return name, nil, nil
	}
	have := map[string]bool{}
	for _, r := range strings.Split(roles, ",") {
		r = strings.TrimSpace(r)
		ok := false
		for _, known := range fieldRoles {
			ok = ok || r == known
		}
		if !ok {
			return "", nil, bad("%q is not a role (facet, fielded, date, text, or none)", r)
		}
		have[r] = true
	}
	return name, orderRoles(have), nil
}

/** AssignFields applies FIELD=ROLE[,ROLE]... assignments to a set's mapping. Roles
 * are facet, fielded, date and text, kept in that order whatever order they are
 * given in; FIELD=none (or FIELD= with no role) turns a field off, including a
 * default one. An assignment that equals the default removes the override. All
 * assignments are checked before any is applied. The set's index is out of date
 * afterwards when anything changed.
 *
 * Parameters:
 *   ctx (context.Context)  — cancels the update
 *   site (string)          — the set's site key, as ResolveSet returns it
 *   assignments ([]string) — each FIELD=ROLE,ROLE
 *
 * Returns:
 *   error — ErrBadFieldMapping for a bad role, field name or built-in field;
 *           ErrNoSet; database errors
 *
 * Example:
 *   err := db.AssignFields(ctx, site, []string{"tags=facet,fielded", "keywords=none"})
 */
func (d *DB) AssignFields(ctx context.Context, site string, assignments []string) error {
	if err := d.ensureSets(ctx); err != nil {
		return err
	}
	type change struct {
		name  string
		roles []string
	}
	var changes []change
	for _, a := range assignments {
		n, r, err := parseAssignment(a)
		if err != nil {
			return err
		}
		changes = append(changes, change{n, r})
	}
	over, err := d.setOverrides(ctx, site)
	if err != nil {
		return err
	}
	for _, c := range changes {
		def, isDefault := defaultFields[c.name]
		switch {
		case isDefault && equalRoles(def, c.roles):
			delete(over, c.name)
		case !isDefault && len(c.roles) == 0:
			delete(over, c.name)
		default:
			over[c.name] = append([]string{}, c.roles...) // [] is "off"
		}
	}
	if len(over) == 0 {
		return d.SetFields(ctx, site, 0, "")
	}
	b, err := json.Marshal(mappingBlob{Fields: over})
	if err != nil {
		return err
	}
	return d.SetFields(ctx, site, 1, string(b))
}
