package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"

	"github.com/rsdoiel/cmarkfind"
	"github.com/rsdoiel/cmarkfind/corpus"
	"github.com/rsdoiel/cmarkfind/server"
)

// serverVersion is what the server's /version says.
func serverVersion() server.Version {
	return server.Version{Name: appName, Version: cmarkfind.Version, ReleaseDate: cmarkfind.ReleaseDate, Hash: cmarkfind.ReleaseHash}
}

/** serverVerb is `server`: the loopback search service (DR-0018). Bare `server`
 * runs it on the stored settings; `server port N` runs it once on port N;
 * `server set NAME VALUE` stores a setting and `server show` lists them.
 */
func (o *out) serverVerb(ctx context.Context, dbFile string, args []string) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "":
		return o.serverRun(ctx, dbFile, args, -1)
	case "port":
		return o.serverRun(ctx, dbFile, args, 0)
	case "set":
		return o.serverSet(ctx, dbFile, args)
	case "show":
		return o.serverShow(ctx, dbFile, args)
	}
	return fmt.Errorf("%w: unknown server verb %q (port, set, show)", ErrUsage, sub)
}

// settingLine is one setting as show prints it.
func settingLine(name string, s server.Settings, stored map[string]bool) string {
	kind := "default"
	if stored[name] {
		kind = "stored"
	}
	return fmt.Sprintf("%s %s (%s)", name, settingText(name, s), kind)
}

func settingText(name string, s server.Settings) string {
	switch name {
	case "port":
		return strconv.Itoa(s.Port)
	case "concurrency":
		return strconv.Itoa(s.Concurrency)
	case "results":
		return strconv.Itoa(s.Results)
	case "css":
		return s.CSS
	}
	if s.Info {
		return "on"
	}
	return "off"
}

func settingValue(name string, s server.Settings) any {
	switch name {
	case "port":
		return s.Port
	case "concurrency":
		return s.Concurrency
	case "results":
		return s.Results
	case "css":
		return s.CSS
	}
	return s.Info
}

func (o *out) serverShow(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("server show", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if fset.NArg() != 0 {
		return fmt.Errorf("%w: server show takes no arguments", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	s, stored, err := server.ReadSettings(ctx, db)
	if err != nil {
		return err
	}
	if o.json {
		m := map[string]any{}
		for _, n := range server.SettingNames {
			m[n] = map[string]any{"value": settingValue(n, s), "stored": stored[n]}
		}
		b, _ := json.Marshal(map[string]any{"settings": m})
		fmt.Fprintf(o.stdout, "%s\n", b)
		return nil
	}
	for _, n := range server.SettingNames {
		fmt.Fprintln(o.stdout, settingLine(n, s, stored))
	}
	return nil
}

func (o *out) serverSet(ctx context.Context, dbFile string, args []string) error {
	fset, j := flags("server set", o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	if fset.NArg() != 2 {
		return fmt.Errorf("%w: server set takes a NAME and a VALUE (or default), one of %s", ErrUsage, strings.Join(server.SettingNames, ", "))
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	name := fset.Arg(0)
	if err := server.WriteSetting(ctx, db, name, fset.Arg(1)); err != nil {
		if errors.Is(err, server.ErrBadSetting) {
			return err
		}
		return fmt.Errorf("%w: %w", ErrIO, err)
	}
	s, stored, err := server.ReadSettings(ctx, db)
	if err != nil {
		return err
	}
	if o.json {
		b, _ := json.Marshal(map[string]any{"name": name, "value": settingValue(name, s), "stored": stored[name]})
		fmt.Fprintf(o.stdout, "%s\n", b)
		return nil
	}
	fmt.Fprintln(o.stdout, settingLine(name, s, stored))
	return nil
}

// serverRun runs the service. port is the one to use: -1 for the stored one, or
// the verb's argument is read when port is 0 (the `server port N` form).
func (o *out) serverRun(ctx context.Context, dbFile string, args []string, mode int) error {
	name := "server"
	if mode == 0 {
		name = "server port"
	}
	fset, j := flags(name, o)
	if err := parse(fset, args, o, j); err != nil {
		return err
	}
	port := -1
	if mode == 0 {
		if fset.NArg() != 1 {
			return fmt.Errorf("%w: server port takes one number from 0 to 65535 (0 is a free port)", ErrUsage)
		}
		n, err := strconv.Atoi(fset.Arg(0))
		if err != nil || n < 0 || n > 65535 || strings.HasPrefix(fset.Arg(0), "+") {
			return fmt.Errorf("%w: %q is not a port from 0 to 65535", ErrUsage, fset.Arg(0))
		}
		port = n
	} else if fset.NArg() != 0 {
		return fmt.Errorf("%w: server takes no arguments; server port N runs it on one port", ErrUsage)
	}
	db, err := openExisting(dbFile)
	if err != nil {
		return err
	}
	defer db.Close()
	settings, stored, err := server.ReadSettings(ctx, db)
	if err != nil {
		return err
	}
	if port < 0 {
		port = settings.Port
	} else {
		settings.Port = port
	}
	ln, err := server.Listen(port)
	if err != nil {
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			return fmt.Errorf("%w: port %d: %w", errPermission, port, err)
		}
		return fmt.Errorf("%w: port %d: %w", ErrCantCreate, port, err)
	}
	defer ln.Close()
	if n, err := needBuild(ctx, db); err == nil && n > 0 {
		verb := "sets have no current index and are"
		if n == 1 {
			verb = "set has no current index and is"
		}
		fmt.Fprintf(o.stderr, "%s: warning: %d %s left out of searches; run cmarkfind indexer build\n", appName, n, verb)
	}
	srv := server.New(db, settings, serverVersion())
	srv.Stored = stored
	srv.Log = o.stderr
	url := "http://" + ln.Addr().String() + "/"
	if o.json {
		b, _ := json.Marshal(map[string]any{"url": url, "port": ln.Addr().(*net.TCPAddr).Port})
		fmt.Fprintf(o.stdout, "%s\n", b)
	} else {
		fmt.Fprintf(o.stdout, "listening on %s\n", url)
	}
	return srv.Serve(ctx, ln)
}

// needBuild counts the sets built here that have no index to read.
func needBuild(ctx context.Context, db *corpus.DB) (int, error) {
	sets, err := db.Sets(ctx)
	n := 0
	for _, s := range sets {
		if s.Source == "local" && !s.Built {
			n++
		}
	}
	return n, err
}
