package main

import (
	"fmt"
	"io"
)

// The zsh hooks save the command line as it is typed. unsent never wraps
// the shell: the line editor hands its own buffer to a hook, and the hook
// appends it to a log that later unsent commands import. `unsent init zsh`
// prints the hooks and `unsent setup zsh` writes them into its block, so no
// process starts with the shell.
//
// Every write is a zsh builtin (zsh/system's syswrite on a descriptor
// sysopen opened once), about 0.03 ms; a fork per keystroke would cost
// about 150 ms. The descriptor is close-on-exec, so the commands the shell
// runs do not inherit it.
//
// Each shell writes its own log, created on the first prompt with mode 0600
// in a 0700 folder: <state>/shell/zsh-<pid>-<microseconds>.log, where
// <state> is the folder stateDir names. Past 256 KB the hook renames the
// log to the same name ending in .done at the next prompt and starts a new
// one, so the importer deletes a .done log, or the log of a shell whose pid
// is gone, and reads a live shell's log only up to where it got.
//
// A record is a header line, "<kind> <unix seconds> <bytes>", then that
// many bytes of text, then a line break. The kinds:
//
//	v  first record of every log: the format version, 1
//	i  a new prompt; the text is the working folder. A line with no s
//	   before the next i was cleared, by Ctrl+C for one
//	c  a continuation prompt: the s before it did not run the line
//	b  the line before a redraw, $PREBUFFER$BUFFER
//	s  Enter ended the line; its text
//	h  the window closed; the line at that moment
//	f  the line gained a leading space: drop this line's earlier records
//
// Only the shell's own prompt and its continuation lines are saved, never
// vared or select, and nothing while the line starts with a space. A
// password read by sudo, ssh or read -s never passes through the line
// editor. zsh skips a redraw when more input is pending, so a shell killed
// in that gap loses its last keys. The hooks' functions run under
// emulate -L zsh, and add-zle-hook-widget is loaded under zsh emulation,
// because its own dispatcher fails under a user's sh_glob. A subshell
// inherits TRAPHUP, and $$ there is still the parent's pid, so the trap
// hangs up ${sysparams[pid]}, the process it runs in.
const zshHooks = `# The command line, saved as you type it, for unsent list and restore.
if (( ! ${+_unsent_fd} )) && [[ -o interactive ]] &&
    zmodload zsh/system zsh/datetime 2>/dev/null &&
    zmodload -F zsh/files b:zf_mkdir b:zf_mv 2>/dev/null &&
    emulate zsh -c 'autoload -Uz add-zle-hook-widget'; then
  typeset -gi _unsent_fd=-1 _unsent_size=0 _unsent_n=0
  typeset -g _unsent_log= _unsent_last=
  function _unsent_put {
    emulate -L zsh
    setopt no_multibyte
    local d n
    if (( _unsent_fd == -1 )); then
      _unsent_fd=-2
      d=${UNSENT_HOME:-${XDG_STATE_HOME:-$HOME/.local/state}/unsent}
      zf_mkdir -p -m 700 $d $d/shell 2>/dev/null
      _unsent_log=$d/shell/zsh-$$-${EPOCHREALTIME//[^0-9]/}.log
      sysopen -a -m 600 -o create,excl,nofollow,sync,cloexec -u n $_unsent_log 2>/dev/null || return 1
      _unsent_fd=$n _unsent_size=0
      _unsent_put v 1
    fi
    (( _unsent_fd >= 0 )) || return 1
    syswrite -c n -o $_unsent_fd "$1 $EPOCHSECONDS ${#2}"$'\n'"$2"$'\n' && (( _unsent_size += n ))
  }
  function _unsent_line {
    emulate -L zsh
    [[ $CONTEXT == start || $CONTEXT == cont ]] || return 0
    local t=$PREBUFFER$BUFFER
    if [[ $t == ' '* ]]; then
      (( _unsent_n )) && _unsent_put f ''
      _unsent_n=0 _unsent_last=' '
      return 0
    fi
    [[ $1 == b && $t == "$_unsent_last" ]] && return 0
    _unsent_last=$t
    _unsent_put $1 "$t" && (( ++_unsent_n ))
    return 0
  }
  function _unsent_init {
    emulate -L zsh
    if [[ $CONTEXT == cont ]]; then
      _unsent_put c ''
    elif [[ $CONTEXT == start ]]; then
      if (( _unsent_fd >= 0 && _unsent_size > 262144 )); then
        zf_mv $_unsent_log ${_unsent_log%.log}.done 2>/dev/null
        exec {_unsent_fd}>&-
        _unsent_fd=-1
      fi
      _unsent_n=0 _unsent_last=
      _unsent_put i "$PWD"
    fi
    return 0
  }
  function _unsent_redraw { _unsent_line b }
  function _unsent_finish { _unsent_line s }
  add-zle-hook-widget line-init _unsent_init
  add-zle-hook-widget line-pre-redraw _unsent_redraw
  add-zle-hook-widget line-finish _unsent_finish
  if (( ! ${+functions[TRAPHUP]} )) || functions -c TRAPHUP _unsent_hup_next 2>/dev/null; then
    function TRAPHUP {
      zle && _unsent_line h
      if (( ${+functions[_unsent_hup_next]} )); then
        _unsent_hup_next "$@"
        return
      fi
      unfunction TRAPHUP
      kill -HUP ${sysparams[pid]}
    }
  fi
fi
`

// cmdInit prints the command-line hooks for a shell, for
// eval "$(unsent init zsh)" in place of the setup block's copy.
func cmdInit(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "zsh" {
		fmt.Fprintln(stderr, "unsent: usage: unsent init zsh (bash and fish are not supported yet)")
		return 2
	}
	fmt.Fprint(stdout, zshHooks)
	return 0
}
