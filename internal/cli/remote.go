package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/rsdoiel/cmarkfind"
	"github.com/rsdoiel/cmarkfind/remote"
	"github.com/rsdoiel/markdownweb/fetch"
)

// fetchOptions are the options of the fetcher the remote verbs use: the
// program's User-Agent; remote forces same-origin redirects itself.
func fetchOptions() fetch.Options {
	return fetch.Options{UserAgent: appName + "/" + cmarkfind.Version}
}

// httpAddress reports whether s is an http or https address with a host.
func httpAddress(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// remoteError gives the negative answer of the remote package its exit 1.
func remoteError(err error) error {
	if errors.Is(err, remote.ErrNoIndex) {
		return negative("%v", err)
	}
	return err
}

/** indexDiscover is `indexer discover SITE`: it reports where a site's
 * published index is and what its manifest says, and changes nothing (no
 * corpus is opened or made).
 */
func (o *out) indexDiscover(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("indexer discover", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if fset.NArg() != 1 {
		return fmt.Errorf("%w: indexer discover takes one site or manifest address", ErrUsage)
	}
	addr := fset.Arg(0)
	if !httpAddress(addr) {
		return fmt.Errorf("%w: %q is not an http or https address", ErrUsage, addr)
	}
	f, err := remote.Discover(ctx, fetchOptions(), addr)
	if err != nil {
		return remoteError(err)
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"manifest": f.Manifest, "via": f.Via, "generation": f.Generation, "pages": f.Pages, "built": f.Built})
		fmt.Fprintf(o.stdout, "%s\n", b)
		return nil
	}
	fmt.Fprintf(o.stdout, "manifest %s\nfound by %s\ngeneration %d\npages %d\nbuilt %s\n", f.Manifest, f.Via, f.Generation, f.Pages, f.Built)
	return nil
}
