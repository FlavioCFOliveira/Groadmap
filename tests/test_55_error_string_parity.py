#!/usr/bin/env python3
"""
Test 55: SPEC/COMMANDS.md published error strings vs. what the binary prints
(rmp task #277).

SPEC/COMMANDS.md § "Published Error Strings Are Exact" (line ~44) states the
convention: every error string the file publishes is the COMPLETE line the
user reads on stderr -- the `Error: ` prefix and the sentinel included, never
a message body alone and never a paraphrase. Nothing in the project detected
a violation of that convention until now: an earlier sweep found 121 distinct
published strings that were not what the binary printed (a quoted message body
with the sentinel silently dropped), all fixed by hand against captured output
from real invocations. Hand reconciliation is a one-time fix; nothing stopped
the next edit from drifting again. This module is the gate: it extracts every
string the file publishes, drives the compiled binary to reach as many of them
as it can in a throwaway roadmap, and compares captured stderr against the
published string CHARACTER FOR CHARACTER after placeholder substitution.

Extraction (see extract_table_corpus / extract_fenced_corpus below) recognises
two structural loci the file itself uses to publish a string:

  1. Markdown tables: a data row whose column count matches its own separator
     row, scanning EVERY cell (not just the last -- some tables carry the
     message in a middle column, with the exit code last) for a backtick- or
     double-quote-delimited span containing "Error:". A double-quoted span
     unescapes `\\"` to `"`; a backtick span whose content is itself a
     double-quoted string (`` `"..."` ``) unwraps the outer literal quotes,
     because those quotes are the author's markup, not stderr's own.
  2. Fenced code blocks: a bare line containing "Error:".

A handful of genuine, distinct published strings live only in prose (not in
any table or fence) -- verified by sweeping every remaining "Error:" line
in the file and checking whether its quoted content already appears in the
table/fence corpus. SUPPLEMENTAL_CORPUS lists exactly those, each pinned to
its line number with a runtime assertion that the source text has not moved
out from under it. Three prose spans are deliberately NOT promoted: they
restate a rule already published concretely by a table row (the numbered
list under "Messages this rule governs", COMMANDS.md:218-232) rather than
naming a new condition; EXCLUDED_TEMPLATE_LINES documents each one.

Placeholders are the closed set COMMANDS.md:54-66 publishes: X, N, M, Y,
<field>, <flag>, <detail>, <engine diagnostic>, and
<absolute path of ~/.roadmaps>; X/N/M/Y count only as whole words, and two
messages print literal `<name>`/`<id>` that are NOT placeholders (the module
proves both directions -- see test_placeholder_rule_both_directions below).
Because this module CHOOSES the offending value for every X/N/M/Y it drives,
substitution is exact string replacement against a value fixed before the
command runs, and the comparison is full string equality against the whole
published line -- never a regex, never a prefix-only check -- except for the
two kinds of narrowing named below. Both are declared by name with a reason,
counted, and reported; nothing is ever silently dropped.

  * EXEMPT_KEYS: the string is not driven at all, because no deterministic
    trigger for it exists from the CLI in a hermetic environment.
  * TAIL_EXEMPT_KEYS: the string IS driven against the binary, and the
    comparison is exact up to the named placeholder and stops there. Only the
    tail -- text produced by something outside this module (a SQLite
    diagnostic, an OS diagnostic) -- goes unasserted; the head, which carries
    the "Error: " prefix and the sentinel that determines the exit code, is
    compared character for character, and the tail is still required to be
    non-empty and to carry the operation context the wrap must preserve
    (ARCHITECTURE.md § Wrapping Rules, rule 2). See check_head.

The database-failure row of the four comment subcommands
(`Error: database error: <detail>`) was a full EXEMPT_KEYS entry until rmp
task #319. The reason recorded there -- that no hermetic trigger existed --
was wrong in one direction and right in the other: dropping the comment table
out of THIS module's own throwaway project.db, which is a file it created and
deletes, is perfectly hermetic and touches no shared infrastructure, so the
row is now driven; only the SQLite diagnostic in the tail stays unasserted.
That mistake is why the defect #319 fixed went unnoticed -- the binary printed
no sentinel at all on those six rows and this gate reported green over them.

The module's own final test method (test_zz_coverage_report, alphabetically
last so it runs after every other check has had the chance to mark its key
"reached") asserts that CORPUS is fully accounted for: every key is either in
REACHED or in EXEMPT_KEYS, with nothing left over. A published string this
module does not know how to reach is a bug in this module, not a silent gap.
"""

import json
import os
import re
import sqlite3
import subprocess
import sys
import uuid

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase, REPO_ROOT, COMMIT_OPEN_HASH, COMMIT_CLOSE_HASH


SPEC_PATH = REPO_ROOT / "SPEC" / "COMMANDS.md"

# A distinctive prefix so every roadmap this module creates is unmistakably
# its own -- never confused with another module's fixture, and never the
# pre-existing `audit-e2e-probe` roadmap that a past agent left behind and
# that this module must not touch.
ROADMAP_PREFIX = "qa_errata_gate_"


# ---------------------------------------------------------------------------
# Extraction: table cells and fenced blocks
# ---------------------------------------------------------------------------

_BACKTICK_RE = re.compile(r"`([^`]*)`")
_DQUOTE_RE = re.compile(r'"((?:[^"\\]|\\.)*)"')
_SEP_RE = re.compile(r"^\s*:?-+:?\s*$")


def _split_table_row(line):
    """Split one markdown table row into cells, honouring backtick spans (a
    `|` inside backticks is not a cell separator) and the `\\|` escape."""
    cells = []
    cur = []
    in_backtick = False
    i, n = 0, len(line)
    while i < n:
        c = line[i]
        if c == "\\" and i + 1 < n and line[i + 1] == "|":
            cur.append("|")
            i += 2
            continue
        if c == "`":
            in_backtick = not in_backtick
            cur.append(c)
            i += 1
            continue
        if c == "|" and not in_backtick:
            cells.append("".join(cur))
            cur = []
            i += 1
            continue
        cur.append(c)
        i += 1
    cells.append("".join(cur))
    return cells


def _is_row(line):
    s = line.strip()
    return s.startswith("|") and s.endswith("|") and len(s) >= 2


def _is_separator_row(cells):
    inner = cells[1:-1]
    return bool(inner) and all(_SEP_RE.match(c) for c in inner)


def _extract_error_spans(text):
    """Yield every quoted span of `text` that contains "Error:", unescaped
    and with backtick-wrapped-double-quote nesting unwrapped (see module
    docstring, extraction rule 1)."""
    for m in _DQUOTE_RE.finditer(text):
        if "Error:" in m.group(1):
            yield m.group(1).replace('\\"', '"')
    for m in _BACKTICK_RE.finditer(text):
        content = m.group(1)
        if "Error:" not in content:
            continue
        stripped = content.strip()
        if len(stripped) >= 2 and stripped[0] == '"' and stripped[-1] == '"':
            yield stripped[1:-1].replace('\\"', '"')
        else:
            yield content


def extract_table_corpus(text):
    """Return {published_string: [1-based line numbers]} for every quoted
    Error: span found in ANY cell of a data row whose column count matches
    its own separator row (extraction rule 1). This is the primary,
    structural locus SPEC/COMMANDS.md uses to publish an error string."""
    lines = text.split("\n")
    corpus = {}
    i, n = 0, len(lines)
    while i < n:
        line = lines[i]
        if _is_row(line) and i + 1 < n and _is_row(lines[i + 1]):
            sep_cells = _split_table_row(lines[i + 1])
            cells = _split_table_row(line)
            if _is_separator_row(sep_cells) and len(sep_cells) == len(cells):
                ncols = len(cells)
                j = i + 2
                while j < n and _is_row(lines[j]):
                    row_cells = _split_table_row(lines[j])
                    if len(row_cells) == ncols:
                        content_cells = row_cells[1:-1] if len(row_cells) >= 2 else row_cells
                        for cell in content_cells:
                            for s in _extract_error_spans(cell):
                                corpus.setdefault(s.strip(), []).append(j + 1)
                    j += 1
                i = j
                continue
        i += 1
    return corpus


def extract_fenced_corpus(text):
    """Return {published_string: [1-based line numbers]} for every bare line
    containing "Error:" inside a fenced (``` ```) code block (extraction
    rule 2)."""
    corpus = {}
    in_fence = False
    for idx, line in enumerate(text.split("\n")):
        if line.strip().startswith("```"):
            in_fence = not in_fence
            continue
        if in_fence and "Error:" in line:
            corpus.setdefault(line.strip(), []).append(idx + 1)
    return corpus


# A handful of genuine published strings live only in prose: verified by hand
# (see module docstring) by sweeping every "Error:"-bearing line outside a
# table row or a fence and checking whether its quoted content was already in
# the table/fence corpus. Each entry is pinned to the exact line and exact
# substring so a future edit that moves or rewords the sentence fails this
# assertion loudly instead of silently going stale.
# (anchor, published_string): `anchor` is a literal substring searched for
# DYNAMICALLY in the current file text (never a hardcoded line number, which
# a prose edit anywhere earlier in the document shifts) to locate the entry
# and report where it lives. For most entries the anchor IS the published
# string itself, since it appears verbatim in the surrounding prose; the
# assign/unassign pair is DERIVED from a generic X-template anchor (the
# derived text itself is not a verbatim quote, by construction -- see the
# comment below).
SUPPLEMENTAL_CORPUS = [
    (
        'Error: ai-help accepts no positional arguments or flags other than --help',
        'Error: ai-help accepts no positional arguments or flags other than --help',
    ),
    (
        'Error: required parameter missing: at least one of --title, --description, --max-tasks or --order is required',
        'Error: required parameter missing: at least one of --title, --description, --max-tasks or --order is required',
    ),
    ('Error: unknown task subcommand: delete', 'Error: unknown task subcommand: delete'),
    ('Error: unknown sprint subcommand: delete', 'Error: unknown sprint subcommand: delete'),
    # COMMANDS.md names `rmp task assign` and `rmp task unassign`
    # concretely in the same sentence as the X-templated
    # "Error: unknown task subcommand: X" line, so X is instantiated with
    # both concrete values the prose itself names (rmp task #246 regression).
    # The anchor is the generic template, which DOES appear verbatim; the
    # derived strings themselves are not literal quotes in the file.
    ('Error: unknown task subcommand: X', 'Error: unknown task subcommand: assign'),
    ('Error: unknown task subcommand: X', 'Error: unknown task subcommand: unassign'),
    # A concrete instance of the -e/--entity-type template
    # ('Error: validation error: invalid entity type: "X"'), naming the exact
    # literal value (-e) the prose describes.
    ('Error: validation error: invalid entity type: "-e"', 'Error: validation error: invalid entity type: "-e"'),
]

# Three prose spans that ARE complete, well-formed "Error:" strings but are
# deliberately NOT promoted into the corpus: COMMANDS.md:218-232, under the
# heading "Messages this rule governs", restates -- as a numbered list, using
# the same <field> placeholder -- the three message templates that
# `Published Field Names in Validation Messages` already governs, and two of
# the three (control-character and UTF-8) are ALSO published verbatim by
# table rows 477 and 476. The third (length-cap, line 224) has no verbatim
# table counterpart in generic <field>/N form, but every field the length cap
# governs is already tested concretely via the CORPUS entries at lines
# 469/472/473/474/475 (title/functional_requirements/technical_requirements/
# acceptance_criteria/description/completion_summary/body, each with its own
# concrete number) -- so the generic template adds no new condition to drive
# against the binary, only a restatement of one already covered. Line numbers
# are of the "Messages this rule governs" bullets themselves.
EXCLUDED_TEMPLATE_LINES = {
    223: "control-character rule statement, concretely published at table row 477",
    225: "length-cap rule statement, concretely published at table rows 469/472/473/474/475",
    328: "UTF-8 rule statement, concretely published at table row 476",
}


