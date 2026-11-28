---
title: cmarkfind-completion
section: 7
header: user manual
footer: version 0.0.1 a159457
date: 2026-11-26
author: R. S. Doiel
---

# NAME

cmarkfind-completion — set up tab completion in bash or PowerShell

# SYNOPSIS

cmarkfind completion bash|powershell

cmarkfind completion bash|powershell -install | -uninstall | -check

# DESCRIPTION

Write a completion script for a shell. It completes the verbs, indexer's
subverbs, each verb's options, the help topics, and file names where a file is
wanted (-db and the arguments of fill). The lists come from
the verbs themselves (see cmarkfind-verbs(7)), so a new verb or option is
completed with no change to the script generator.

With no option the script goes to standard output. Load it for one session:

    eval "$(cmarkfind completion bash)"
    cmarkfind completion powershell | Out-String | Invoke-Expression

-install does that for every new session. For bash it writes
$XDG_DATA_HOME/bash-completion/completions/cmarkfind (under ~/.local/share when
XDG_DATA_HOME is not set) and adds one line to the file a new shell reads,
~/.bashrc, or on macOS the first of ~/.bash_profile, ~/.bash_login and
~/.profile that exists; the line is what makes it work where no loader reads the
completions directory, as in macOS's bash 3.2. For PowerShell it writes
cmarkfind-completion.ps1 beside the profile (~/.config/powershell, or
Documents\PowerShell on Windows) and adds one line to Microsoft.PowerShell_profile.ps1.
The line is added once. An existing file that cmarkfind did not write is never
overwritten.

# OPTIONS

-install
: Write the script and have the shell load it.

-uninstall
: Remove the script if cmarkfind wrote it, and the line it added. Files
  cmarkfind did not write are left alone.

-check
: Say, one line each, whether the script is installed, current, and sourced by
  the startup file. It reads files only; for bash it ends with the command that
  asks the running shell.

-json
: Write the result as JSON: {"shell", "path"} for -install, {"shell", "removed"}
  for -uninstall and {"shell", "ok"} for -check.

# EXIT STATUS

0
: Done, or -check found everything in order.

1
: -uninstall found nothing of cmarkfind's to remove; -check found a problem.

2
: No shell, a shell other than bash or powershell (pwsh is accepted), or more
  than one of -install, -uninstall and -check.

66
: No home directory could be found.

73
: A file that cmarkfind did not write is in the way, or the script's directory
  cannot be made.

74
: The startup file could not be read or written.

77
: The system refused access.

See cmarkfind-exit-status(7).

# SEE ALSO

cmarkfind(1), cmarkfind-verbs(7)
