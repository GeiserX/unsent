package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// The transcripts here are synthetic: built record by record by ct, or read
// from testdata/claude/transcripts/. Their shapes follow real Claude Code
// files; their text, ids and folders are made up.

const (
	ctNew = "2.1.284" // marks typed records
	ctOld = "2.1.150" // marks nothing
)

// ctRec is one transcript record under construction.
type ctRec map[string]any

// ctBuild writes records as JSONL. Fields every record carries (cwd,
// sessionId, version, a timestamp a second after the last one) are filled
// in when missing.
func ctBuild(recs ...ctRec) string {
	var b strings.Builder
	at := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	for _, r := range recs {
		at = at.Add(time.Second)
		if _, ok := r["timestamp"]; !ok {
			r["timestamp"] = at.Format("2006-01-02T15:04:05.000Z")
		}
		for k, v := range map[string]any{"cwd": "/work/demo", "sessionId": "s-demo", "version": ctNew} {
			if _, ok := r[k]; !ok {
				r[k] = v
			}
		}
		line, err := json.Marshal(r)
		if err != nil {
			panic(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func ctWith(r ctRec, kv ...any) ctRec {
	for i := 0; i+1 < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	return r
}

func ctParent(p string) any {
	if p == "" {
		return nil
	}
	return p
}

// ctTyped is a record typed in the box on a build that marks it.
func ctTyped(id, parent string, content any) ctRec {
	return ctRec{"type": "user", "uuid": id, "parentUuid": ctParent(parent),
		"origin": map[string]any{"kind": "human"}, "promptSource": "typed",
		"message": map[string]any{"role": "user", "content": content}}
}

// ctPlain is a user record with no marks at all.
func ctPlain(id, parent, version string, content any) ctRec {
	return ctRec{"type": "user", "uuid": id, "parentUuid": ctParent(parent), "version": version,
		"message": map[string]any{"role": "user", "content": content}}
}

// ctSays is an assistant record holding blocks, of message msg.
func ctSays(id, parent, msg string, blocks ...map[string]any) ctRec {
	return ctRec{"type": "assistant", "uuid": id, "parentUuid": ctParent(parent),
		"message": map[string]any{"id": msg, "role": "assistant", "content": blocks}}
}

func ctText(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func ctThinking() map[string]any {
	return map[string]any{"type": "thinking", "thinking": "hidden", "signature": "x"}
}

func ctToolUse(id, name string) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{"x": 1}}
}

// ctToolResult is a user record answering tool call id.
func ctToolResult(id, parent, call string, result any, isError bool) ctRec {
	block := map[string]any{"type": "tool_result", "tool_use_id": call, "content": "ok"}
	if isError {
		block["is_error"] = true
	}
	r := ctRec{"type": "user", "uuid": id, "parentUuid": ctParent(parent),
		"message": map[string]any{"role": "user", "content": []any{block}}}
	if result != nil {
		r["toolUseResult"] = result
	}
	return r
}

func ctSystem(id, parent, subtype string) ctRec {
	return ctRec{"type": "system", "uuid": id, "parentUuid": ctParent(parent), "subtype": subtype}
}

func ctQueued(id, parent string, att map[string]any) ctRec {
	att["type"] = "queued_command"
	return ctRec{"type": "attachment", "uuid": id, "parentUuid": ctParent(parent), "attachment": att}
}

func ctRead(t *testing.T, jsonl string) []claudeTurn {
	t.Helper()
	turns, err := readClaudeTranscript(strings.NewReader(jsonl))
	if err != nil {
		t.Fatal(err)
	}
	return turns
}

// ctBrief is what most tests compare: uuid, kind, text and what was asked.
type ctBrief struct {
	UUID, Kind, Text, Asked string
	ReplyTo                 bool
}

func ctBriefs(turns []claudeTurn) []ctBrief {
	out := []ctBrief{}
	for _, t := range turns {
		out = append(out, ctBrief{t.UUID, t.Kind, t.Text, t.Asked, t.ReplyTo})
	}
	return out
}

func ctExpect(t *testing.T, jsonl string, want ...ctBrief) {
	t.Helper()
	if want == nil {
		want = []ctBrief{}
	}
	if got := ctBriefs(ctRead(t, jsonl)); !reflect.DeepEqual(got, want) {
		t.Fatalf("turns:\n got %+v\nwant %+v", got, want)
	}
}

// Before 2.1.183 nothing marks a typed record: a plain record is a turn
// unless it is one of the harness's own. Since then a plain record is not.
func TestClaudeTranscriptVersionGate(t *testing.T) {
	ctExpect(t, ctBuild(
		ctPlain("u1", "", ctOld, "fix the build"),
		ctSays("a1", "u1", "m1", ctText("Done.")),
		ctPlain("u2", "a1", ctOld, "<task-notification>\n<task-id>t1</task-id></task-notification>"),
		ctPlain("u3", "u2", ctNew, "text a hook injected"),
		ctPlain("u4", "u3", "", "a record with no version"),
	),
		ctBrief{"u1", "typed", "fix the build", "", false},
		// The walk passes the records that are not turns.
		ctBrief{"u4", "typed", "a record with no version", "Done.", false},
	)
}

// Files mix versions after an upgrade mid-conversation: each record is
// judged by its own version.
func TestClaudeTranscriptMixedVersions(t *testing.T) {
	ctExpect(t, ctBuild(
		ctPlain("u1", "", "2.1.182", "old build, plain"),
		ctSays("a1", "u1", "m1", ctText("Which branch?")),
		ctPlain("u2", "a1", "2.1.183", "new build, plain: not a turn"),
		ctWith(ctTyped("u3", "u2", "main"), "version", "2.1.200"),
		ctPlain("u4", "u3", "2.1.99", "an older build again"),
		ctWith(ctPlain("u5", "u4", "2.1.300", "promptSource alone"), "promptSource", "typed"),
	),
		ctBrief{"u1", "typed", "old build, plain", "", false},
		ctBrief{"u3", "typed", "main", "Which branch?", true},
		ctBrief{"u4", "typed", "an older build again", "", false},
		ctBrief{"u5", "typed", "promptSource alone", "", false},
	)
}

// A turn's content is a string or blocks; an image adds nothing to the
// text, and an image alone is still a turn.
func TestClaudeTranscriptBlocksAndImages(t *testing.T) {
	img := map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "iVBO"}}
	ctExpect(t, ctBuild(
		ctTyped("u1", "", "a string"),
		ctTyped("u2", "u1", []any{ctText("[Image #1] what is this"), img}),
		ctTyped("u3", "u2", []any{img, ctText("first"), ctText("second")}),
		ctTyped("u4", "u3", []any{img}),
		ctTyped("u5", "u4", []any{}),
	),
		ctBrief{"u1", "typed", "a string", "", false},
		ctBrief{"u2", "typed", "[Image #1] what is this", "", false},
		ctBrief{"u3", "typed", "first\nsecond", "", false},
		ctBrief{"u4", "typed", "", "", false},
	)
}

// A slash command the human typed reads back as "/name args", on every
// version and with no marks at all (2.1.287 writes a custom command with
// neither origin nor promptSource); a record led by <command-name> is the
// harness's echo of a local command.
func TestClaudeTranscriptSlashCommands(t *testing.T) {
	ctExpect(t, ctBuild(
		ctTyped("u1", "", "<command-message>review</command-message>\n<command-name>/review</command-name>\n<command-args>the open PR</command-args>"),
		ctWith(ctTyped("u2", "u1", "<command-message>digest</command-message>\n<command-name>/digest</command-name>"), "promptSource", nil),
		ctPlain("u3", "u2", ctOld, "<command-message>notes</command-message>\n<command-name>/notes</command-name>\n<command-args></command-args>"),
		ctPlain("u4", "u3", ctNew, "<command-message>plan</command-message>\n<command-name>/plan</command-name>"),
		ctPlain("u5", "u4", ctNew, "<command-name>/model</command-name>\n            <command-message>model</command-message>\n            <command-args></command-args>"),
		ctPlain("u6", "u5", ctOld, "<local-command-stdout>Set model to x</local-command-stdout>"),
		ctWith(ctTyped("u7", "u6", "<command-message>skill</command-message>\n<command-name>skill</command-name>"), "isMeta", true),
		ctPlain("u8", "u7", "2.1.287", "<command-message>digest</command-message>\n<command-name>/digest</command-name>\n<command-args>where we were</command-args>"),
	),
		ctBrief{"u1", "slash", "/review the open PR", "", false},
		ctBrief{"u2", "slash", "/digest", "", false},
		ctBrief{"u3", "slash", "/notes", "", false},
		ctBrief{"u4", "slash", "/plan", "", false},
		ctBrief{"u8", "slash", "/digest where we were", "", false},
	)
}

// /compact typed off the chain, the boundary, the summary that repeats
// earlier messages and the caveat records replayed with earlier
// timestamps: none is a turn. The next turn's walk crosses the boundary
// through logicalParentUuid to the text before it.
func TestClaudeTranscriptCompaction(t *testing.T) {
	boundary := ctSystem("c1", "", "compact_boundary")
	boundary["logicalParentUuid"] = "a1"
	ctExpect(t, ctBuild(
		ctTyped("u1", "", "start"),
		ctSays("a1", "u1", "m1", ctText("Ready when you are?")),
		ctTyped("x1", "", "/compact"),
		boundary,
		ctWith(ctPlain("s1", "c1", ctNew, "This session is being continued from a previous conversation. The user said: start"), "isCompactSummary", true),
		ctWith(ctPlain("k1", "s1", ctOld, "Caveat: The messages below were generated by the user while running local commands."), "timestamp", "2026-03-01T09:00:00.000Z"),
		ctWith(ctPlain("k2", "k1", ctOld, "<command-name>/compact</command-name>"), "timestamp", "2026-03-01T09:00:01.000Z"),
		ctWith(ctPlain("k3", "k2", ctOld, "<local-command-stdout>Compacted</local-command-stdout>"), "timestamp", "2026-03-01T09:00:02.000Z"),
		ctTyped("u2", "k3", "carry on"),
	),
		ctBrief{"u1", "typed", "start", "", false},
		ctBrief{"u2", "typed", "carry on", "Ready when you are?", true},
	)
}

// A walk across a compact boundary: the summary's parent is the boundary,
// whose parentUuid is null and logicalParentUuid the last record before it.
func TestClaudeTranscriptWalksAcrossCompactBoundary(t *testing.T) {
	boundary := ctSystem("c1", "", "compact_boundary")
	boundary["logicalParentUuid"] = "a2"
	ctExpect(t, ctBuild(
		ctTyped("u1", "", "one"),
		ctSays("a1", "u1", "m1", ctText("Before the cut.")),
		ctSays("a2", "a1", "m1", ctToolUse("t1", "Bash")),
		boundary,
		ctWith(ctPlain("s1", "c1", ctNew, "This session is being continued"), "isCompactSummary", true),
		ctTyped("u2", "s1", "two"),
	),
		ctBrief{"u1", "typed", "one", "", false},
		ctBrief{"u2", "typed", "two", "Before the cut.", false},
	)
}

// logicalParentUuid can point forward and close a loop; the walk must end,
// and falls back to the last text in file order.
func TestClaudeTranscriptParentCycleEnds(t *testing.T) {
	c1 := ctSystem("c1", "", "compact_boundary")
	c1["logicalParentUuid"] = "c2"
	c2 := ctSystem("c2", "", "compact_boundary")
	c2["logicalParentUuid"] = "c1"
	done := make(chan []claudeTurn, 1)
	go func() {
		done <- ctRead(t, ctBuild(
			ctTyped("u1", "", "one"),
			ctSays("a1", "u1", "m1", ctText("Last thing said.")),
			c1, c2,
			ctTyped("u2", "c1", "two"),
		))
	}()
	select {
	case turns := <-done:
		if got := ctBriefs(turns); len(got) != 2 || got[1].Asked != "Last thing said." {
			t.Fatalf("turns: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the walk never ended on a parent loop")
	}
}

// A parent missing from the file: the last text before the turn in file
// order stands in.
func TestClaudeTranscriptDanglingParent(t *testing.T) {
	ctExpect(t, ctBuild(
		ctTyped("u1", "", "one"),
		ctSays("a1", "u1", "m1", ctText("First answer.")),
		ctSays("a2", "a1", "m2", ctText("Second answer?")),
		ctTyped("u2", "gone", "two"),
		ctSays("a3", "u2", "m3", ctText("After it.")),
	),
		ctBrief{"u1", "typed", "one", "", false},
		ctBrief{"u2", "typed", "two", "Second answer?", true},
	)
}

// A rewind leaves the abandoned branch in the file. The turn typed after
// the rewind hangs off the earlier answer, and the walk finds that one,
// where file order would give the abandoned branch's text.
func TestClaudeTranscriptRewindFollowsTheParent(t *testing.T) {
	ctExpect(t, ctBuild(
		ctTyped("u1", "", "plan it"),
		ctSays("a1", "u1", "m1", ctText("Plan A or plan B?")),
		ctTyped("u2", "a1", "A"),
		ctSays("a2", "u2", "m2", ctText("Plan A is done.")),
		ctTyped("u3", "a1", "B"),
	),
		ctBrief{"u1", "typed", "plan it", "", false},
		ctBrief{"u2", "typed", "A", "Plan A or plan B?", true},
		ctBrief{"u3", "typed", "B", "Plan A or plan B?", true},
	)
}

// Timestamps run backwards with concurrent writers: turns keep file order
// and their own times.
func TestClaudeTranscriptBackwardTimestamps(t *testing.T) {
	turns := ctRead(t, ctBuild(
		ctWith(ctTyped("u1", "", "later stamp"), "timestamp", "2026-03-01T10:00:05.000Z"),
		ctWith(ctSays("a1", "u1", "m1", ctText("Reply.")), "timestamp", "2026-03-01T10:00:03.000Z"),
		ctWith(ctTyped("u2", "a1", "earlier stamp"), "timestamp", "2026-03-01T10:00:01.250Z"),
	))
	if len(turns) != 2 || turns[0].UUID != "u1" || turns[1].UUID != "u2" {
		t.Fatalf("order: %+v", ctBriefs(turns))
	}
	want := time.Date(2026, 3, 1, 10, 0, 1, 250e6, time.UTC)
	if !turns[1].Time.Equal(want) || turns[1].Time.Location() != time.UTC {
		t.Fatalf("time %v, want %v", turns[1].Time, want)
	}
	if turns[1].Asked != "Reply." {
		t.Fatalf("asked %q", turns[1].Asked)
	}
}

// One API response split over records sharing message.id: every text
// block is joined, whichever record the walk reaches first. A response
// ending in a tool call with no text sends the walk on to the text before.
func TestClaudeTranscriptSplitResponse(t *testing.T) {
	ctExpect(t, ctBuild(
		ctTyped("u1", "", "look"),
		ctSays("a1", "u1", "m1", ctThinking()),
		ctSays("a2", "a1", "m1", ctText("Part one.")),
		ctSays("a3", "a2", "m1", ctText("Part two?")),
		ctSays("a4", "a3", "m1", ctToolUse("t1", "Bash")),
		ctTyped("u2", "a4", "stop"),
		ctSays("a5", "u2", "m2", ctText("Stopped. Shall I go on?")),
		ctSays("a6", "a5", "m2", ctToolUse("t2", "Read")),
		ctToolResult("r1", "a6", "t2", nil, false),
		ctSays("a7", "r1", "m3", ctThinking()),
		ctSays("a8", "a7", "m3", ctToolUse("t3", "Bash")),
		ctToolResult("r2", "a8", "t3", nil, false),
		ctTyped("u3", "r2", "yes"),
	),
		ctBrief{"u1", "typed", "look", "", false},
		ctBrief{"u2", "typed", "stop", "Part one.\nPart two?", true},
		ctBrief{"u3", "typed", "yes", "Stopped. Shall I go on?", true},
	)
}

// Prompts typed while the agent worked: absorbed mid-turn as a
// queued_command attachment, or sent later as a queued record. A
// notification queued the same way, a meta one and the queue's own
// bookkeeping records are not turns.
func TestClaudeTranscriptQueuedPrompts(t *testing.T) {
	human := map[string]any{"kind": "human"}
	ctExpect(t, ctBuild(
		ctTyped("u1", "", "go"),
		ctSays("a1", "u1", "m1", ctText("Working on it.")),
		ctQueued("q1", "a1", map[string]any{"commandMode": "prompt", "prompt": "also this", "origin": human}),
		ctWith(ctQueued("q2", "q1", map[string]any{"commandMode": "prompt", "prompt": []any{ctText("old build, blocks")}}), "version", ctOld),
		ctQueued("q3", "q2", map[string]any{"commandMode": "task-notification", "prompt": "<task-notification>done</task-notification>"}),
		ctQueued("q4", "q3", map[string]any{"commandMode": "prompt", "prompt": "meta", "origin": human, "isMeta": true}),
		ctWith(ctQueued("q5", "q4", map[string]any{"commandMode": "prompt", "prompt": "meta record", "origin": human}), "isMeta", true),
		ctQueued("q6", "q5", map[string]any{"commandMode": "prompt", "prompt": "<task-notification>x</task-notification>"}),
		ctQueued("q7", "q6", map[string]any{"commandMode": "prompt", "prompt": "from a peer", "origin": map[string]any{"kind": "peer"}}),
		ctRec{"type": "queue-operation", "operation": "enqueue", "content": "later"},
		ctWith(ctRec{"type": "queue-operation", "operation": "dequeue"}, "uuid", "op1"),
		ctWith(ctTyped("u2", "q7", "later"), "promptSource", "queued"),
	),
		ctBrief{"u1", "typed", "go", "", false},
		ctBrief{"q1", "absorbed", "also this", "Working on it.", false},
		ctBrief{"q2", "absorbed", "old build, blocks", "Working on it.", false},
		ctBrief{"u2", "queued", "later", "Working on it.", false},
	)
}

func ctAskResult(questions []string, answers map[string]string, extra ...any) map[string]any {
	qs := []any{}
	for _, q := range questions {
		qs = append(qs, map[string]any{"question": q, "header": "H", "multiSelect": false,
			"options": []any{map[string]any{"label": "x", "description": "y"}}})
	}
	r := map[string]any{"questions": qs, "answers": answers}
	for i := 0; i+1 < len(extra); i += 2 {
		r[extra[i].(string)] = extra[i+1]
	}
	return r
}

// AskUserQuestion answers are turns: answered (one question, a
// multi-select string, some questions left open, a note), not when nobody
// answered, and not when rejected (the typed record after it counts).
func TestClaudeTranscriptAskUserQuestion(t *testing.T) {
	ctExpect(t, ctBuild(
		ctTyped("u1", "", "ask me"),
		ctSays("a1", "u1", "m1", ctText("Some questions."), ctToolUse("q1", "AskUserQuestion")),
		ctToolResult("r1", "a1", "q1", ctAskResult([]string{"Which color?"}, map[string]string{"Which color?": "Blue"}), false),
		ctSays("a2", "r1", "m2", ctToolUse("q2", "AskUserQuestion")),
		ctToolResult("r2", "a2", "q2", ctAskResult([]string{"Which sizes?", "Ship when?", "Who reviews?"},
			map[string]string{"Who reviews?": "Me", "Which sizes?": "S, M"},
			"annotations", map[string]any{"Who reviews?": map[string]any{"notes": "only after lunch"}, "Which sizes?": map[string]any{"preview": "a preview"}}), false),
		ctSays("a3", "r2", "m3", ctToolUse("q3", "AskUserQuestion")),
		ctToolResult("r3", "a3", "q3", ctAskResult([]string{"Anyone there?"}, map[string]string{}, "afkTimeoutMs", 60000), false),
		ctSays("a4", "r3", "m4", ctToolUse("q4", "AskUserQuestion")),
		ctToolResult("r4", "a4", "q4", ctAskResult([]string{"Rejected?"}, map[string]string{"Rejected?": "x"}), true),
		ctTyped("u2", "r4", "no, do it differently"),
		ctSays("a5", "u2", "m5", ctToolUse("t5", "Bash")),
		ctToolResult("r5", "a5", "t5", ctAskResult([]string{"Not asked"}, map[string]string{"Not asked": "x"}), false),
		ctToolResult("r6", "gone", "q9", ctAskResult([]string{"Orphan"}, map[string]string{"Orphan": "x"}), false),
		ctTyped("u3", "r5", "next"),
	),
		ctBrief{"u1", "typed", "ask me", "", false},
		ctBrief{"r1", "answer", "Which color? → Blue", "Which color?", true},
		ctBrief{"r2", "answer", "Which sizes? → S, M; Who reviews? → Me (only after lunch)", "Which sizes?\nShip when?\nWho reviews?", true},
		ctBrief{"u2", "typed", "no, do it differently", "", false},
		ctBrief{"u3", "typed", "next", "", false},
	)
}

// The walk stops at an earlier human turn: a burst of prompts with no
// reply between them answers nothing. An answer counts as a human turn.
func TestClaudeTranscriptBurstAsksNothing(t *testing.T) {
	ctExpect(t, ctBuild(
		ctTyped("u1", "", "one"),
		ctSays("a1", "u1", "m1", ctText("Reply?")),
		ctTyped("u2", "a1", "two"),
		ctTyped("u3", "u2", "three"),
		ctSays("a2", "u3", "m2", ctToolUse("q1", "AskUserQuestion")),
		ctToolResult("r1", "a2", "q1", ctAskResult([]string{"Q?"}, map[string]string{"Q?": "A"}), false),
		ctTyped("u4", "r1", "four"),
	),
		ctBrief{"u1", "typed", "one", "", false},
		ctBrief{"u2", "typed", "two", "Reply?", true},
		ctBrief{"u3", "typed", "three", "", false},
		ctBrief{"r1", "answer", "Q? → A", "Q?", true},
		ctBrief{"u4", "typed", "four", "", false},
	)
}

// Messages from peers, the SDK and the harness are not turns, whatever
// marks they carry.
func TestClaudeTranscriptNotHuman(t *testing.T) {
	recs := []ctRec{
		ctWith(ctPlain("p1", "", ctNew, "<teammate-message from=\"x\">hi</teammate-message>"), "origin", map[string]any{"kind": "peer"}, "promptSource", "system"),
		ctPlain("p2", "", ctOld, "<agent-message from=\"x\">hi</agent-message>"),
		ctPlain("p3", "", ctOld, "Another Claude session sent a message: hi"),
		ctWith(ctTyped("p4", "", "an SDK prompt"), "entrypoint", "sdk-cli"),
		ctWith(ctPlain("p5", "", ctNew, "sdk"), "promptSource", "sdk"),
		ctWith(ctPlain("p6", "", ctNew, "continue"), "origin", map[string]any{"kind": "auto-continuation"}, "isMeta", true),
		ctWith(ctTyped("p7", "", "sidechain"), "isSidechain", true),
		ctWith(ctTyped("p8", "", "meta"), "isMeta", true),
		ctWith(ctPlain("p9", "", ctNew, "notify"), "origin", map[string]any{"kind": "task-notification"}),
	}
	for _, p := range []string{"<bash-input>ls</bash-input>", "<bash-stdout>x</bash-stdout>", "<bash-stderr>x</bash-stderr>",
		"<system-reminder>x</system-reminder>", "[Request interrupted by user]", "  /compact keep it short"} {
		recs = append(recs, ctPlain("x-"+p, "", ctOld, p))
	}
	ctExpect(t, ctBuild(recs...))
}

// The first record of a uuid wins; a torn last line is skipped quietly,
// and so is a garbled line in the middle.
func TestClaudeTranscriptDuplicatesAndTornLines(t *testing.T) {
	body := ctBuild(
		ctTyped("u1", "", "first copy"),
		ctTyped("u1", "", "second copy"),
	)
	body += "{not json\n"
	body += ctBuild(ctTyped("u2", "u1", "kept"))
	torn := ctBuild(ctTyped("u3", "u2", "torn"))
	body += torn[:len(torn)/2]
	ctExpect(t, body,
		ctBrief{"u1", "typed", "first copy", "", false},
		ctBrief{"u2", "typed", "kept", "", false},
	)
}

// A record is read whole however long its line is.
func TestClaudeTranscriptLongLines(t *testing.T) {
	big := strings.Repeat("y", 3<<20)
	turns := ctRead(t, ctBuild(
		ctTyped("u1", "", "go"),
		ctSays("a1", "u1", "m1", ctText("Reading?")),
		ctSays("a2", "a1", "m1", ctToolUse("t1", "Read")),
		ctWith(ctToolResult("r1", "a2", "t1", map[string]any{"file": map[string]any{"content": big}}, false)),
		ctTyped("u2", "r1", strings.Repeat("z", 2<<20)),
	))
	if len(turns) != 2 || len(turns[1].Text) != 2<<20 || turns[1].Asked != "Reading?" {
		t.Fatalf("got %d turns", len(turns))
	}
}

// A paste Claude Code wrapped in <pasted_content> tags reads back as it
// was sent, typed or absorbed mid-turn.
func TestClaudeTranscriptPastedContent(t *testing.T) {
	human := map[string]any{"kind": "human"}
	ctExpect(t, ctBuild(
		ctTyped("u1", "", "look at\n<pasted_content id=\"1\">line one\nline two</pasted_content>\nplease"),
		ctSays("a1", "u1", "m1", ctText("Looking.")),
		ctQueued("q1", "a1", map[string]any{"commandMode": "prompt", "prompt": "and <pasted_content>this</pasted_content>", "origin": human}),
	),
		ctBrief{"u1", "typed", "look at\nline one\nline two\nplease", "", false},
		ctBrief{"q1", "absorbed", "and this", "Looking.", false},
	)
}

// A huge assistant text gives the same asked and reply_to as its tail: the
// reader keeps only the end of each.
func TestClaudeTranscriptHugeAssistantText(t *testing.T) {
	head := strings.Repeat("words and more words. ", 10<<20/22)
	tail := "é" + strings.Repeat("b", 1500) + "\nWhich one?\n\n"
	turns := ctRead(t, ctBuild(
		ctTyped("u1", "", "go"),
		ctSays("a1", "u1", "m1", ctText(head)),
		ctSays("a2", "a1", "m1", ctText(tail)),
		ctTyped("u2", "a2", "the first"),
	))
	want := lastRunes(strings.TrimSpace(head+"\n"+tail), claudeAskedMax)
	if len(turns) != 2 || turns[1].Asked != want || !turns[1].ReplyTo {
		t.Fatalf("asked %d bytes, reply_to %v", len(turns[1].Asked), turns[1].ReplyTo)
	}
}

// What was asked keeps its last 1000 runes, where the question is, and is
// never cut inside one; reply_to reads the whole text's last paragraph.
func TestClaudeTranscriptAskedCutAndReplyTo(t *testing.T) {
	long := "the head goes éé" + strings.Repeat("a", 998) + "?"
	turns := ctRead(t, ctBuild(
		ctTyped("u0", "", "start"),
		ctSays("a1", "u0", "m1", ctText(long)),
		ctTyped("u1", "a1", "one"),
		ctSays("a2", "u1", "m2", ctText("Is it?\n\n  \n")),
		ctTyped("u2", "a2", "two"),
		ctSays("a3", "u2", "m3", ctText("Is it? No.")),
		ctTyped("u3", "a3", "three"),
		ctSays("a4", "u3", "m4", ctText("Why?\n\nBecause.")),
		ctTyped("u4", "a4", "four"),
	))
	if got := turns[1].Asked; got != "é"+strings.Repeat("a", 998)+"?" {
		t.Fatalf("cut: %d bytes, starts %q", len(got), got[:4])
	}
	var reply []bool
	for _, tr := range turns {
		reply = append(reply, tr.ReplyTo)
	}
	if want := []bool{false, true, true, true, false}; !reflect.DeepEqual(reply, want) {
		t.Fatalf("reply_to %v, want %v", reply, want)
	}
	if turns[2].Asked != "Is it?" {
		t.Fatalf("asked %q", turns[2].Asked)
	}
}

// A message replies when the last paragraph of what the agent said, the
// text after its last blank line, holds a question mark anywhere.
func TestAsksSomething(t *testing.T) {
	for _, c := range []struct {
		name, text string
		want       bool
	}{
		{"at the end", "I read the code.\n\nShall I fix the parser?", true},
		{"mid last paragraph", "Done.\n\nShall I delete the copies, or keep them on disk? My default is to keep them.", true},
		{"only in an earlier paragraph", "Which file?\n\nI picked main.go and fixed it.", false},
		{"none", "Fixed it.\n\nAll tests pass.", false},
		{"empty", "", false},
		{"blank only", " \n\t\n", false},
		{"trailing whitespace", "Done.\n\nShip it?  \n \n\t", true},
		{"a list in the last paragraph", "Summary:\n\n- keep the flag?\n- drop the tests", true},
		{"a list after the question", "Keep the flag?\n\n- tests pass\n- docs updated", false},
		{"a line of spaces splits paragraphs", "Keep it?\n   \nDone.", false},
		{"crlf", "Done.\r\n\r\nKeep it? Yes.\r\n", true},
	} {
		if got := asksSomething(c.text); got != c.want {
			t.Errorf("%s: asksSomething(%q) = %v, want %v", c.name, c.text, got, c.want)
		}
	}
}

// Every turn carries its record's folder, session and version.
func TestClaudeTranscriptCarriesRecordFields(t *testing.T) {
	turns := ctRead(t, ctBuild(
		ctWith(ctTyped("u1", "", "here"), "cwd", "/work/other", "sessionId", "s-other", "version", "2.1.290"),
	))
	want := claudeTurn{UUID: "u1", Kind: "typed", Time: time.Date(2026, 3, 1, 10, 0, 1, 0, time.UTC),
		Text: "here", Cwd: "/work/other", SessionID: "s-other", Version: "2.1.290"}
	if len(turns) != 1 || !reflect.DeepEqual(turns[0], want) {
		t.Fatalf("got %+v", turns)
	}
}

// The fixture files: whole synthetic sessions, read end to end.
func TestClaudeTranscriptFixtures(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "claude", "transcripts", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	turns, err := readClaudeTranscript(f)
	if err != nil {
		t.Fatal(err)
	}
	want := []ctBrief{
		{"10000000-0000-4000-8000-000000000001", "typed", "List the files in the demo folder", "", false},
		{"10000000-0000-4000-8000-000000000005", "typed", "yes, the big one", "There are three files. Want me to open the biggest?", true},
		{"10000000-0000-4000-8000-000000000008", "absorbed", "and count its lines", "Opening it now.", false},
		{"10000000-0000-4000-8000-000000000011", "answer", "Which format? → Plain text", "Which format?", true},
		{"10000000-0000-4000-8000-000000000013", "slash", "/review the demo", "It has 42 lines.", false},
	}
	if got := ctBriefs(turns); !reflect.DeepEqual(got, want) {
		t.Fatalf("turns:\n got %+v\nwant %+v", got, want)
	}
	for _, tr := range turns {
		if tr.SessionID != "00000000-0000-4000-8000-0000000000aa" || tr.Cwd != "/work/demo" || tr.Time.IsZero() {
			t.Fatalf("fields: %+v", tr)
		}
	}
}

// The long cases' hashes come from Claude Code's own function, run in
// node: abs of a 32-bit (h<<5)-h+c over the path's UTF-16 units, base 36.
func TestClaudeSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/Users/me/repos/demo", "-Users-me-repos-demo"},
		{"/work/my.project_v2 copy", "-work-my-project-v2-copy"},
		{"/Work/UPPER/MiXeD", "-Work-UPPER-MiXeD"},
		{"/work/with\ttab", "-work-with-tab"},
		{"/tmp/é", "-tmp--"},
		{"/tmp/😀", "-tmp---"}, // two UTF-16 units
		{"", ""},
		{strings.Repeat("a", 200), strings.Repeat("a", 200)},
		{strings.Repeat("a", 201), strings.Repeat("a", 200) + "-rkvsv5"},
		{"/" + strings.Repeat("abcdefghij", 25), "-" + strings.Repeat("abcdefghij", 20)[:199] + "-w4aff0"},
		{"/work/" + strings.Repeat("é", 300), "-work-" + strings.Repeat("-", 194) + "-uv5x9r"},
		{"/Users/demo/" + strings.Repeat("x", 200), "-Users-demo-" + strings.Repeat("x", 188) + "-rj52a2"}, // a negative hash
	}
	for _, c := range cases {
		if got := claudeSlug(c.in); got != c.want {
			t.Errorf("claudeSlug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormaliseSent(t *testing.T) {
	cases := map[string]string{
		"  a\tb  ":           "a b",
		"line one\n\nline 2": "line one line 2",
		"x\r\n\ty":           "x y",
		"":                   "",
		" \n\t ":             "",
		// A paste as the transcript wraps it equals the paste inline.
		"look at\n<pasted_content id=\"1\">a\n\tb</pasted_content> please": "look at a b please",
		"<pasted_content>x</pasted_content>":                               "x",
		"<pasted_contents>kept</pasted_contents>":                          "<pasted_contents>kept</pasted_contents>",
	}
	for in, want := range cases {
		if got := normaliseSent(in); got != want {
			t.Errorf("normaliseSent(%q) = %q, want %q", in, got, want)
		}
	}
}

// Two config dirs sharing one projects folder through a symlink count
// once; a dir with only history.jsonl is kept; a missing dir is dropped.
func TestClaudeConfigDirs(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	shared := filepath.Join(root, "shared-projects")
	a := filepath.Join(root, "cfg-a")
	b := filepath.Join(root, "cfg-b")
	h := filepath.Join(root, "cfg-history")
	for _, d := range []string{home, shared, a, b, h, filepath.Join(home, ".claude", "projects")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{a, b} {
		if err := os.Symlink(shared, filepath.Join(d, "projects")); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(h, "history.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", b)

	got := claudeConfigDirs([]string{a, h, h, filepath.Join(root, "missing"), ""})
	want := []string{a, h, filepath.Join(home, ".claude")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got := claudeConfigDirs(nil); !reflect.DeepEqual(got, []string{filepath.Join(home, ".claude")}) {
		t.Fatalf("defaults: %v", got)
	}
}

func TestClaudeVersionBefore(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"2.1.182", true}, {"2.1.183", false}, {"2.1.184", false}, {"2.0.999", true},
		{"2.2", false}, {"2.1", true}, {"3", false}, {"", true}, {"x.y", true}, {"2.1.183-beta", false},
		{"1.0.0", true}, {"2.1.1830", false},
	}
	for _, c := range cases {
		if got := claudeVersionBefore(c.v, claudeTypedSince); got != c.want {
			t.Errorf("claudeVersionBefore(%q) = %v, want %v", c.v, got, c.want)
		}
	}
}

// A read that fails is an error, unlike a torn line.
func TestClaudeTranscriptReadError(t *testing.T) {
	r := io.MultiReader(strings.NewReader(ctBuild(ctTyped("u1", "", "one"))), iotest.ErrReader(errors.New("disk gone")))
	if _, err := readClaudeTranscript(r); err == nil || err.Error() != "disk gone" {
		t.Fatalf("err %v", err)
	}
}