def build_corpus():
    """Build the full {published_string: [line numbers]} corpus this module
    tests against, and validate SUPPLEMENTAL_CORPUS is still anchored to the
    text it was read from."""
    text = SPEC_PATH.read_text(encoding="utf-8")
    lines = text.split("\n")

    corpus = extract_table_corpus(text)
    for s, line_nos in extract_fenced_corpus(text).items():
        corpus.setdefault(s, []).extend(line_nos)

    for anchor, s in SUPPLEMENTAL_CORPUS:
        lineno = next((i + 1 for i, line in enumerate(lines) if anchor in line), None)
        assert lineno is not None, (
            f"SUPPLEMENTAL_CORPUS anchor no longer found anywhere in "
            f"SPEC/COMMANDS.md: {anchor!r} -- the prose moved, was reworded, "
            f"or was removed, and this entry must be re-anchored or dropped."
        )
        corpus.setdefault(s, []).append(lineno)

    return {k: sorted(set(v)) for k, v in corpus.items()}


CORPUS = build_corpus()


# ---------------------------------------------------------------------------
# Exemptions: published strings this module cannot deterministically drive
# to a full character-for-character match. Each is named and reasoned here,
# never silently dropped -- test_zz_coverage_report prints this table and
# fails if CORPUS ever grows a key that is neither reached nor exempted.
# ---------------------------------------------------------------------------

EXEMPT_KEYS = {
    "Error: database error: graph query failed: <engine diagnostic>": (
        "internal/commands/graph.go: the tail is text the Cypher engine "
        "itself produces for a parse/execution failure and is not "
        "specified by COMMANDS.md:3261 (\"what follows is the engine's own "
        "text and is not specified here\")."
    ),
    "Error: database error: graph store unavailable: <detail>": (
        "Derived from internal/commands/graph.go:847,917,1023 and "
        "internal/web/data.go:1978: an internal graph-store open/read/write "
        "failure, not reachable through ordinary CLI execution against a "
        "healthy filesystem."
    ),
    "Error: database error: cannot bind 127.0.0.1:8787: listen tcp 127.0.0.1:8787: bind: address already in use": (
        "internal/web: the tail after \"cannot bind 127.0.0.1:8787: \" is "
        "the Go standard library's net.OpError rendering, platform-"
        "dependent per COMMANDS.md:3369; not assertable as a fixed literal."
    ),
    "Error: database error: cannot bind 10.0.0.5:8787: listen tcp 10.0.0.5:8787: bind: cannot assign requested address": (
        "internal/web: same platform-dependent net.OpError tail as the "
        "row above, and binding a specific non-loopback, non-local address "
        "like 10.0.0.5 is itself environment-dependent (routable only on "
        "hosts that do not own that address)."
    ),
}


# Strings this module DOES drive against the binary, but whose comparison
# stops at the named placeholder because the text after it is produced by
# something this module does not control. The head -- everything before the
# placeholder, which is where the "Error: " prefix and the sentinel live -- is
# still compared character for character, and check_head additionally requires
# the tail to be non-empty and to carry the operation context. This is a
# NARROWING, not an exemption: the key is marked reached, and the sentinel
# half is asserted rather than skipped.
TAIL_EXEMPT_KEYS = {
    "Error: database error: <detail>": (
        "<detail>",
        "The six database-failure rows of the comment subcommands (three in "
        "the task family, three in the sprint family). The head "
        "\"Error: database error: \" is asserted exactly, and so is the "
        "operation context that follows it (\"writing task comment: \", "
        "\"querying task comment N: \", ...). What is left unasserted is only "
        "the SQLite driver's own diagnostic at the end -- for a dropped "
        "table, \"SQL logic error: no such table: task_comments (1)\" -- whose "
        "exact wording belongs to modernc.org/sqlite and is not specified by "
        "COMMANDS.md."
    ),
}


# X, N, M, Y count only as WHOLE WORDS (COMMANDS.md:54-66): a bare letter
# token must not match the same letter sitting inside an unrelated word --
# "TESTING" contains an "N", and a naive substring replace of the token "N"
# would corrupt it into "TESTI<value>G". Bracketed tokens (<field>, <flag>,
# <detail>, <engine diagnostic>, <absolute path of ~/.roadmaps>) contain
# non-word characters and carry no such risk, so they are replaced as plain
# substrings.
_WORD_TOKENS = frozenset({"X", "N", "M", "Y"})


def subst(template, subs):
    """Substitute every placeholder token in `subs` into `template` with the
    concrete value THIS MODULE chose before running the command. Because
    every X/N/M/Y/<field>/<flag>/<absolute path...> value is fixed in
    advance (never read back from the captured output), this is exact
    substitution against a value fixed before the command runs, and the
    resulting comparison is full string equality -- never a regex, never a
    prefix check -- but the substitution ITSELF uses a word-boundary regex
    for the single-letter tokens so it cannot corrupt an unrelated word that
    happens to contain the same letter (see _WORD_TOKENS)."""
    expected = template
    for token, value in subs.items():
        if token in _WORD_TOKENS:
            pattern = re.compile(r"\b" + re.escape(token) + r"\b")
            assert pattern.search(expected), (
                f"substitution token {token!r} does not appear as a whole "
                f"word in template {template!r}"
            )
            expected = pattern.sub(lambda m, v=value: v, expected)
        else:
            assert token in expected, (
                f"substitution token {token!r} does not appear in template {template!r}"
            )
            expected = expected.replace(token, value)
    return expected


REACHED = set()


