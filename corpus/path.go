package corpus

import (
	"os"
	"path/filepath"
	"runtime"
)

/** DefaultPath returns where the corpus lives when nothing names it:
 * cmarkfind/corpus.db in the user's data directory ($XDG_DATA_HOME or
 * ~/.local/share on Linux, ~/Library/Application Support on macOS,
 * %LocalAppData% on Windows). cmarkfind and cmarkbrowser both use it, so they
 * find the same file.
 *
 * Returns:
 *   string — the path
 *
 * Example:
 *   p := corpus.DefaultPath() // "/home/me/.local/share/cmarkfind/corpus.db"
 */
func DefaultPath() string {
	var base string
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if base = os.Getenv("LocalAppData"); base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
	case "darwin":
		base = filepath.Join(home, "Library", "Application Support")
	default:
		if base = os.Getenv("XDG_DATA_HOME"); base == "" {
			base = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(base, "cmarkfind", "corpus.db")
}

/** PathFromEnv returns the CMARKFIND_DB variable when it is set, else DefaultPath.
 *
 * Returns:
 *   string — the path
 *
 * Example:
 *   p := corpus.PathFromEnv()
 */
func PathFromEnv() string {
	if p := os.Getenv("CMARKFIND_DB"); p != "" {
		return p
	}
	return DefaultPath()
}
