package server

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode"

	"github.com/rsdoiel/cmarkfind/corpus"
)

// metaPrefix starts the corpus meta key of each setting: server_port, and so on.
const metaPrefix = "server_"

var whole = regexp.MustCompile(`^[0-9]{1,9}$`)

/** Defaults are the settings of a server nobody has configured: port 8900, as
 * many searches at once as there are CPUs (at least 2), 100 hits a page, no
 * stylesheet and the information endpoints on.
 *
 * Returns:
 *   Settings — the defaults
 *
 * Example:
 *   s := server.Defaults()
 */
func Defaults() Settings {
	return Settings{Port: 8900, Concurrency: max(2, runtime.NumCPU()), Results: 100, Info: true, Title: DefaultTitle}
}

/** ReadSettings reads the settings from the corpus's meta table, with the
 * default for any that is not stored.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the reads
 *   db (*corpus.DB)       — the corpus
 *
 * Returns:
 *   Settings        — the settings in force
 *   map[string]bool — the names that are stored and not defaulted
 *   error           — from the database; ErrBadSetting for a stored value this program would not have written
 *
 * Example:
 *   s, stored, err := server.ReadSettings(ctx, db)
 */
func ReadSettings(ctx context.Context, db *corpus.DB) (Settings, map[string]bool, error) {
	s, stored := Defaults(), map[string]bool{}
	for _, name := range SettingNames {
		v, ok, err := db.Meta(ctx, metaPrefix+name)
		if err != nil {
			return s, nil, err
		}
		if !ok {
			continue
		}
		norm, err := checkSetting(name, v)
		if err != nil {
			return s, nil, fmt.Errorf("the stored setting %s: %w", name, err)
		}
		apply(&s, name, norm)
		stored[name] = true
	}
	return s, stored, nil
}

/** WriteSetting stores one setting in the corpus's meta table; the value
 * "default" removes it. The value is checked first and nothing is stored when it
 * is refused.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the write
 *   db (*corpus.DB)       — the corpus
 *   name (string)         — port, concurrency, results, css or info
 *   value (string)        — the value, or "default"
 *
 * Returns:
 *   error — ErrBadSetting (exit 2) for an unknown name or a value out of range; from the database
 *
 * Example:
 *   err := server.WriteSetting(ctx, db, "results", "50")
 */
func WriteSetting(ctx context.Context, db *corpus.DB, name, value string) error {
	known := false
	for _, n := range SettingNames {
		known = known || n == name
	}
	if !known {
		return fmt.Errorf("%w: %q is not a setting (%s)", ErrBadSetting, name, strings.Join(SettingNames, ", "))
	}
	if value == "default" {
		return db.DeleteMeta(ctx, metaPrefix+name)
	}
	norm, err := checkSetting(name, value)
	if err != nil {
		return err
	}
	return db.SetMeta(ctx, metaPrefix+name, norm)
}

// apply puts a checked value into the settings.
func apply(s *Settings, name, v string) {
	switch name {
	case "port":
		s.Port, _ = strconv.Atoi(v)
	case "concurrency":
		s.Concurrency, _ = strconv.Atoi(v)
	case "results":
		s.Results, _ = strconv.Atoi(v)
	case "css":
		s.CSS = v
	case "info":
		s.Info = v == "on"
	case "title":
		s.Title = v
	}
}

// checkSetting validates a value and returns it as it is stored.
func checkSetting(name, v string) (string, error) {
	bad := func(format string, a ...any) (string, error) {
		return "", fmt.Errorf("%w: %s: %s", ErrBadSetting, name, fmt.Sprintf(format, a...))
	}
	ranged := func(lo, hi int) (string, error) {
		if !whole.MatchString(v) {
			return bad("%q is not a whole number from %d to %d", v, lo, hi)
		}
		n, _ := strconv.Atoi(v)
		if n < lo || n > hi {
			return bad("%d is not from %d to %d", n, lo, hi)
		}
		return strconv.Itoa(n), nil
	}
	switch name {
	case "port":
		return ranged(0, 65535)
	case "concurrency":
		return ranged(1, 256)
	case "results":
		return ranged(1, 1000)
	case "info":
		if v != "on" && v != "off" {
			return bad("%q is not on or off", v)
		}
		return v, nil
	case "css":
		if v == "" {
			return "", nil
		}
		return checkCSS(v)
	case "title":
		return checkTitle(v)
	}
	return bad("not a setting")
}

// checkCSS accepts an http or https address, or a path that starts with one /.
func checkCSS(v string) (string, error) {
	bad := func(why string) (string, error) {
		return "", fmt.Errorf("%w: css: %q is %s", ErrBadSetting, v, why)
	}
	if len(v) > 2048 {
		return bad("over 2,048 bytes")
	}
	for _, r := range v {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("\"'<>\\`", r) {
			return bad("not a plain address or path")
		}
	}
	if strings.HasPrefix(v, "/") {
		if strings.HasPrefix(v, "//") {
			return bad("a network-path reference, not a path or an http or https address")
		}
		return v, nil
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return bad("not a path starting with / or an http or https address")
	}
	return v, nil
}

// cssOrigin is what the content security policy allows for styles: 'self' for a
// path, the stylesheet's scheme and host for an address, empty for none.
func cssOrigin(css string) string {
	switch {
	case css == "":
		return ""
	case strings.HasPrefix(css, "/"):
		return "'self'"
	}
	u, _ := url.Parse(css)
	return u.Scheme + "://" + u.Host
}

// checkTitle accepts 1 to 16 characters of printable ASCII, without a quote, an
// apostrophe, a backslash, an angle bracket or an ampersand, and with no space at
// either end. 16 is OpenSearch's limit for a ShortName; the rest keeps the name
// the same in an XML attribute, an HTML page, a Markdown heading and the quoted
// title of a Link header.
func checkTitle(v string) (string, error) {
	bad := func(why string) (string, error) {
		return "", fmt.Errorf("%w: title: %q is %s", ErrBadSetting, v, why)
	}
	if v == "" || len(v) > 16 {
		return bad("not from 1 to 16 characters")
	}
	if v != strings.TrimSpace(v) {
		return bad("spaced at an end")
	}
	for _, r := range v {
		if r < 0x20 || r > 0x7e || strings.ContainsRune("\"'\\<>&", r) {
			return bad("not plain printable ASCII without \" ' \\ < > or &")
		}
	}
	return v, nil
}
