package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/rsdoiel/cmarkfind/index"
	"github.com/rsdoiel/markdownweb/fetch"
)

// ErrNoIndex marks a site that publishes no index (exit 1): the answer is no.
var ErrNoIndex = errors.New("no index is published")

const (
	// PointerPath is the file, beside toc.opml, that names where a site's
	// manifest is (cmarkfind DR-0017 item 5).
	PointerPath = ".well-known/cmarkfind.json"
	// DefaultManifestPath is where a site that has no pointer file publishes it.
	DefaultManifestPath = "cmarkfind/manifest.json"
	maxPointerSize      = 64 << 10
)

/** Found is what Discover learned about a published index.
 *
 * Fields:
 *   Manifest (string)    — the address of manifest.json
 *   Via (string)         — how it was found: "address" (given as one), "cmarkfind.json" or "default"
 *   Generation (uint64)  — the generation the manifest declares
 *   Pages (int)          — the pages the index holds
 *   Built (string)       — when it was built, as the manifest says
 *   SiteRoot (string)    — the site's folder (the one that holds .well-known); empty when given a manifest address
 */
type Found struct {
	Manifest   string
	Via        string
	Generation uint64
	Pages      int
	Built      string
	SiteRoot   string
}

/** SiteRoot returns the folder of a site address: the address with any query
 * or fragment dropped and a trailing /.well-known/toc.opml (or
 * /.well-known/cmarkfind.json) removed, and a trailing slash added.
 *
 * Parameters:
 *   addr (string) — an http or https address of a site, its outline or its pointer file
 *
 * Returns:
 *   string — the folder, always ending in "/"
 *   error  — a data error for an address that is not http or https
 *
 * Example:
 *   root, err := remote.SiteRoot("https://example.org/blog/.well-known/toc.opml") // https://example.org/blog/
 */
func SiteRoot(addr string) (string, error) {
	u, err := url.Parse(addr)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("%w: %q is not an http or https address", index.ErrData, addr)
	}
	u.RawQuery, u.Fragment, u.RawPath = "", "", ""
	p := u.Path
	for _, tail := range []string{"/.well-known/toc.opml", "/.well-known/cmarkfind.json"} {
		p = strings.TrimSuffix(p, tail)
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	u.Path = p
	return u.String(), nil
}

/** IsManifestAddress reports whether addr names a manifest.json (so Discover
 * and Mirror take it as it is and do not look for a pointer file).
 *
 * Parameters:
 *   addr (string) — an address
 *
 * Returns:
 *   bool — true when its path ends in /manifest.json
 *
 * Example:
 *   remote.IsManifestAddress("https://example.org/cmarkfind/manifest.json") // true
 */
func IsManifestAddress(addr string) bool {
	u, err := url.Parse(addr)
	return err == nil && path.Base(u.Path) == "manifest.json"
}

/** Discover finds the manifest of the index a site publishes and reads it (the
 * manifest alone: no other file is fetched, nothing is stored).
 *
 * With a manifest address it reads that. With a site address it asks for
 * /.well-known/cmarkfind.json at the site root and follows it, and only when
 * that file is absent (a 404) tries /cmarkfind/manifest.json. A file that is
 * there and names a manifest that is not is ErrNoIndex naming the address; it
 * does not fall back.
 *
 * Parameters:
 *   ctx (context.Context) — cancels the requests
 *   opts (fetch.Options)  — the fetcher's options; SameOrigin is always on
 *   addr (string)         — a site address or a manifest address
 *
 * Returns:
 *   *Found — what was found
 *   error  — ErrNoIndex; a data error (index.IsDataError) for a pointer file or manifest that is wrong; a wrapped fetch.ErrUnavailable when the server cannot be reached
 *
 * Example:
 *   f, err := remote.Discover(ctx, fetch.Options{}, "https://example.org/")
 */