class TestErrorStringParity:
    """Drives the compiled binary in a throwaway roadmap and compares
    captured stderr against SPEC/COMMANDS.md's published strings, character
    for character, after this module's own chosen placeholder values are
    substituted in."""

    # ------------------------------------------------------------------
    # Fixture plumbing
    # ------------------------------------------------------------------

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = f"{ROADMAP_PREFIX}{uuid.uuid4().hex[:10]}"
        self.test.run_cmd(["roadmap", "create", self.roadmap])
        # An id no fixture in this roadmap will ever hold, for every
        # "resource not found" case that needs one.
        self.missing_id = 900000001

    def teardown_method(self):
        self.test.teardown()

    # ------------------------------------------------------------------
    # Invocation helpers
    # ------------------------------------------------------------------

    def run_stdin(self, args, input_text=None):
        """Run the CLI with `input_text` (or a closed/empty stdin when None)
        piped in, returning (exit_code, stdout, stderr)."""
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        result = subprocess.run(
            [self.test.cli_path] + args,
            input=input_text if input_text is not None else "",
            capture_output=True,
            text=True,
            env=env,
        )
        return result.returncode, result.stdout, result.stderr

    def check(self, key, args, exit_code, subs=None, stdin=None, note=""):
        """The gate's core assertion: run `args` with stdin ALWAYS under this
        module's explicit control (empty by default -- never the inherited
        stdin of the process running the suite, which would make the result
        depend on the environment invoking it), and require BOTH the exit
        code and the first line of stderr to equal exactly what CORPUS[key]
        publishes once `subs` is substituted in. Marks `key` reached."""
        assert key in CORPUS, (
            f"case references a string extraction no longer finds in "
            f"SPEC/COMMANDS.md: {key!r} ({note})"
        )
        expected = subst(key, subs or {})
        rc, out, err = self.run_stdin(args, stdin)
        actual_line = err.splitlines()[0] if err else ""
        assert rc == exit_code, (
            f"[{note or key}] exit code: expected {exit_code}, got {rc}\n"
            f"  args: {args}\n  stdout: {out!r}\n  stderr: {err!r}"
        )
        assert actual_line == expected, (
            f"[{note or key}] published string does not match the binary\n"
            f"  file:     {SPEC_PATH} line(s) {CORPUS[key]}\n"
            f"  published: {expected!r}\n"
            f"  captured:  {actual_line!r}\n"
            f"  args:      {args}\n"
            f"  full stderr: {err!r}"
        )
        assert out == "", (
            f"[{note or key}] a failing invocation wrote to stdout: {out!r}"
        )
        REACHED.add(key)

    def check_head(self, key, args, exit_code, tail_contains, subs=None,
                   stdin=None, note=""):
        """The narrowed form of `check`, for the keys TAIL_EXEMPT_KEYS names.

        Everything BEFORE the named placeholder is compared character for
        character against captured stderr, exactly as `check` does -- so the
        "Error: " prefix and the sentinel that determines the exit code are
        asserted, never skipped. Only the text after the placeholder is
        outside this module's control, and even that is not waved through:
        it must be non-empty, and it must contain every fragment
        `tail_contains` names (the operation context the wrap is required to
        preserve, per ARCHITECTURE.md § Wrapping Rules rule 2).

        `key` must be declared in TAIL_EXEMPT_KEYS: a string is never
        compared by head alone unless the narrowing is declared and reasoned
        there."""
        assert key in CORPUS, (
            f"case references a string extraction no longer finds in "
            f"SPEC/COMMANDS.md: {key!r} ({note})"
        )
        assert key in TAIL_EXEMPT_KEYS, (
            f"check_head used on a string that declares no tail narrowing: "
            f"{key!r} -- add it to TAIL_EXEMPT_KEYS with its reason, or use "
            f"check() for a full comparison"
        )
        token = TAIL_EXEMPT_KEYS[key][0]
        head, sep, published_tail = subst(key, subs or {}).partition(token)
        assert sep, (
            f"TAIL_EXEMPT_KEYS declares placeholder {token!r} for {key!r}, "
            f"but the published string does not contain it"
        )
        assert published_tail == "", (
            f"check_head only supports a placeholder that ends the published "
            f"string; {key!r} carries {published_tail!r} after {token!r}"
        )
        assert head.startswith("Error: ") and len(head) > len("Error: "), (
            f"the asserted head of {key!r} is {head!r}, which carries no "
            f"sentinel -- narrowing it would assert nothing of substance"
        )

        rc, out, err = self.run_stdin(args, stdin)
        actual_line = err.splitlines()[0] if err else ""
        assert rc == exit_code, (
            f"[{note or key}] exit code: expected {exit_code}, got {rc}\n"
            f"  args: {args}\n  stdout: {out!r}\n  stderr: {err!r}"
        )
        assert actual_line.startswith(head), (
            f"[{note or key}] the published head does not match the binary\n"
            f"  file:      {SPEC_PATH} line(s) {CORPUS[key]}\n"
            f"  published: {head!r} (then {token})\n"
            f"  captured:  {actual_line!r}\n"
            f"  args:      {args}\n  full stderr: {err!r}"
        )
        actual_tail = actual_line[len(head):]
        assert actual_tail, (
            f"[{note or key}] the binary printed the head and nothing for "
            f"{token}: {actual_line!r}"
        )
        for fragment in tail_contains:
            assert fragment in actual_tail, (
                f"[{note or key}] the {token} tail lost the operation "
                f"context: expected {fragment!r} in {actual_tail!r}"
            )
        assert out == "", (
            f"[{note or key}] a failing invocation wrote to stdout: {out!r}"
        )
        REACHED.add(key)

    # ------------------------------------------------------------------
    # Realistic fixture builders (no foo/bar/test1 placeholders)
    # ------------------------------------------------------------------

    def mk_task(self, title, fr, tr, ac, **kw):
        return self.test.create_task(self.roadmap, title, fr, tr, ac, **kw)

    def mk_sprint(self, title, desc, extra=None):
        cmd = ["sprint", "create", "-r", self.roadmap, "-t", title, "-d", desc]
        if extra:
            cmd.extend(extra)
        result = self.test.run_cmd_json(cmd)
        return result["id"]

    # ------------------------------------------------------------------
    # Dispatch failures and the ai-help contract
    # ------------------------------------------------------------------

    def test_dispatch_failures_and_aihelp(self):
        # #1: an unresolved top-level command.
        self.check(
            "Error: unknown command: nadadisto",
            ["nadadisto"], 127, note="unresolved command",
        )
        # #2: an unresolved subcommand of a family that DID resolve.
        self.check(
            "Error: unknown task subcommand: nadadisto",
            ["task", "nadadisto"], 127, note="unresolved task subcommand",
        )
        # S3/S4: the `delete` alias is scoped to `roadmap remove` only.
        self.check(
            "Error: unknown task subcommand: delete",
            ["task", "delete"], 127, note="task delete is not an alias",
        )
        self.check(
            "Error: unknown sprint subcommand: delete",
            ["sprint", "delete"], 127, note="sprint delete is not an alias",
        )
        # S5/S6: `assign`/`unassign` were removed with the specialists field
        # (rmp task #246) and dispatch as any other unresolved name.
        self.check(
            "Error: unknown task subcommand: assign",
            ["task", "assign"], 127, note="task assign no longer exists",
        )
        self.check(
            "Error: unknown task subcommand: unassign",
            ["task", "unassign"], 127, note="task unassign no longer exists",
        )
        # S1: `ai-help` takes no positional arguments and no flags but --help.
        self.check(
            "Error: ai-help accepts no positional arguments or flags other than --help",
            ["ai-help", "extra-positional"], 2, note="ai-help rejects a stray argument",
        )

    # ------------------------------------------------------------------
    # Roadmap name validation and roadmap create/remove
    # ------------------------------------------------------------------

    def test_roadmap_name_and_create_errors(self):
        # #17 invalid characters (checked AFTER reserved-name, per
        # internal/utils/path.go ValidateRoadmapName order).
        self.check(
            "Error: Roadmap name must only contain lowercase letters, numbers, underscores, and hyphens",
            ["roadmap", "create", "payments#gateway"], 6, note="invalid characters",
        )
        # #18 exceeds 50 characters.
        long_name = "n" * 51
        self.check(
            "Error: Roadmap name must not exceed 50 characters (got N)",
            ["roadmap", "create", long_name], 6, subs={"N": "51"}, note="name too long",
        )
        # #19 empty name.
        self.check(
            "Error: Roadmap name is required",
            ["roadmap", "create", ""], 6, note="empty name",
        )
        # #20 starts with a hyphen.
        self.check(
            "Error: validation error: roadmap name cannot start with '-'",
            ["roadmap", "create", "-payments"], 6, note="starts with hyphen",
        )
        # #21 reserved system name: WindowsReservedNames is checked
        # case-insensitively, and lowercase "con" still satisfies the
        # `^[a-z0-9_-]+$` charset, so this reaches the reserved check.
        self.check(
            'Error: validation error: "X": roadmap name is a reserved system name',
            ["roadmap", "create", "con"], 6, subs={"X": "con"}, note="reserved name",
        )
        # #22 already exists.
        existing = f"{ROADMAP_PREFIX}dup_{uuid.uuid4().hex[:8]}"
        self.test.run_cmd(["roadmap", "create", existing])
        self.check(
            'Error: resource already exists: roadmap "X" already exists',
            ["roadmap", "create", existing], 5, subs={"X": existing}, note="duplicate roadmap",
        )

    # ------------------------------------------------------------------
    # `task create` field validation
    # ------------------------------------------------------------------

    FR = "Reject webhook payloads whose HMAC signature does not match the shared secret"
    TR = "Verify the X-Signature header against a computed HMAC-SHA256 before parsing the body"
    AC = "An invalid signature returns HTTP 401 and the payload is never processed"

    def test_task_create_field_errors(self):
        r = self.roadmap
        # #7/#27: --title absent (generic <flag> template and its concrete
        # instance are the SAME captured invocation).
        args = ["task", "create", "-r", r, "-fr", self.FR, "-tr", self.TR, "-ac", self.AC]
        self.check("Error: required parameter missing: --<flag>", args, 2,
                    subs={"<flag>": "title"}, note="title flag absent (generic)")
        self.check("Error: required parameter missing: --title", args, 2,
                    note="title flag absent (concrete)")

        # #28: title supplied but empty once trimmed.
        args = ["task", "create", "-r", r, "-t", "   ", "-fr", self.FR, "-tr", self.TR, "-ac", self.AC]
        self.check("Error: validation error: title cannot be empty", args, 6,
                    note="title empty once trimmed")

        # #6: title exceeds 255 characters.
        long_title = "Migrate the settlement ledger to double-entry accounting " * 5
        assert len(long_title) > 255
        args = ["task", "create", "-r", r, "-t", long_title, "-fr", self.FR, "-tr", self.TR, "-ac", self.AC]
        self.check(
            "Error: field exceeds maximum size: title exceeds maximum length of 255 characters",
            args, 6, note="title too long",
        )

        # #29: --functional-requirements absent.
        args = ["task", "create", "-r", r, "-t", "Harden payment webhook signature verification",
                "-tr", self.TR, "-ac", self.AC]
        self.check("Error: required parameter missing: --functional-requirements", args, 2,
                    note="fr flag absent")

        # #30: functional_requirements empty once trimmed.
        args = ["task", "create", "-r", r, "-t", "Harden payment webhook signature verification",
                "-fr", "   ", "-tr", self.TR, "-ac", self.AC]
        self.check("Error: validation error: functional_requirements cannot be empty", args, 6,
                    note="fr empty once trimmed")

        # #8/#31: functional_requirements exceeds 4096 (generic <field> template
        # and its concrete instance share this one invocation).
        long_fr = "f" * 4097
        args = ["task", "create", "-r", r, "-t", "Harden payment webhook signature verification",
                "-fr", long_fr, "-tr", self.TR, "-ac", self.AC]
        self.check("Error: field exceeds maximum size: <field> exceeds maximum length of 4096 characters",
                    args, 6, subs={"<field>": "functional_requirements"}, note="fr too long (generic)")
        self.check(
            "Error: field exceeds maximum size: functional_requirements exceeds maximum length of 4096 characters",
            args, 6, note="fr too long (concrete)",
        )

        # #32: --technical-requirements absent.
        args = ["task", "create", "-r", r, "-t", "Harden payment webhook signature verification",
                "-fr", self.FR, "-ac", self.AC]
        self.check("Error: required parameter missing: --technical-requirements", args, 2,
                    note="tr flag absent")

        # #33: technical_requirements empty once trimmed.
        args = ["task", "create", "-r", r, "-t", "Harden payment webhook signature verification",
                "-fr", self.FR, "-tr", "   ", "-ac", self.AC]
        self.check("Error: validation error: technical_requirements cannot be empty", args, 6,
                    note="tr empty once trimmed")

        # #34: technical_requirements exceeds 4096.
        long_tr = "t" * 4097
        args = ["task", "create", "-r", r, "-t", "Harden payment webhook signature verification",
                "-fr", self.FR, "-tr", long_tr, "-ac", self.AC]
        self.check(
            "Error: field exceeds maximum size: technical_requirements exceeds maximum length of 4096 characters",
            args, 6, note="tr too long",
        )

        # #35: --acceptance-criteria absent.
        args = ["task", "create", "-r", r, "-t", "Harden payment webhook signature verification",
                "-fr", self.FR, "-tr", self.TR]
        self.check("Error: required parameter missing: --acceptance-criteria", args, 2,
                    note="ac flag absent")

        # #36: acceptance_criteria empty once trimmed.
        args = ["task", "create", "-r", r, "-t", "Harden payment webhook signature verification",
                "-fr", self.FR, "-tr", self.TR, "-ac", "   "]
        self.check("Error: validation error: acceptance_criteria cannot be empty", args, 6,
                    note="ac empty once trimmed")

        # #37: acceptance_criteria exceeds 4096.
        long_ac = "c" * 4097
        args = ["task", "create", "-r", r, "-t", "Harden payment webhook signature verification",
                "-fr", self.FR, "-tr", self.TR, "-ac", long_ac]
        self.check(
            "Error: field exceeds maximum size: acceptance_criteria exceeds maximum length of 4096 characters",
            args, 6, note="ac too long",
        )

        # #23: invalid --type on task create.
        args = ["task", "create", "-r", r, "-t", "Harden payment webhook signature verification",
                "-fr", self.FR, "-tr", self.TR, "-ac", self.AC, "-y", "SUPERBUG"]
        self.check('Error: validation error: invalid task type: "X"', args, 6,
                    subs={"X": "SUPERBUG"}, note="invalid task type on create")

        # #38: priority out of range on create.
        args = ["task", "create", "-r", r, "-t", "Harden payment webhook signature verification",
                "-fr", self.FR, "-tr", self.TR, "-ac", self.AC, "-p", "15"]
        self.check("Error: validation error: priority must be between 0 and 9, got N", args, 6,
                    subs={"N": "15"}, note="priority out of range on create")

        # #39: severity out of range on create.
        args = ["task", "create", "-r", r, "-t", "Harden payment webhook signature verification",
                "-fr", self.FR, "-tr", self.TR, "-ac", self.AC, "--severity", "12"]
        self.check("Error: validation error: severity must be between 0 and 9, got N", args, 6,
                    subs={"N": "12"}, note="severity out of range on create")

        # #40: --parent names a task that does not exist.
        args = ["task", "create", "-r", r, "-t", "Reconcile ledger entries against provider payout report",
                "-fr", self.FR, "-tr", self.TR, "-ac", self.AC, "--parent", str(self.missing_id)]
        self.check("Error: resource not found: parent task N not found", args, 4,
                    subs={"N": str(self.missing_id)}, note="parent task not found")

        # #12/#13 (generic <field> UTF-8 and control-character templates):
        # exercised on task edit below, where a single existing task is
        # cheaper to reuse across both invocations.

    # ------------------------------------------------------------------
    # `task list` filters
    # ------------------------------------------------------------------

    def test_task_list_filter_errors(self):
        r = self.roadmap
        # #23 (already reached above) shares its template with this generic
        # invalid-type check, so no separate REACHED bookkeeping is lost by
        # exercising it again through a different subcommand.
        self.check('Error: validation error: invalid task type: "X"',
                    ["task", "list", "-r", r, "-y", "SUPERBUG"], 6,
                    subs={"X": "SUPERBUG"}, note="task list invalid type")
        # #24: invalid --sort value.
        self.check(
            "Error: validation error: --sort must be one of: priority, created, status, severity",
            ["task", "list", "-r", r, "--sort", "urgency"], 6, note="task list invalid sort",
        )
        # #25: invalid --created-since format.
        self.check(
            'Error: validation error: --created-since: invalid date format: expected RFC3339 '
            '(2026-01-01T00:00:00Z) or date-only (2026-01-01): "X"',
            ["task", "list", "-r", r, "--created-since", "last-tuesday"], 6,
            subs={"X": "last-tuesday"}, note="task list invalid created-since",
        )
        # #26: invalid --created-until format.
        self.check(
            'Error: validation error: --created-until: invalid date format: expected RFC3339 '
            '(2026-01-01T00:00:00Z) or date-only (2026-01-01): "X"',
            ["task", "list", "-r", r, "--created-until", "next-friday"], 6,
            subs={"X": "next-friday"}, note="task list invalid created-until",
        )

    # ------------------------------------------------------------------
    # `task edit`: the generic <field> UTF-8 / control-character templates,
    # and the non-integer priority/severity misuse errors.
    # ------------------------------------------------------------------

    def test_task_edit_errors(self):
        r = self.roadmap
        task_id = self.mk_task(
            "Add structured logging correlation ID to checkout service",
            self.FR, self.TR, self.AC,
        )

        # #3: the generic <field> emptiness template (Field Validation's own
        # summary table), instantiated with functional_requirements. The
        # concrete per-command wording this produces ("functional_requirements
        # cannot be empty") is the SAME string `task create` already reaches;
        # this call additionally proves the generic template.
        self.check(
            "Error: validation error: <field> cannot be empty",
            ["task", "edit", "-r", r, str(task_id), "-fr", "   "], 6,
            subs={"<field>": "functional_requirements"}, note="task edit fr empty (generic template)",
        )

        # #12: <field> value is not valid UTF-8 (generic template). Encode
        # the argv entry as bytes carrying a lone continuation byte, which is
        # not well-formed UTF-8 on its own.
        bad_utf8 = b"Correlation ID propagation \xff\xfe broke across retries"
        argv = [self.test.cli_path, "task", "edit", "-r", r, str(task_id),
                "-fr"]
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        proc = subprocess.run(
            [a.encode() for a in argv] + [bad_utf8],
            capture_output=True, env=env,
        )
        rc, err = proc.returncode, proc.stderr.decode("utf-8", errors="replace")
        expected = subst(
            "Error: validation error: <field>: the value is not valid UTF-8",
            {"<field>": "functional_requirements"},
        )
        actual_line = err.splitlines()[0] if err else ""
        assert rc == 6, f"invalid-UTF-8 edit: expected exit 6, got {rc}; stderr={err!r}"
        assert actual_line == expected, (
            f"invalid-UTF-8 edit stderr mismatch\n  expected: {expected!r}\n  actual: {actual_line!r}"
        )
        REACHED.add("Error: validation error: <field>: the value is not valid UTF-8")

        # #13: <field> carries a forbidden control character (generic
        # template). A leading VT (0x0B) is a control character, not
        # whitespace the trim would silently remove first (see COMMANDS.md
        # § "This constraint does not move the control-character check").
        self.check(
            "Error: validation error: <field>: control characters are not allowed",
            ["task", "edit", "-r", r, str(task_id), "-fr", "\x0bCorrelation ID propagation broke across retries"],
            6, subs={"<field>": "functional_requirements"}, note="control char in fr",
        )

        # #60: --priority is not an integer.
        self.check(
            'Error: invalid input: invalid value for --priority: strconv.Atoi: parsing "X": invalid syntax',
            ["task", "edit", "-r", r, str(task_id), "-p", "urgent"], 2,
            subs={"X": "urgent"}, note="task edit non-integer priority",
        )
        # #61: --severity is not an integer.
        self.check(
            'Error: invalid input: invalid value for --severity: strconv.Atoi: parsing "X": invalid syntax',
            ["task", "edit", "-r", r, str(task_id), "--severity", "urgent"], 2,
            subs={"X": "urgent"}, note="task edit non-integer severity",
        )
        # #58 (task edit range form is the SAME message as task prio's,
        # captured under Change Priority below).

    # ------------------------------------------------------------------
    # `task get` batch behaviour
    # ------------------------------------------------------------------

    def test_task_get_batch_errors(self):
        r = self.roadmap
        task_id = self.mk_task(
            "Reconcile ledger entries against provider payout report",
            self.FR, self.TR, self.AC,
        )
        # #41: some IDs do not exist.
        self.check(
            "Error: resource not found: some tasks not found",
            ["task", "get", "-r", r, f"{task_id},{self.missing_id}"], 4,
            note="task get some ids missing",
        )
        # #42: an ID is not an integer.
        self.check(
            'Error: invalid input: invalid task ID: "X" (must be a positive integer)',
            ["task", "get", "-r", r, "abc"], 2, subs={"X": "abc"},
            note="task get non-integer id",
        )
        # #43: an ID is an integer but not positive.
        self.check(
            "Error: validation error: invalid task ID: N (must be positive)",
            ["task", "get", "-r", r, "0"], 6, subs={"N": "0"},
            note="task get zero id",
        )

    # ------------------------------------------------------------------
    # `task next`
    # ------------------------------------------------------------------

    def test_task_next_errors(self):
        r = self.roadmap
        # #44: no sprint is currently open (fresh roadmap, no sprint at all).
        self.check(
            "Error: resource not found: no sprint is currently open. "
            "Use 'rmp sprint start <id>' to open a sprint first",
            ["task", "next", "-r", r], 4, note="task next no open sprint",
        )
        # #45: invalid num argument. An open sprint removes any ambiguity
        # about which of the two checks (open sprint vs. num format) runs
        # first.
        task_id = self.mk_task(
            "Emit checkout latency histograms to the metrics pipeline",
            self.FR, self.TR, self.AC,
        )
        sprint_id = self.mk_sprint(
            "Observability hardening",
            "Give the checkout team the latency and error signals needed to "
            "catch a regression before customers report it.",
        )
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(sprint_id), str(task_id)])
        self.test.run_cmd(["sprint", "start", "-r", r, str(sprint_id)])
        self.check(
            "Error: validation error: num must be a positive integer",
            ["task", "next", "-r", r, "0"], 6, note="task next invalid num",
        )
        # #46: no roadmap selected (reached here; this is the fenced-block
        # string COMMANDS.md:623 publishes as the literal, non-templated
        # invocation example, reused by dozens of table rows).
        self.check(
            "Error: no roadmap selected: use -r <name> or --roadmap <name>",
            ["task", "next"], 3, note="no roadmap selected",
        )

    # ------------------------------------------------------------------
    # `task stat` (Change Status)
    # ------------------------------------------------------------------

    def test_task_stat_errors(self):
        r = self.roadmap
        backlog_id = self.mk_task(
            "Backfill missing invoice line items from the March migration",
            self.FR, self.TR, self.AC,
        )

        # #47: target state is not a recognised status.
        self.check(
            'Error: validation error: invalid task status: "X"',
            ["task", "stat", "-r", r, str(backlog_id), "ARCHIVED"], 6,
            subs={"X": "ARCHIVED"}, note="task stat unknown status",
        )
        # #48: an illegal transition to a state that itself needs no commit
        # flag (BACKLOG -> TESTING, skipping DOING).
        self.check(
            "Error: validation error: invalid status transition from X to Y for task N",
            ["task", "stat", "-r", r, str(backlog_id), "TESTING"], 6,
            subs={"X": "BACKLOG", "Y": "TESTING", "N": str(backlog_id)},
            note="task stat illegal transition",
        )
        # #49: SPRINT may only be set via `sprint add-tasks`.
        self.check(
            "Error: validation error: status SPRINT can only be set automatically via 'sprint add-tasks'",
            ["task", "stat", "-r", r, str(backlog_id), "SPRINT"], 6,
            note="task stat manual SPRINT",
        )
        # #50: --summary used with a non-COMPLETED target (validated before
        # the transition itself, so BACKLOG -> TESTING with --summary still
        # reports the summary misuse).
        self.check(
            "Error: validation error: --summary is only valid when transitioning to COMPLETED",
            ["task", "stat", "-r", r, str(backlog_id), "TESTING", "--summary", "Investigated and fixed"],
            6, note="task stat summary on non-COMPLETED",
        )
        # #51: --summary is not valid UTF-8 (validated before the task's own
        # existence, so a non-existent id still reaches this check first).
        argv = [self.test.cli_path, "task", "stat", "-r", r, str(backlog_id), "COMPLETED",
                "--commit-close", COMMIT_CLOSE_HASH, "--summary"]
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        proc = subprocess.run(
            [a.encode() for a in argv] + [b"Rolled back \xff\xfe the bad migration"],
            capture_output=True, env=env,
        )
        rc, err = proc.returncode, proc.stderr.decode("utf-8", errors="replace")
        expected = "Error: validation error: completion_summary: the value is not valid UTF-8"
        actual_line = err.splitlines()[0] if err else ""
        assert rc == 6, f"stat --summary invalid UTF-8: expected exit 6, got {rc}; stderr={err!r}"
        assert actual_line == expected, f"expected {expected!r}, got {actual_line!r}"
        REACHED.add(expected)

        # #52: --commit-open used with a non-DOING target.
        self.check(
            "Error: --commit-open flag is only allowed when transitioning to DOING",
            ["task", "stat", "-r", r, str(backlog_id), "TESTING", "--commit-open", COMMIT_OPEN_HASH],
            6, note="task stat commit-open on non-DOING",
        )
        # #53: --commit-close used with a non-COMPLETED target.
        self.check(
            "Error: --commit-close flag is only allowed when transitioning to COMPLETED",
            ["task", "stat", "-r", r, str(backlog_id), "TESTING", "--commit-close", COMMIT_CLOSE_HASH],
            6, note="task stat commit-close on non-COMPLETED",
        )
        # #54: DOING target without --commit-open.
        self.check(
            "Error: --commit-open is required when transitioning to DOING",
            ["task", "stat", "-r", r, str(backlog_id), "DOING"], 6,
            note="task stat DOING without commit-open",
        )
        # #55: COMPLETED target without --commit-close.
        self.check(
            "Error: --commit-close is required when transitioning to COMPLETED",
            ["task", "stat", "-r", r, str(backlog_id), "COMPLETED"], 6,
            note="task stat COMPLETED without commit-close",
        )
        # #56: malformed commit hash.
        self.check(
            'Error: invalid commit hash for --commit-open: "X" (expected 7 to 64 hexadecimal characters)',
            ["task", "stat", "-r", r, str(backlog_id), "DOING", "--commit-open", "xyz"], 6,
            subs={"X": "xyz"}, note="task stat malformed commit hash",
        )
        # #57: --commit-open supplied with no value after it.
        self.check(
            "Error: --commit-open requires a value",
            ["task", "stat", "-r", r, str(backlog_id), "DOING", "--commit-open"], 2,
            note="task stat commit-open with no value",
        )
        # --commit-close's own mirror of #56 (malformed hash).
        self.check(
            'Error: invalid commit hash for --commit-close: "X" (expected 7 to 64 hexadecimal characters)',
            ["task", "stat", "-r", r, str(backlog_id), "COMPLETED", "--commit-close", "xyz"], 6,
            subs={"X": "xyz"}, note="task stat malformed commit-close hash",
        )
        # --commit-close's own mirror of #57 (no value after the flag).
        self.check(
            "Error: --commit-close requires a value",
            ["task", "stat", "-r", r, str(backlog_id), "COMPLETED", "--commit-close"], 2,
            note="task stat commit-close with no value",
        )
        # #10: --summary exceeds 4096 characters. Reachable with the target
        # task still in BACKLOG: step 3 (summary length/encoding/control)
        # runs before step 5 (existence) and step 6 (transition legality),
        # so an otherwise-illegal BACKLOG -> COMPLETED jump with a valid
        # --commit-close still reaches the length check first.
        self.check(
            "Error: field exceeds maximum size: completion_summary exceeds maximum length of 4096 characters",
            ["task", "stat", "-r", r, str(backlog_id), "COMPLETED", "--commit-close", COMMIT_CLOSE_HASH,
             "--summary", "s" * 4097], 6, note="task stat summary too long",
        )

    # ------------------------------------------------------------------
    # `task prio` / `task sev`
    # ------------------------------------------------------------------

    def test_task_prio_sev_errors(self):
        r = self.roadmap
        task_id = self.mk_task(
            "Cap retry backoff for the settlement webhook consumer",
            self.FR, self.TR, self.AC,
        )
        # #58: task prio's own range wording.
        self.check(
            "Error: validation error: invalid priority: must be 0-9 (got N)",
            ["task", "prio", "-r", r, str(task_id), "15"], 6,
            subs={"N": "15"}, note="task prio out of range",
        )
        # #59: task sev's own range wording.
        self.check(
            "Error: validation error: invalid severity: must be 0-9 (got N)",
            ["task", "sev", "-r", r, str(task_id), "15"], 6,
            subs={"N": "15"}, note="task sev out of range",
        )

    # ------------------------------------------------------------------
    # `task remove`
    # ------------------------------------------------------------------

    def test_task_remove_errors(self):
        r = self.roadmap
        # #62: a task outside BACKLOG cannot be removed.
        active_id = self.mk_task(
            "Rotate the payment gateway's API signing key",
            self.FR, self.TR, self.AC,
        )
        sprint_id = self.mk_sprint(
            "Key rotation sprint",
            "Rotate every externally-facing signing key before the current "
            "one's scheduled expiry, with zero downtime for webhook senders.",
        )
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(sprint_id), str(active_id)])
        self.check(
            "Error: validation error: task #N cannot be deleted — status is X, must be BACKLOG",
            ["task", "remove", "-r", r, str(active_id)], 6,
            subs={"N": str(active_id), "X": "SPRINT"}, note="remove non-BACKLOG task",
        )

        # #63: a task with subtasks cannot be removed, even from BACKLOG.
        # The template distinguishes the parent's id (N) from the subtask
        # count (M), so ordinary subs suffice here.
        parent_id = self.mk_task(
            "Migrate order fulfillment queue to at-least-once delivery",
            self.FR, self.TR, self.AC,
        )
        self.test.run_cmd([
            "task", "create", "-r", r,
            "-t", "Add idempotency key to the fulfillment consumer",
            "-fr", self.FR, "-tr", self.TR, "-ac", self.AC,
            "--parent", str(parent_id),
        ])
        self.check(
            "Error: validation error: task #N cannot be deleted — it has M subtask(s); remove them first",
            ["task", "remove", "-r", r, str(parent_id)], 6,
            subs={"N": str(parent_id), "M": "1"}, note="remove task with subtask",
        )

    # ------------------------------------------------------------------
    # `task subtasks` / `task blockers` / `task blocking`
    # ------------------------------------------------------------------

    def test_task_relations_not_found(self):
        r = self.roadmap
        # #64: this three-command family's own not-found wording (no
        # trailing "not found", unlike most other resource-not-found rows).
        self.check(
            "Error: resource not found: task N",
            ["task", "subtasks", "-r", r, str(self.missing_id)], 4,
            subs={"N": str(self.missing_id)}, note="task subtasks not found",
        )
        self.check(
            "Error: resource not found: task N",
            ["task", "blockers", "-r", r, str(self.missing_id)], 4,
            subs={"N": str(self.missing_id)}, note="task blockers not found",
        )
        self.check(
            "Error: resource not found: task N",
            ["task", "blocking", "-r", r, str(self.missing_id)], 4,
            subs={"N": str(self.missing_id)}, note="task blocking not found",
        )
        # Shares the same X/format template as many other id-parsing rows;
        # exercised here through `task subtasks` specifically.
        self.check(
            'Error: invalid input: invalid task ID: "X" (must be a positive integer)',
            ["task", "subtasks", "-r", r, "abc"], 2, subs={"X": "abc"},
            note="task subtasks non-integer id",
        )

    # ------------------------------------------------------------------
    # `task add-dep` / `task remove-dep`
    # ------------------------------------------------------------------

    def test_task_dependency_errors(self):
        r = self.roadmap
        task_a = self.mk_task(
            "Reissue failed payouts flagged by the reconciliation job",
            self.FR, self.TR, self.AC,
        )
        task_b = self.mk_task(
            "Add a dead-letter queue for payouts the provider rejects twice",
            self.FR, self.TR, self.AC,
        )

        # #65: the dependent task does not exist. Sentinel in the middle:
        # "task #N not found: " wraps the lookup failure, then the
        # `resource not found: ` sentinel, per COMMANDS.md:1343.
        self.check(
            "Error: task #N not found: resource not found: task N",
            ["task", "add-dep", "-r", r, str(self.missing_id), str(task_a)], 4,
            subs={"N": str(self.missing_id)}, note="add-dep dependent task missing",
        )
        # #66: self-dependency.
        self.check(
            "Error: validation error: task cannot depend on itself",
            ["task", "add-dep", "-r", r, str(task_a), str(task_a)], 6,
            note="add-dep self dependency",
        )
        # #67: circular dependency. task_b depends on task_a; then making
        # task_a depend on task_b closes the cycle.
        self.test.run_cmd(["task", "add-dep", "-r", r, str(task_b), str(task_a)])
        key = "Error: validation error: adding dependency would create a circular dependency between task #N and task #M"
        assert key in CORPUS, key
        expected = (
            f"Error: validation error: adding dependency would create a circular "
            f"dependency between task #{task_a} and task #{task_b}"
        )
        rc, out, err = self.run_stdin(["task", "add-dep", "-r", r, str(task_a), str(task_b)])
        actual_line = err.splitlines()[0] if err else ""
        assert rc == 6, f"circular dependency: expected exit 6, got {rc}; stderr={err!r}"
        assert actual_line == expected, f"expected {expected!r}, got {actual_line!r}"
        REACHED.add(key)

        # #68: missing arguments (dep-id omitted).
        self.check(
            "Error: required parameter missing: task ID and dependency ID required",
            ["task", "add-dep", "-r", r, str(task_a)], 2, note="add-dep missing dep id",
        )
        # #69: removing a dependency that does not exist (task_a and task_b
        # carry no direct dependency in this direction).
        self.check(
            "Error: resource not found: dependency from task #N to task #M not found",
            ["task", "remove-dep", "-r", r, str(task_a), str(task_b)], 4,
            subs={"N": str(task_a), "M": str(task_b)}, note="remove-dep not found",
        )

    # ------------------------------------------------------------------
    # `task comment-add` / `comment-edit` / friends
    # ------------------------------------------------------------------

    def test_task_comment_errors(self):
        r = self.roadmap
        task_id = self.mk_task(
            "Investigate duplicate charge reports from the mobile checkout flow",
            self.FR, self.TR, self.AC,
        )

        # #14: --type absent.
        self.check(
            "Error: required parameter missing: --type",
            ["task", "comment-add", "-r", r, str(task_id), "--body", "Reproduced on iOS 18.1 only"],
            2, note="task comment-add missing type",
        )
        # #15: --type not one of the seven task comment values.
        self.check(
            'Error: validation error: invalid comment type "X" for a task comment; valid types: '
            'FINDING, HYPOTHESIS, TEST, DECISION, PROGRESS, UPDATE, NOTE',
            ["task", "comment-add", "-r", r, str(task_id), "--type", "BLOCKER",
             "--body", "Reproduced on iOS 18.1 only"], 6, subs={"X": "BLOCKER"},
            note="task comment-add invalid type",
        )
        # #4: no comment body supplied (--body present but empty).
        self.check(
            "Error: required parameter missing: no comment body supplied",
            ["task", "comment-add", "-r", r, str(task_id), "--type", "NOTE", "--body", ""],
            2, note="task comment-add empty body flag",
        )
        # #11: comment body exceeds 4096 characters.
        self.check(
            "Error: field exceeds maximum size: body exceeds maximum length of 4096 characters",
            ["task", "comment-add", "-r", r, str(task_id), "--type", "NOTE", "--body", "b" * 4097],
            6, note="task comment-add body too long",
        )
        # #70: comment body is not valid UTF-8.
        argv = [self.test.cli_path, "task", "comment-add", "-r", r, str(task_id),
                "--type", "NOTE", "--body"]
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        proc = subprocess.run(
            [a.encode() for a in argv] + [b"Repro steps attached \xff\xfe below"],
            capture_output=True, env=env,
        )
        rc, err = proc.returncode, proc.stderr.decode("utf-8", errors="replace")
        expected = "Error: validation error: body: the value is not valid UTF-8"
        actual_line = err.splitlines()[0] if err else ""
        assert rc == 6, f"comment body invalid UTF-8: expected exit 6, got {rc}; stderr={err!r}"
        assert actual_line == expected, f"expected {expected!r}, got {actual_line!r}"
        REACHED.add(expected)

        # #71: comment body carries a forbidden control character.
        self.check(
            "Error: validation error: body: control characters are not allowed",
            ["task", "comment-add", "-r", r, str(task_id), "--type", "NOTE",
             "--body", "\x0bRepro steps attached below"], 6,
            note="task comment-add control char in body",
        )
        # #72: task not found (comment-add's own not-found wording, WITH the
        # trailing "not found", unlike `task subtasks`/`blockers`/`blocking`).
        self.check(
            "Error: resource not found: task N not found",
            ["task", "comment-add", "-r", r, str(self.missing_id), "--type", "NOTE",
             "--body", "Reproduced on iOS 18.1 only"], 4,
            subs={"N": str(self.missing_id)}, note="task comment-add task not found",
        )
        # Roadmap not found on `task comment-add`. COMMANDS.md:1548 publishes
        # `Error: resource not found: roadmap "X" not found` (WITH a trailing
        # "not found") for this exact scenario, attributed to key #73 in this
        # module's development notes. The binary does NOT print that: this
        # command resolves the roadmap through the shared connection-opening
        # path (internal/db/connection.go:438/481, `roadmap %q` with NO
        # "not found" suffix), the same path `stats`/`backlog show-next`/
        # `task list`/`sprint list`/`audit list` all use -- verified
        # empirically across all of them. Only `graph`'s own subcommands
        # carry the "not found" suffix, from a distinct pre-check in
        # internal/commands/graph.go:301. SPEC/COMMANDS.md:1548 (and its
        # sibling at :2609 for `sprint comment-add`) is therefore a genuine,
        # previously-undetected drift from the binary -- exactly the class
        # of defect rmp task #277 exists to catch -- reported to
        # specification-manager rather than silently reconciled here. This
        # check asserts what the binary ACTUALLY prints, i.e. the #104
        # template (see test_backlog_and_stats_errors), not the SPEC line
        # this scenario cites.
        ghost_roadmap = "ghost-roadmap-9182"
        self.check(
            'Error: resource not found: roadmap "X"',
            ["task", "comment-add", "-r", ghost_roadmap, "1", "--type", "NOTE",
             "--body", "Reproduced on iOS 18.1 only"], 4,
            subs={"X": ghost_roadmap}, note="task comment-add roadmap not found",
        )
        # #74: missing task-id positional entirely.
        self.check(
            "Error: required parameter missing: task ID required",
            ["task", "comment-add", "-r", r], 2, note="task comment-add missing task id",
        )
        # #75: a second positional argument is refused.
        self.check(
            'Error: invalid input: unexpected argument "X"',
            ["task", "comment-add", "-r", r, str(task_id), "orphan-token",
             "--type", "NOTE", "--body", "Reproduced on iOS 18.1 only"], 2,
            subs={"X": "orphan-token"}, note="task comment-add extra positional",
        )
        # #76: an unknown flag. The example in COMMANDS.md is literal
        # ("--foo" is not a placeholder), so this invokes with that exact
        # flag name to reproduce the exact published line.
        self.check(
            "Error: invalid input: unknown flag: --foo",
            ["task", "comment-add", "-r", r, str(task_id), "--type", "NOTE",
             "--body", "Reproduced on iOS 18.1 only", "--foo"], 2,
            note="task comment-add unknown flag",
        )

        # Create one real comment to drive comment-edit/-remove's id-based
        # errors below.
        comment_id = self.test.run_cmd_json(
            ["task", "comment-add", "-r", r, str(task_id), "--type", "NOTE",
             "--body", "Reproduced on iOS 18.1 only"]
        )["id"]

        # #5: comment-edit with no --type, no --body, and empty stdin.
        self.check(
            "Error: required parameter missing: at least one of --type or --body is required",
            ["task", "comment-edit", "-r", r, str(comment_id)], 2,
            note="task comment-edit no change requested",
        )
        # #78: invalid comment ID format.
        self.check(
            'Error: invalid input: invalid comment ID: "X" (must be a positive integer)',
            ["task", "comment-edit", "-r", r, "abc", "--body", "Root cause confirmed: race in the retry path"],
            2, subs={"X": "abc"}, note="task comment-edit non-integer id",
        )
        # #79: comment not found.
        self.check(
            "Error: resource not found: task comment N not found",
            ["task", "comment-edit", "-r", r, str(self.missing_id),
             "--body", "Root cause confirmed: race in the retry path"], 4,
            subs={"N": str(self.missing_id)}, note="task comment-edit not found",
        )
        # #80: missing comment ID positional entirely.
        self.check(
            "Error: required parameter missing: comment ID required",
            ["task", "comment-remove", "-r", r], 2, note="task comment-remove missing id",
        )

    # ------------------------------------------------------------------
    # The database-failure row of the comment subcommands (rmp task #319).
    #
    # COMMANDS.md publishes `Error: database error: <detail>` six times: on
    # `comment-add`, `comment-edit` and `comment-remove`, in the task family
    # and again in the sprint family. Until #319 the binary printed those
    # lines WITHOUT the sentinel ("Error: writing task comment: ..."), and
    # this module reported green over all six because the string was a blanket
    # EXEMPT_KEYS entry. It is now driven, with only the SQLite diagnostic in
    # the tail left unasserted (TAIL_EXEMPT_KEYS).
    #
    # The failure is provoked by dropping the two comment tables out of THIS
    # module's own throwaway project.db -- a file the fixture created under a
    # temporary HOME and deletes in teardown. Nothing shared is touched.
    # ------------------------------------------------------------------

    DB_FAIL_KEY = "Error: database error: <detail>"

    def drop_comment_tables(self):
        """Remove both comment tables from this fixture's project.db, so every
        comment statement fails for a reason that is neither a missing row nor
        a constraint violation -- the third propagation row, and the only one
        that must produce the database-error sentinel."""
        db_path = self.test.roadmaps_dir / self.roadmap / "project.db"
        assert db_path.exists(), f"fixture database not found at {db_path}"
        con = sqlite3.connect(str(db_path))
        try:
            con.execute("DROP TABLE task_comments")
            con.execute("DROP TABLE sprint_comments")
            con.commit()
        finally:
            con.close()

    def test_comment_database_failures(self):
        r = self.roadmap
        task_id = self.mk_task(
            "Reconcile the comment write path with the propagation table",
            "A database failure must reach the user as a classified failure, "
            "not as an unlabelled message that exits 1 by accident.",
            "Wrap at internal/db, after the constraint classifier, never before it.",
            "The stderr line begins with the database-error sentinel and keeps "
            "its operation context.",
        )
        sprint_id = self.mk_sprint(
            "Error propagation hardening", self.SPRINT_DESC,
        )
        task_comment_id = self.test.run_cmd_json(
            ["task", "comment-add", "-r", r, str(task_id), "--type", "FINDING",
             "--body", "The write path names no sentinel, so the class is lost."]
        )["id"]
        sprint_comment_id = self.test.run_cmd_json(
            ["sprint", "comment-add", "-r", r, str(sprint_id), "--type", "FINDING",
             "--body", "The sprint family shares the same query layer."]
        )["id"]

        # --------------------------------------------------------------
        # First, the two propagation rows that sit ABOVE the database row,
        # asserted against an INTACT schema. A blanket wrap -- the obvious
        # wrong fix, applied ahead of the classifier instead of after it --
        # would turn both of these into "Error: database error: ..." with exit
        # 1, so these two assertions are what makes the fix's placement
        # observable from outside the binary.
        # --------------------------------------------------------------

        # Row 1: sql.ErrNoRows stays utils.ErrNotFound, exit 4. The wrapped
        # return in getComment sits on the very next line after this branch.
        self.check(
            "Error: resource not found: task comment N not found",
            ["task", "comment-remove", "-r", r, str(self.missing_id)], 4,
            subs={"N": str(self.missing_id)},
            note="ErrNoRows is not swallowed by the database sentinel",
        )
        # Row 2: a constraint violation stays utils.ErrAlreadyExists, exit 5.
        # The seeded sprint above holds order 1; a second sprint claiming it
        # collides on idx_sprints_order.
        self.check(
            "Error: resource already exists: sprint order N is already in use",
            ["sprint", "create", "-r", r, "-t", "Checkout reliability",
             "-d", self.SPRINT_DESC, "--order", "1"], 5, subs={"N": "1"},
            note="a constraint violation is not swallowed by the database sentinel",
        )

        # --------------------------------------------------------------
        # Now the database row itself, on all six published sites.
        # --------------------------------------------------------------
        self.drop_comment_tables()

        self.check_head(
            self.DB_FAIL_KEY,
            ["task", "comment-add", "-r", r, str(task_id), "--type", "NOTE",
             "--body", "This insert cannot reach a table that is gone."],
            1, tail_contains=("writing task comment: ",),
            note="task comment-add database failure",
        )
        self.check_head(
            self.DB_FAIL_KEY,
            ["task", "comment-edit", "-r", r, str(task_comment_id),
             "--body", "This update cannot reach a table that is gone."],
            1, tail_contains=(f"querying task comment {task_comment_id}: ",),
            note="task comment-edit database failure",
        )
        self.check_head(
            self.DB_FAIL_KEY,
            ["task", "comment-remove", "-r", r, str(task_comment_id)],
            1, tail_contains=(f"querying task comment {task_comment_id}: ",),
            note="task comment-remove database failure",
        )
        self.check_head(
            self.DB_FAIL_KEY,
            ["sprint", "comment-add", "-r", r, str(sprint_id), "--type", "PROGRESS",
             "--body", "This insert cannot reach a table that is gone."],
            1, tail_contains=("writing sprint comment: ",),
            note="sprint comment-add database failure",
        )
        self.check_head(
            self.DB_FAIL_KEY,
            ["sprint", "comment-edit", "-r", r, str(sprint_comment_id),
             "--body", "This update cannot reach a table that is gone."],
            1, tail_contains=(f"querying sprint comment {sprint_comment_id}: ",),
            note="sprint comment-edit database failure",
        )
        self.check_head(
            self.DB_FAIL_KEY,
            ["sprint", "comment-remove", "-r", r, str(sprint_comment_id)],
            1, tail_contains=(f"querying sprint comment {sprint_comment_id}: ",),
            note="sprint comment-remove database failure",
        )

        # `comment-list` is asserted here too, and deliberately NOT through
        # check_head: COMMANDS.md publishes no database-failure row for the
        # listing subcommands, so claiming one would be a fiction. The listing
        # nevertheless reaches the THIRD of the three sites #319 fixed
        # (listComments), which none of the six published rows exercise, and
        # ARCHITECTURE.md § Propagation Rules governs it in the same terms.
        for args, context in (
            (["task", "comment-list", "-r", r, str(task_id)], "querying task comments: "),
            (["sprint", "comment-list", "-r", r, str(sprint_id)], "querying sprint comments: "),
        ):
            rc, out, err = self.run_stdin(args)
            line = err.splitlines()[0] if err else ""
            assert rc == 1, f"{args}: expected exit 1, got {rc}; stderr={err!r}"
            assert line.startswith("Error: database error: " + context), (
                f"{args}: the listing lost the sentinel or its operation "
                f"context: {line!r}"
            )
            assert out == "", f"{args}: a failing invocation wrote to stdout: {out!r}"

    # ------------------------------------------------------------------
    # `sprint create` / `sprint update`
    # ------------------------------------------------------------------

    SPRINT_DESC = (
        "Give the checkout team the latency and error signals needed to "
        "catch a regression before customers report it."
    )

    def test_sprint_create_errors(self):
        r = self.roadmap
        # #81: --description absent.
        self.check(
            "Error: required parameter missing: --description",
            ["sprint", "create", "-r", r, "-t", "Observability hardening"], 2,
            note="sprint create missing description",
        )
        # Title absent shares its template with #7 (--<flag> generic),
        # reached above via task create; sprint create's own concrete form:
        self.check(
            "Error: required parameter missing: --title",
            ["sprint", "create", "-r", r, "-d", self.SPRINT_DESC], 2,
            note="sprint create missing title",
        )
        # #82: description supplied but empty once trimmed.
        self.check(
            "Error: validation error: description cannot be empty",
            ["sprint", "create", "-r", r, "-t", "Observability hardening", "-d", "   "],
            6, note="sprint create description empty once trimmed",
        )
        # #9: description exceeds 2048 characters.
        self.check(
            "Error: field exceeds maximum size: description exceeds maximum length of 2048 characters",
            ["sprint", "create", "-r", r, "-t", "Observability hardening", "-d", "d" * 2049],
            6, note="sprint create description too long",
        )
        # #83: --max-tasks out of the 1-10000 range.
        self.check(
            "Error: validation error: --max-tasks must be between 1 and 10000 (got N)",
            ["sprint", "create", "-r", r, "-t", "Observability hardening", "-d", self.SPRINT_DESC,
             "--max-tasks", "10001"], 6, subs={"N": "10001"},
            note="sprint create max-tasks out of range",
        )
        # #84: --max-tasks non-integer.
        self.check(
            'Error: invalid input: invalid value for --max-tasks: strconv.Atoi: parsing "X": invalid syntax',
            ["sprint", "create", "-r", r, "-t", "Observability hardening", "-d", self.SPRINT_DESC,
             "--max-tasks", "plenty"], 2, subs={"X": "plenty"},
            note="sprint create max-tasks non-integer",
        )
        # #85: --order <= 0 (integer, out of range).
        self.check(
            "Error: validation error: --order must be a positive integer greater than zero (got N)",
            ["sprint", "create", "-r", r, "-t", "Observability hardening", "-d", self.SPRINT_DESC,
             "--order", "0"], 6, subs={"N": "0"},
            note="sprint create order zero",
        )
        # #86: --order non-integer (exit 6, NOT 2 -- unlike --max-tasks).
        self.check(
            "Error: validation error: --order must be a positive integer greater than zero",
            ["sprint", "create", "-r", r, "-t", "Observability hardening", "-d", self.SPRINT_DESC,
             "--order", "soon"], 6, note="sprint create order non-integer",
        )
        # #87: --order collides with another sprint's order.
        self.test.run_cmd_json(
            ["sprint", "create", "-r", r, "-t", "Checkout reliability", "-d", self.SPRINT_DESC,
             "--order", "1"]
        )
        self.check(
            "Error: resource already exists: sprint order N is already in use",
            ["sprint", "create", "-r", r, "-t", "Observability hardening", "-d", self.SPRINT_DESC,
             "--order", "1"], 5, subs={"N": "1"}, note="sprint create order collision",
        )

    def test_sprint_update_errors(self):
        r = self.roadmap
        sprint_id = self.mk_sprint("Observability hardening", self.SPRINT_DESC)

        # S2: no field flag supplied at all.
        self.check(
            "Error: required parameter missing: at least one of --title, --description, "
            "--max-tasks or --order is required",
            ["sprint", "update", "-r", r, str(sprint_id)], 2,
            note="sprint update no flags",
        )
        # Title empty once trimmed shares COMMANDS.md's concrete template
        # with #28/#82 (task/sprint create); reached here on `update`.
        self.check(
            "Error: validation error: title cannot be empty",
            ["sprint", "update", "-r", r, str(sprint_id), "-t", "   "], 6,
            note="sprint update title empty once trimmed",
        )
        self.check(
            "Error: field exceeds maximum size: title exceeds maximum length of 255 characters",
            ["sprint", "update", "-r", r, str(sprint_id), "-t", "x" * 256], 6,
            note="sprint update title too long",
        )

        # #92: --order rejected once the sprint is CLOSED.
        closed_id = self.mk_sprint(
            "Legacy invoice cleanup",
            "Retire the legacy invoice export path now that every downstream "
            "consumer reads from the new reporting API.",
        )
        self.test.run_cmd(["sprint", "start", "-r", r, str(closed_id)])
        self.test.run_cmd(["sprint", "close", "-r", r, str(closed_id)])
        self.check(
            "Error: validation error: sprint #N order cannot be changed — sprint is CLOSED",
            ["sprint", "update", "-r", r, str(closed_id), "--order", "77"], 6,
            subs={"N": str(closed_id)}, note="sprint update order on CLOSED sprint",
        )

    # ------------------------------------------------------------------
    # `sprint open-tasks`, `sprint add-tasks`/`remove-tasks`, `sprint remove`
    # ------------------------------------------------------------------

    def test_sprint_task_management_errors(self):
        r = self.roadmap
        # #88: sprint not found (open-tasks' own wording, no "not found" suffix).
        self.check(
            "Error: resource not found: sprint N",
            ["sprint", "open-tasks", "-r", r, str(self.missing_id)], 4,
            subs={"N": str(self.missing_id)}, note="sprint open-tasks not found",
        )
        # #89: missing sprint ID positional.
        self.check(
            "Error: required parameter missing: sprint ID required",
            ["sprint", "open-tasks", "-r", r], 2, note="sprint open-tasks missing id",
        )

        sprint_id = self.mk_sprint("Observability hardening", self.SPRINT_DESC)
        task_id = self.mk_task(
            "Emit checkout latency histograms to the metrics pipeline",
            self.FR, self.TR, self.AC,
        )
        outside_task_id = self.mk_task(
            "Add a dead-letter queue for payouts the provider rejects twice",
            self.FR, self.TR, self.AC,
        )
        outside_task_id_2 = self.mk_task(
            "Retry provider webhooks with exponential backoff and a cap",
            self.FR, self.TR, self.AC,
        )
        outside_task_id_3 = self.mk_task(
            "Reconcile settlement totals against the provider daily report",
            self.FR, self.TR, self.AC,
        )

        # #90/#91: both messages render a Go slice of ids, published as the
        # `<ids>` placeholder. The list is VARIABLE-LENGTH, so driving a
        # single length would let a template that hard-codes a fixed number
        # of members pass: the earlier `[N M]` spelling passed only because
        # this module happened to choose exactly two ids. Each message is
        # therefore driven at one, two AND three members, and the three-member
        # case supplies the ids OUT of ascending order to prove the binary
        # echoes the caller's order rather than a sorted one.
        for ids in (
            [self.missing_id],
            [self.missing_id, self.missing_id + 1],
            [self.missing_id + 2, self.missing_id, self.missing_id + 1],
        ):
            self.check(
                "Error: resource not found: task(s) not found: [<ids>]",
                ["sprint", "add-tasks", "-r", r, str(sprint_id),
                 ",".join(str(i) for i in ids)], 4,
                subs={"<ids>": " ".join(str(i) for i in ids)},
                note=f"sprint add-tasks missing ids ({len(ids)} member(s))",
            )

        for ids in (
            [outside_task_id],
            [outside_task_id, outside_task_id_2],
            [outside_task_id_3, outside_task_id, outside_task_id_2],
        ):
            self.check(
                "Error: validation error: task(s) not in sprint #N: [<ids>]",
                ["sprint", "remove-tasks", "-r", r, str(sprint_id),
                 ",".join(str(i) for i in ids)], 6,
                subs={"N": str(sprint_id),
                      "<ids>": " ".join(str(i) for i in ids)},
                note=f"sprint remove-tasks non-members ({len(ids)} member(s))",
            )

        # Sprint ID does not exist, on add-tasks (same #88 template, a
        # different call site -- this exercises the OTHER source line
        # rather than double-counting the same invocation).
        self.check(
            "Error: resource not found: sprint N",
            ["sprint", "add-tasks", "-r", r, str(self.missing_id), str(task_id)], 4,
            subs={"N": str(self.missing_id)}, note="sprint add-tasks sprint not found",
        )
        # A task ID that is not a positive integer, on add-tasks.
        self.check(
            'Error: invalid input: invalid task ID: "X" (must be a positive integer)',
            ["sprint", "add-tasks", "-r", r, str(sprint_id), "abc"], 2,
            subs={"X": "abc"}, note="sprint add-tasks non-integer task id",
        )

        # #93: `sprint remove`'s own not-found wording (WITH "not found").
        self.check(
            "Error: resource not found: sprint N not found",
            ["sprint", "remove", "-r", r, str(self.missing_id)], 4,
            subs={"N": str(self.missing_id)}, note="sprint remove not found",
        )

    # ------------------------------------------------------------------
    # `sprint comment-add` / `comment-edit`
    # ------------------------------------------------------------------

    def test_sprint_comment_errors(self):
        r = self.roadmap
        sprint_id = self.mk_sprint("Observability hardening", self.SPRINT_DESC)

        # #16: --type not one of the four sprint comment values.
        self.check(
            'Error: validation error: invalid comment type "X" for a sprint comment; valid types: '
            'FINDING, DECISION, PROGRESS, UPDATE',
            ["sprint", "comment-add", "-r", r, str(sprint_id), "--type", "HYPOTHESIS",
             "--body", "Latency p99 dropped 40% after the histogram rollout"], 6,
            subs={"X": "HYPOTHESIS"}, note="sprint comment-add invalid type",
        )
        # #94: invalid sprint ID format.
        self.check(
            'Error: invalid input: invalid sprint ID: "X" (must be a positive integer)',
            ["sprint", "comment-add", "-r", r, "abc", "--type", "FINDING",
             "--body", "Latency p99 dropped 40% after the histogram rollout"], 2,
            subs={"X": "abc"}, note="sprint comment-add non-integer id",
        )

        comment_id = self.test.run_cmd_json(
            ["sprint", "comment-add", "-r", r, str(sprint_id), "--type", "FINDING",
             "--body", "Latency p99 dropped 40% after the histogram rollout"]
        )["id"]

        # #95: sprint comment not found.
        self.check(
            "Error: resource not found: sprint comment N not found",
            ["sprint", "comment-edit", "-r", r, str(self.missing_id),
             "--body", "Confirmed against the dashboard for a full week"], 4,
            subs={"N": str(self.missing_id)}, note="sprint comment-edit not found",
        )
        # A second positional argument, on `sprint comment-remove`.
        self.check(
            'Error: invalid input: unexpected argument "X"',
            ["sprint", "comment-remove", "-r", r, str(comment_id), "orphan-token"], 2,
            subs={"X": "orphan-token"}, note="sprint comment-remove extra positional",
        )
        # Missing comment ID, on `sprint comment-edit`.
        self.check(
            "Error: required parameter missing: comment ID required",
            ["sprint", "comment-edit", "-r", r], 2, note="sprint comment-edit missing id",
        )

    # ------------------------------------------------------------------
    # `audit list` / `audit history`
    # ------------------------------------------------------------------

    def test_audit_errors(self):
        r = self.roadmap
        # #96: --limit out of range.
        self.check(
            "Error: validation error: --limit must be between 1 and 500 (got N)",
            ["audit", "list", "-r", r, "--limit", "501"], 6, subs={"N": "501"},
            note="audit list limit out of range",
        )
        # #97: --limit non-integer.
        self.check(
            "Error: invalid input: invalid limit: X",
            ["audit", "list", "-r", r, "--limit", "plenty"], 2, subs={"X": "plenty"},
            note="audit list limit non-integer",
        )
        # #98: --entity-id out of range.
        self.check(
            "Error: validation error: --entity-id must be between 1 and 2147483647 (got N)",
            ["audit", "list", "-r", r, "--entity-id", "0"], 6, subs={"N": "0"},
            note="audit list entity-id out of range",
        )
        # #99: --entity-id non-integer.
        self.check(
            "Error: invalid input: invalid entity ID: X",
            ["audit", "list", "-r", r, "--entity-id", "abc"], 2, subs={"X": "abc"},
            note="audit list entity-id non-integer",
        )
        # #100: -e/--entity-type not TASK or SPRINT (generic X template).
        # The same corpus key is published twice -- COMMANDS.md:2929 for the
        # `audit list` flag and :2972 for the `audit history` positional -- so
        # driving it once covers both, which is the point: the two surfaces
        # share one enum owner and therefore one string (rmp task #289).
        self.check(
            'Error: validation error: invalid entity type: "X"',
            ["audit", "list", "-r", r, "-e", "PROJECT"], 6, subs={"X": "PROJECT"},
            note="audit list invalid entity type",
        )
        # #100b: -o/--operation not in the catalogue. COMMANDS.md:2928
        # publishes this row; before rmp task #289 the `-o` refusal had no
        # published row at all, which is exactly why its divergent wording
        # ("invalid operation: BOGUS", unquoted) survived unnoticed -- an
        # unpublished string is invisible to this gate.
        self.check(
            'Error: validation error: invalid audit operation: "X"',
            ["audit", "list", "-r", r, "-o", "TASK_TELEPORT"], 6,
            subs={"X": "TASK_TELEPORT"}, note="audit list invalid operation",
        )
        # #101: <entity-id> positional is not an integer, on `audit history`.
        self.check(
            'Error: invalid input: invalid entity ID: "X" (must be a positive integer)',
            ["audit", "history", "-r", r, "TASK", "abc"], 2, subs={"X": "abc"},
            note="audit history non-integer entity id",
        )
        # #102: <entity-id> is zero (literal "0" in the published text, not
        # a placeholder -- the message states the exact rejected value).
        self.check(
            "Error: validation error: invalid entity ID: 0 (must be positive)",
            ["audit", "history", "-r", r, "TASK", "0"], 6, note="audit history entity id zero",
        )
        # #103: <entity-id> exceeds MaxInt32.
        self.check(
            "Error: validation error: invalid entity ID: N (exceeds maximum value 2147483647)",
            ["audit", "history", "-r", r, "TASK", "2147483648"], 6,
            subs={"N": "2147483648"}, note="audit history entity id too large",
        )
        # Concrete instance of the -e/--entity-type template above, this
        # time on `audit history`'s leading positional -- COMMANDS.md:2829
        # names this exact scenario ("a leading -e is parsed as the
        # entity-type value"), so -e is the literal value under test.
        self.check(
            'Error: validation error: invalid entity type: "-e"',
            ["audit", "history", "-r", r, "-e", "1"], 6,
            note="audit history leading -e parsed as entity type",
        )
        # The audit date filters accept the same two forms `task list` does
        # (rmp task 324). One pair drives both the `audit list` Bound Validation
        # rows and the `audit stats` Error Conditions rows, since the corpus
        # dedupes by content and the two tables publish the same strings.
        self.check(
            'Error: validation error: --since: invalid date format: expected RFC3339 '
            '(2026-01-01T00:00:00Z) or date-only (2026-01-01): "X"',
            ["audit", "list", "-r", r, "--since", "last-tuesday"], 6,
            subs={"X": "last-tuesday"}, note="audit list invalid since",
        )
        self.check(
            'Error: validation error: --until: invalid date format: expected RFC3339 '
            '(2026-01-01T00:00:00Z) or date-only (2026-01-01): "X"',
            ["audit", "list", "-r", r, "--until", "next-friday"], 6,
            subs={"X": "next-friday"}, note="audit list invalid until",
        )

    # ------------------------------------------------------------------
    # `backlog show-next` / `stats`
    # ------------------------------------------------------------------

    def test_backlog_and_stats_errors(self):
        r = self.roadmap
        ghost_roadmap = "ghost-roadmap-9182"
        # #104: roadmap not found (WITHOUT trailing "not found" -- distinct
        # from #73's comment-add wording).
        self.check(
            'Error: resource not found: roadmap "X"',
            ["backlog", "show-next", "-r", ghost_roadmap], 4,
            subs={"X": ghost_roadmap}, note="backlog show-next roadmap not found",
        )
        self.check(
            'Error: resource not found: roadmap "X"',
            ["stats", "-r", ghost_roadmap], 4,
            subs={"X": ghost_roadmap}, note="stats roadmap not found",
        )
        # #105: show-next's count argument must be a positive integer.
        self.check(
            "Error: validation error: count must be a positive integer",
            ["backlog", "show-next", "0", "-r", r], 6, note="backlog show-next zero count",
        )

    # ------------------------------------------------------------------
    # `graph create` / `query` / `update` / `delete` / `search`
    # ------------------------------------------------------------------

    def test_graph_errors(self):
        r = self.roadmap
        # #106: no query supplied (neither --query nor stdin).
        self.check(
            "Error: required parameter missing: no query supplied",
            ["graph", "query", "-r", r], 2, note="graph query no query supplied",
        )
        # #107: query above the maximum length (1 MiB). Fed on stdin rather
        # than as a --query argument: an argv entry this large trips the
        # operating system's own ARG_MAX before rmp ever sees it (measured:
        # `OSError(7, 'Argument list too long')` well under 1 MiB on this
        # host), and GRAPH.md's Cypher Input Source and Precedence makes
        # standard input an equally valid, equally load-bearing source for
        # the SAME bounded-length check.
        oversized = "RETURN " + ("1" * 1048571)
        assert len(oversized) == 1048578
        self.check(
            "Error: validation error: query exceeds maximum length of 1048576 bytes",
            ["graph", "query", "-r", r], 6, stdin=oversized,
            note="graph query oversized",
        )
        # #108: `graph create` rejects a read-only query.
        self.check(
            "Error: validation error: graph create accepts only CREATE/MERGE queries",
            ["graph", "create", "-r", r, "--query", "MATCH (n) RETURN n"], 6,
            note="graph create rejects read-only query",
        )
        # #109: `graph query` rejects a writing query.
        self.check(
            "Error: validation error: graph query accepts only read-only queries",
            ["graph", "query", "-r", r, "--query", "CREATE (n:Incident {key:'payment-outage-0417'})"],
            6, note="graph query rejects writing query",
        )
        # #110: `graph update` rejects a query outside SET/REMOVE.
        self.check(
            "Error: validation error: graph update accepts only SET/REMOVE queries",
            ["graph", "update", "-r", r, "--query", "CREATE (n:Incident {key:'payment-outage-0417'})"],
            6, note="graph update rejects create query",
        )
        # #111: `graph delete` rejects a query outside DELETE/DETACH DELETE.
        self.check(
            "Error: validation error: graph delete accepts only DELETE/DETACH DELETE queries",
            ["graph", "delete", "-r", r, "--query", "CREATE (n:Incident {key:'payment-outage-0417'})"],
            6, note="graph delete rejects create query",
        )
        # #112: `graph search` rejects a writing query.
        self.check(
            "Error: validation error: graph search accepts only read-only queries",
            ["graph", "search", "-r", r, "--query", "CREATE (n:Incident {key:'payment-outage-0417'})"],
            6, note="graph search rejects writing query",
        )
        # Roadmap not found, on a graph subcommand (WITH "not found",
        # distinct from #104's stats/backlog wording).
        self.check(
            'Error: resource not found: roadmap "X" not found',
            ["graph", "query", "-r", "ghost-roadmap-9182", "--query", "MATCH (n) RETURN n"], 4,
            subs={"X": "ghost-roadmap-9182"}, note="graph query roadmap not found",
        )
        # A Cypher query written as a positional argument. The graph family
        # declares a maximum of zero positional arguments (COMMANDS.md
        # § Positional Arity by Command), and it is one of the three commands
        # that publish a line of their own for the refusal: the canonical
        # wording with a parenthetical naming the two sources a query may
        # come from. The roadmap named here EXISTS, so the exit code proves
        # the refusal precedes opening the graph store rather than following
        # a lookup failure.
        self.check(
            'Error: invalid input: unexpected argument "X" (graph queries use --query or stdin)',
            ["graph", "query", "-r", r, "MATCH (n:Incident) RETURN n"], 2,
            subs={"X": "MATCH (n:Incident) RETURN n"},
            note="graph query bare positional query",
        )

    # ------------------------------------------------------------------
    # `rmp web`
    # ------------------------------------------------------------------

    def test_web_errors(self):
        # #117: --port out of range (literal "70000" -- not a placeholder).
        self.check(
            'Error: validation error: --port must be an integer between 0 and 65535 (got 70000)',
            ["web", "--port", "70000", "--no-open"], 6, note="web port out of range",
        )
        # #118: --port not an integer (literal "notanumber" -- not a placeholder).
        self.check(
            'Error: validation error: --port must be an integer between 0 and 65535 (got "notanumber")',
            ["web", "--port", "notanumber", "--no-open"], 6, note="web port non-integer",
        )
        # Unknown flag on `rmp web` (shares the literal "--foo" template).
        self.check(
            "Error: invalid input: unknown flag: --foo",
            ["web", "--foo", "--no-open"], 2, note="web unknown flag",
        )
        # A positional argument on `rmp web`. The command declares a maximum
        # of zero (COMMANDS.md § Positional Arity by Command) and is the
        # third command that publishes a line of its own for the refusal:
        # the offending token follows a colon and carries no quotes. The
        # refusal precedes binding the listener, so no server is started.
        self.check(
            "Error: invalid input: unexpected argument: X",
            ["web", "monitoring-dashboard", "--no-open"], 2,
            subs={"X": "monitoring-dashboard"}, note="web positional argument",
        )

    # ------------------------------------------------------------------
    # #119: the data directory itself is unreadable.
    # ------------------------------------------------------------------

    def test_unreadable_data_directory(self):
        roadmaps_dir = self.test.home_dir / ".roadmaps"
        roadmaps_dir.mkdir(parents=True, exist_ok=True)
        os.chmod(roadmaps_dir, 0o000)
        try:
            self.check(
                "Error: reading data directory <absolute path of ~/.roadmaps>: database error",
                ["roadmap", "list"], 1,
                subs={"<absolute path of ~/.roadmaps>": str(roadmaps_dir)},
                note="unreadable data directory",
            )
        finally:
            # Restore before teardown_method's shutil.rmtree walks this
            # directory, which an unreadable directory would make fail.
            os.chmod(roadmaps_dir, 0o700)

    # ------------------------------------------------------------------
    # Placeholder rule, proved in both directions (NON-VACUITY requirement
    # 3c): a literal `<name>`/`<id>` in the two documented exceptions is
    # NEVER substituted, while a genuine N placeholder tracks whatever
    # numeric value this module actually supplied.
    # ------------------------------------------------------------------

    def test_placeholder_rule_both_directions(self):
        r = self.roadmap

        # Direction 1: `<name>` is NOT a placeholder in this one message
        # (COMMANDS.md:66 names it explicitly as one of the two literal
        # exceptions). The check below never substitutes anything for
        # "<name>" -- it is left as literal template text -- and the
        # comparison against captured stderr still passes CHARACTER FOR
        # CHARACTER, which is only possible because the binary's own text
        # really does print the four characters "<name>" verbatim, twice,
        # rather than interpolating this module's roadmap name into either
        # position. If the rule were wrong (if `<name>` were treated as a
        # placeholder standing for a real roadmap name), substituting a
        # concrete value here would have been REQUIRED for the comparison
        # to pass, and no such substitution is supplied.
        rc, out, err = self.run_stdin(["task", "list"])
        actual_line = err.splitlines()[0] if err else ""
        assert actual_line == "Error: no roadmap selected: use -r <name> or --roadmap <name>", actual_line
        assert "<name>" in actual_line, (
            "the binary's own literal text must contain the four characters "
            f"'<name>' verbatim for this proof to mean anything; got {actual_line!r}"
        )
        assert self.roadmap not in actual_line, (
            "a real roadmap name leaked into a message where COMMANDS.md "
            "documents literal '<name>' text -- the placeholder rule broke"
        )
        REACHED.add("Error: no roadmap selected: use -r <name> or --roadmap <name>")

        # Direction 2: N IS a genuine placeholder, and this module's chosen
        # value is what appears -- proved by driving the SAME template with
        # two DIFFERENT lengths and requiring each captured line to carry
        # its OWN number, not a single hardcoded one that happens to match
        # once.
        for length in (51, 73):
            name = "n" * length
            rc, out, err = self.run_stdin(["roadmap", "create", name])
            actual_line = err.splitlines()[0] if err else ""
            expected = subst(
                "Error: Roadmap name must not exceed 50 characters (got N)",
                {"N": str(length)},
            )
            assert rc == 6, f"length {length}: expected exit 6, got {rc}; stderr={err!r}"
            assert actual_line == expected, (
                f"length {length}: expected {expected!r}, got {actual_line!r}"
            )
        REACHED.add("Error: Roadmap name must not exceed 50 characters (got N)")

    # ------------------------------------------------------------------
    # Exemptions and final coverage accounting. test_zz_coverage_report is
    # named to sort alphabetically last among this class's test_* methods
    # (see _run_all(), which iterates sorted(dir(cls))), so it runs only
    # after every other check in this module has had the chance to mark
    # its own key reached.
    # ------------------------------------------------------------------

    def test_yy_exemptions_are_named(self):
        """Every exemption and every tail narrowing is declared by name with
        its reason (NON-VACUITY requirement 3d): nothing is silently dropped
        from the corpus, and nothing is silently weakened either."""
        for key, reason in EXEMPT_KEYS.items():
            assert key in CORPUS, (
                f"EXEMPT_KEYS names a string extraction no longer finds: {key!r}"
            )
            assert reason.strip(), f"exemption for {key!r} carries no reason"
            assert key not in TAIL_EXEMPT_KEYS, (
                f"{key!r} is declared both as a full exemption and as a tail "
                f"narrowing; it must be one or the other"
            )
            print(f"  EXEMPT: {key!r}\n    reason: {reason}")

        for key, (token, reason) in TAIL_EXEMPT_KEYS.items():
            assert key in CORPUS, (
                f"TAIL_EXEMPT_KEYS names a string extraction no longer finds: {key!r}"
            )
            assert reason.strip(), f"tail narrowing for {key!r} carries no reason"
            head, sep, tail = key.partition(token)
            assert sep, (
                f"TAIL_EXEMPT_KEYS declares placeholder {token!r} for {key!r}, "
                f"which does not contain it"
            )
            assert tail == "", (
                f"the narrowed placeholder must end the published string; "
                f"{key!r} carries {tail!r} after {token!r}"
            )
            # The narrowing is only legitimate while the ASSERTED half still
            # carries the sentinel: if a future edit moved the placeholder to
            # the front, this entry would be asserting nothing but "Error: ".
            assert head.startswith("Error: ") and len(head) > len("Error: "), (
                f"the asserted head of {key!r} is {head!r}, which carries no "
                f"sentinel -- the narrowing would assert nothing of substance"
            )
            assert key in REACHED, (
                f"{key!r} declares a tail narrowing but was never driven "
                f"against the binary; a narrowing is not an exemption -- the "
                f"head must actually be asserted by some case in this module"
            )
            print(f"  TAIL-NARROWED at {token}: {key!r}\n    asserted head: "
                  f"{head!r}\n    reason: {reason}")

    def test_zz_coverage_report(self):
        exempted = set(EXEMPT_KEYS.keys())
        narrowed = set(TAIL_EXEMPT_KEYS.keys())
        accounted = REACHED | exempted
        missing = sorted(set(CORPUS.keys()) - accounted)
        stale_exemptions = sorted((exempted | narrowed) - set(CORPUS.keys()))

        print(f"\nSPEC/COMMANDS.md published-string corpus: {len(CORPUS)} distinct strings")
        print(f"  reached (driven against the binary and matched exactly): {len(REACHED - narrowed)}")
        print(f"  reached, compared by head only (tail narrowed):          {len(REACHED & narrowed)}")
        print(f"  exempted (named, reasoned, never executed):               {len(exempted)}")
        print(f"  accounted for:                                            {len(accounted)}")

        assert not stale_exemptions, (
            f"EXEMPT_KEYS/TAIL_EXEMPT_KEYS name strings no longer in CORPUS: "
            f"{stale_exemptions}"
        )
        assert narrowed <= REACHED, (
            f"tail-narrowed strings that were never driven: "
            f"{sorted(narrowed - REACHED)}"
        )
        assert not missing, (
            f"{len(missing)} published string(s) are neither reached nor exempted "
            f"-- a gap in this module's own coverage, not a silent pass:\n" +
            "\n".join(f"  - {m!r} (lines {CORPUS[m]})" for m in missing)
        )

        # NON-VACUITY requirement 3b's floor: comfortably below the actual
        # total (currently well over 115) so an unrelated future SPEC edit
        # does not flap this test, but high enough that an extraction which
        # finds nothing -- or finds only a handful -- cannot pass silently.
        # See this module's development notes for the executed proof that
        # breaking extraction trips this floor instead of a vacuous pass.
        floor = 100
        assert len(REACHED) >= floor, (
            f"only {len(REACHED)} strings were reached, below the floor of {floor}; "
            f"extraction or the case list likely regressed"
        )
        print(f"  floor: {floor} (met: {len(REACHED) >= floor})")


def _run_all():
    cls = TestErrorStringParity
    methods = sorted(m for m in dir(cls) if m.startswith("test_"))
    passed = 0
    failed = 0
    failures = []
    for name in methods:
        instance = cls()
        instance.setup_method()
        try:
            getattr(instance, name)()
            passed += 1
        except AssertionError as exc:
            failed += 1
            failures.append((name, exc))
            print(f"✗ {name}")
        except Exception as exc:  # noqa: BLE001
            failed += 1
            failures.append((name, exc))
            print(f"✗ {name} (error: {type(exc).__name__}: {exc})")
        finally:
            instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Error string parity tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
