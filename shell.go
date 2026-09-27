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
// in a 0700 folder: <state>/shell/zsh-<host>-<pid>-<stamp>.log, where
// <state> is the folder stateDir names, <host> is $HOST with anything but
// letters, digits, dots and dashes turned into _, and <stamp> is
// $EPOCHREALTIME with the dot taken out. Past 256 KB
// the hook renames the log to the same name ending in .done at the next
// prompt and starts a new one. A write that fails does the same at once,
// so a torn record is only ever the tail of a log nothing writes to any
// more, and the next prompt opens a fresh log. The next prompt also opens
// a fresh log when the log is gone, as when an importer in a container
// with its own pids took the shell for dead and deleted it. So the importer
// (shellimport.go) deletes a .done log, or the log of a shell on its own
// host whose pid is gone, and reads a live shell's log only up to where it
// got. A log named for another host, such as a second machine sharing the
// home folder, is not its to judge by pid.
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
//	f  the line gained a leading space, or came to match HISTORY_IGNORE:
//	   drop the line. The text is "new" when the ignored text does not
//	   hold the first half of the last line saved, so it replaced that
//	   line (a recall of an ignored line) and the line stands
//
// A line recalled from the shell's history and not edited since, one
// whose $BUFFER is still ${history[$HISTNO]}, gets no b or h record: the
// shell keeps it, and browsing with Up would fill unsent's history with
// copies. The line typed before the recall stays the last one saved, as
// zsh keeps it too, and Enter's s record says what ran.
//
// Only the shell's own prompt and its continuation lines are saved, never
// vared or select, and nothing while the line starts with a space or
// matches the user's HISTORY_IGNORE. zsh matches that pattern itself, as
// it does for its history file, with the user's extended_glob; a pattern
// zsh cannot parse matches nothing. The importer (shellimport.go) drops
// the line at the forget mark. A password read by sudo, ssh or read -s
// never passes through the line editor. zsh skips a redraw when more input is pending, so a shell killed
// in that gap loses its last keys. The hooks write only into a folder the
// shell's user owns, so a root shell that reads the user's rc file, as
// sudo -s does on macOS, saves nothing there and creates nothing.
//
// The hooks' functions run under emulate -L zsh, and add-zle-hook-widget
// is loaded under zsh emulation, because its own dispatcher fails under a
// user's sh_glob. TRAPHUP is set when the first log opens, so it wraps a
// TRAPHUP function the rc file defined after the hooks too. A list-form
// HUP trap (trap '...' HUP, or an empty one that ignores HUP) is left alone,
// because a TRAPHUP function would replace it; the last redraw's record
// stands in for the h record then. zsh prints its traps nowhere but
// standard output, and $(trap) runs in a subshell that has none, so the
// hooks list them into a file beside the log and delete it; if that fails,
// HUP is left alone as well. A subshell
// inherits TRAPHUP, and $$ there is still the parent's pid, so the trap
// hangs up ${sysparams[pid]}, the process it runs in.
const zshHooks = `# The command line, saved as you type it, for unsent list and restore.
if (( ! ${+_unsent_fd} )) && [[ -o interactive ]] &&
    zmodload zsh/system zsh/datetime zsh/parameter 2>/dev/null &&
    zmodload -F zsh/files b:zf_mkdir b:zf_mv b:zf_rm 2>/dev/null &&
    emulate zsh -c 'autoload -Uz add-zle-hook-widget'; then
  typeset -gi _unsent_fd=-1 _unsent_size=0 _unsent_n=0 _unsent_trapped=0
  typeset -g _unsent_log= _unsent_last=
  function _unsent_put {
    emulate -L zsh
    setopt no_multibyte
    local d p n
    if (( _unsent_fd == -1 )); then
      _unsent_fd=-2
      d=${UNSENT_HOME:-${XDG_STATE_HOME:-$HOME/.local/state}/unsent}
      p=$d/shell
      while [[ ! -e $p ]]; do p=${p:h}; done
      [[ -O $p ]] || return 1
      zf_mkdir -p -m 700 $d $d/shell 2>/dev/null
      _unsent_log=$d/shell/zsh-${HOST//[^A-Za-z0-9.-]/_}-$$-${EPOCHREALTIME//[^0-9]/}.log
      sysopen -a -m 600 -o create,excl,nofollow,sync,cloexec -u n $_unsent_log 2>/dev/null || return 1
      _unsent_fd=$n _unsent_size=0
      _unsent_put v 1 || return 1
      (( _unsent_trapped )) || _unsent_trap
    fi
    (( _unsent_fd >= 0 )) || return 1
    if syswrite -c n -o $_unsent_fd "$1 $EPOCHSECONDS ${#2}"$'\n'"$2"$'\n'; then
      (( _unsent_size += n ))
    else
      _unsent_shut -2
      return 1
    fi
  }
  function _unsent_shut {
    zf_mv $_unsent_log ${_unsent_log%.log}.done 2>/dev/null
    { exec {_unsent_fd}>&- } 2>/dev/null
    _unsent_fd=$1
  }
  function _unsent_line {
    local -i xg=0 ig=0
    [[ -o extended_glob ]] && xg=1
    emulate -L zsh
    (( xg )) && setopt extended_glob
    [[ $CONTEXT == start || $CONTEXT == cont ]] || return 0
    [[ $1 != s ]] && (( HISTNO != HISTCMD )) && [[ $BUFFER == "${history[$HISTNO]}" ]] && return 0
    local t=$PREBUFFER$BUFFER k=
    if [[ -n $t && -n $HISTORY_IGNORE ]]; then
      { { [[ $t == ${~HISTORY_IGNORE} ]] && ig=1 } 2>/dev/null } always { TRY_BLOCK_ERROR=0 }
    fi
    if [[ $t == ' '* ]] || (( ig )); then
      if (( _unsent_n )); then
        [[ $t == *"${_unsent_last[1,${#_unsent_last}/2]}"* ]] || k=new
        _unsent_put f "$k"
      fi
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
      if (( _unsent_fd >= 0 )) && [[ ! -e $_unsent_log ]]; then
        _unsent_shut -1
      elif (( _unsent_fd >= 0 && _unsent_size > 262144 )); then
        _unsent_shut -1
      elif (( _unsent_fd == -2 )); then
        _unsent_fd=-1
      fi
      _unsent_n=0 _unsent_last=
      _unsent_put i "$PWD"
    fi
    return 0
  }
  function _unsent_trap {
    emulate -L zsh
    setopt no_local_traps
    local f=${_unsent_log%.log}.trap l
    _unsent_trapped=1
    { trap >|$f && l=$(<$f) } 2>/dev/null || l=$'\ntrap -- ? HUP'
    zf_rm -f $f 2>/dev/null
    (( ${#${(M)${(f)l}:#trap -- * HUP}} )) && return 0
    if (( ${+functions[TRAPHUP]} )); then
      functions -c TRAPHUP _unsent_hup_next 2>/dev/null || return 0
    fi
    function TRAPHUP {
      zle && _unsent_line h
      if (( ${+functions[_unsent_hup_next]} )); then
        _unsent_hup_next "$@"
        return
      fi
      unfunction TRAPHUP
      kill -HUP ${sysparams[pid]}
    }
  }
  function _unsent_redraw { _unsent_line b }
  function _unsent_finish { _unsent_line s }
  add-zle-hook-widget line-init _unsent_init
  add-zle-hook-widget line-pre-redraw _unsent_redraw
  add-zle-hook-widget line-finish _unsent_finish
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