func Discover(ctx context.Context, opts fetch.Options, addr string) (*Found, error) {
	var found Found
	var manifest string
	if IsManifestAddress(addr) {
		manifest, found.Via = addr, "address"
	} else {
		root, err := SiteRoot(addr)
		if err != nil {
			return nil, err
		}
		found.SiteRoot = root
		pointed, err := readPointer(ctx, opts, root)
		if err != nil {
			return nil, err
		}
		if pointed != "" {
			manifest, found.Via = pointed, "cmarkfind.json"
		} else {
			manifest, found.Via = root+DefaultManifestPath, "default"
		}
	}
	src, err := NewSource(ctx, opts, manifest)
	if err != nil {
		return nil, err
	}
	ix, err := index.OpenSource(src)
	if err != nil {
		if index.IsMissing(err) {
			if found.Via == "default" {
				return nil, fmt.Errorf("%w at %s (no %s, and nothing at %s)", ErrNoIndex, found.SiteRoot, PointerPath, DefaultManifestPath)
			}
			return nil, fmt.Errorf("%w: %s is not there (named by %s)", ErrNoIndex, manifest, found.Via)
		}
		return nil, err
	}
	found.Manifest, found.Generation, found.Pages, found.Built = manifest, ix.Generation(), ix.Count(), ix.Built()
	return &found, nil
}

// readPointer reads root's cmarkfind.json: "" when there is none (a 404), the
// manifest address it names otherwise.
func readPointer(ctx context.Context, opts fetch.Options, root string) (string, error) {
	opts.SameOrigin, opts.MaxBody = true, maxPointerSize
	href := root + PointerPath
	res, err := fetch.New(opts).Get(ctx, href, "application/json")
	if err != nil {
		return "", classify(err)
	}
	switch {
	case res.Status == 404 || res.Status == 410:
		return "", nil
	case res.Status < 200 || res.Status > 299:
		return "", fmt.Errorf("%w: %s answered HTTP %d", fetch.ErrUnavailable, href, res.Status)
	}
	ref, err := pointerIndex(res.Body)
	if err != nil {
		return "", fmt.Errorf("%w (%s)", err, href)
	}
	base, _ := url.Parse(href)
	return resolvePointer(base, ref)
}

// pointerIndex reads the "index" string of a cmarkfind.json: an object with no
// repeated key, whose other keys are ignored.
func pointerIndex(b []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return "", fmt.Errorf("%w: cmarkfind.json is not a JSON object", index.ErrData)
	}
	seen := map[string]bool{}
	ref, have := "", false
	for dec.More() {
		t, err := dec.Token()
		key, ok := t.(string)
		if err != nil || !ok {
			return "", fmt.Errorf("%w: cmarkfind.json is not valid JSON", index.ErrData)
		}
		if seen[key] {
			return "", fmt.Errorf("%w: cmarkfind.json repeats the key %q", index.ErrData, key)
		}
		seen[key] = true
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return "", fmt.Errorf("%w: cmarkfind.json is not valid JSON", index.ErrData)
		}
		if key == "index" {
			if err := json.Unmarshal(raw, &ref); err != nil {
				return "", fmt.Errorf("%w: cmarkfind.json: index is not a string", index.ErrData)
			}
			have = true
		}
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return "", fmt.Errorf("%w: cmarkfind.json is not valid JSON", index.ErrData)
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", fmt.Errorf("%w: cmarkfind.json has more than one value", index.ErrData)
	}
	if !have || ref == "" {
		return "", fmt.Errorf("%w: cmarkfind.json has no \"index\"", index.ErrData)
	}
	return ref, nil
}

// resolvePointer makes the manifest address a pointer names: a same-origin
// path or address, with no query or fragment, no dot segment, no control
// character, ending in manifest.json.
func resolvePointer(base *url.URL, ref string) (string, error) {
	bad := func(why string) (string, error) {
		return "", fmt.Errorf("%w: cmarkfind.json names %q, which is %s", index.ErrData, ref, why)
	}
	for _, r := range ref {
		if r < ' ' || r == 0x7f || r == '\\' {
			return bad("not a plain path or address")
		}
	}
	u, err := url.Parse(ref)
	if err != nil {
		return bad("not an address")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil || u.Opaque != "" || strings.Contains(ref, "?") || strings.Contains(ref, "#") {
		return bad("not a plain path or address")
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return bad("a path with dot segments")
		}
	}
	r := base.ResolveReference(u)
	if !sameOrigin(base, r) {
		return bad("on another origin")
	}
	if path.Base(r.Path) != "manifest.json" {
		return bad("not a manifest.json")
	}
	return r.String(), nil
}

func sameOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}
