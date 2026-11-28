// Package cli is the cmarkfind command line: option parsing, the verbs, help
// and the exit codes of workspace DR-0003 (cmarkfind DR-0002).
package cli

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/rsdoiel/cmarkfind/corpus"
	"github.com/rsdoiel/cmarkfind/index"
	"github.com/rsdoiel/markdownweb/fetch"
	"github.com/rsdoiel/markdownweb/page"
	"github.com/rsdoiel/markdownweb/toc"
)

// Exit codes, per workspace DR-0003.
const (
	ExitOK           = 0
	ExitNegative     = 1
	ExitUsage        = 2
	ExitData         = 65
	ExitNoInput      = 66
	ExitUnavailable  = 69
	ExitInternal     = 70
	ExitCantCreate   = 73
	ExitIO           = 74
	ExitTempFail     = 75
	ExitNoPermission = 77
	ExitConfig       = 78
)

// ErrUsage marks a bad command line; wrap it with %w to exit 2.
var ErrUsage = errors.New("usage")

// ErrNoMatch is search's negative answer: exit 1, with nothing more to say.
var ErrNoMatch = errors.New("no matches")

// ErrCantCreate marks output that cannot be created (the database or its
// directory); wrap it with %w to exit 73.
var ErrCantCreate = errors.New("cannot create")

// ErrIO marks a read or write that failed part way (a startup file that cannot
// be written); wrap it with %w to exit 74.
var ErrIO = errors.New("i/o failed")

// ErrNothing is the sentinel of a negative answer that has something to say
// (completion -uninstall found nothing, -check found a problem): exit 1, message
// printed. Make one with negative.
var ErrNothing = errors.New("negative answer")

type negativeError string

func (e negativeError) Error() string        { return string(e) }
func (e negativeError) Is(target error) bool { return target == ErrNothing }

// negative is an error that exits 1 and says only its own text.
func negative(format string, a ...any) error { return negativeError(fmt.Sprintf(format, a...)) }

/** Classify is the one place an error becomes an exit code and its class name.
 * It classifies by sentinel with errors.Is, never by message text.
 *
 * Parameters:
 *   err (error) — nil, or an error wrapping one of the sentinels below
 *
 * Returns:
 *   int    — the exit code
 *   string — the class (ok, negative, usage, data, no_input, unavailable,
 *            internal, cant_create, io, temp_fail, no_permission, config)
 *
 * Example:
 *   code, class := cli.Classify(fmt.Errorf("%w: no query", cli.ErrUsage)) // 2, "usage"
 */
func Classify(err error) (int, string) {
	switch {
	case err == nil:
		return ExitOK, "ok"
	case errors.Is(err, ErrUsage), errors.Is(err, corpus.ErrBadPrefix), errors.Is(err, corpus.ErrBadAddress), errors.Is(err, corpus.ErrBadTag), errors.Is(err, corpus.ErrAmbiguousSet), errors.Is(err, corpus.ErrBadCollectionName), errors.Is(err, corpus.ErrCollectionRoot), index.IsQueryLimit(err):
		return ExitUsage, "usage"
	case errors.Is(err, corpus.ErrIndexShared):
		return ExitConfig, "config"
	case errors.Is(err, fs.ErrPermission):
		return ExitNoPermission, "no_permission"
	case errors.Is(err, ErrCantCreate), errors.Is(err, corpus.ErrIndex):
		return ExitCantCreate, "cant_create"
	case errors.Is(err, ErrIO):
		return ExitIO, "io"
	case errors.Is(err, corpus.ErrLocked):
		return ExitTempFail, "temp_fail"
	case errors.Is(err, corpus.ErrSchema), errors.Is(err, fetch.ErrData), errors.Is(err, page.ErrData), errors.Is(err, fetch.ErrTooLarge), index.IsDataError(err):
		return ExitData, "data"
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, toc.ErrNoOutline), errors.Is(err, page.ErrNoInput), errors.Is(err, corpus.ErrNoSet), errors.Is(err, corpus.ErrNoCollection), index.IsMissing(err):
		return ExitNoInput, "no_input"
	case errors.Is(err, ErrNoMatch), errors.Is(err, ErrNothing), errors.Is(err, fetch.ErrForbidden):
		return ExitNegative, "negative"
	case errors.Is(err, fetch.ErrUnavailable), errors.Is(err, page.ErrUnavailable):
		return ExitUnavailable, "unavailable"
	}
	return ExitInternal, "internal"
}
