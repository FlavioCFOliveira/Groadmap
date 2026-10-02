#!/usr/bin/env python3
"""
Test 75: the user documentation publishes only error lines the SPEC publishes,
and every link inside DOCS/ resolves (rmp task #431).

Two gates, both over files rather than over the binary.

1. Error lines. Every line that `DOCS/**/*.md` and `README.md` show beginning
   `Error: ` must be a line the SPEC publishes. Nothing held the user
   documentation to the published error lines before this module: test_55 pins
   the SPEC's lines to the binary, and a page under DOCS/ could quote a line
   the binary stopped printing -- or never printed -- with every gate green.

2. Links. Every relative link and every intra-document anchor in
   `DOCS/**/*.md` resolves: the target file exists, and a fragment names a
   heading (or an explicit HTML anchor) of the target, by GitHub's anchor
   rules.

# How an error line is read

A documentation file is read EXACTLY as test_55 reads a SPEC file, with
test_55's own extractors -- extract_table_corpus, extract_fenced_corpus and
extract_prose_corpus -- imported rather than copied. They are the normaliser:
a fenced line that is a JSON string member publishes its decoded value up to
the first newline; an inline code span wrapping a double-quoted string drops
the author's quotes; a prose span wrapped across two source lines is read
whole. A second normaliser here could disagree with test_55's about what a
published line IS, and then the same text would be one line to one gate and
another line to the other. Of what the extractors yield, a string beginning
`Error: ` is a published line; any other string is not one of this gate's.

One kind of span names the prefix without publishing a line: a span whose body
after `Error: ` is an ellipsis alone (`Error: ...`), which the documentation
uses to speak of every error line at once. It is excluded as a class, by that
shape, never by listing the lines that have it.

# How a line is matched

The SPEC side is test_55's CORPUS: every string the governed SPEC files
publish, from tables, fences and prose, with the derived instances test_55
adds. A documentation line matches a corpus string when the two are equal
character for character after the corpus string's placeholders are taken as
wildcards. The placeholders are exactly the ones SPEC/COMMANDS.md § Published
Error Strings Are Exact declares, read from its table by test_55's
declared_placeholders; the single letters X, N, M and Y count only as whole
words, as they do in test_55. Everything that is not a declared placeholder is
compared literally. A placeholder stands for at least one character.

A corpus string whose body after `Error: ` is placeholders and nothing else --
SPEC/HELP.md's `Error: <detail>`, the shape of every error line -- fixes no
wording, and would match every documentation line there is. It is a shape, not
a line, so it vouches for nothing and is not used as a matcher. Like the
ellipsis rule above, this is decided by the string's shape and applies to any
corpus string of that shape.

# The handover rule: lines that end in an engine diagnostic

A corpus string that ENDS in the `<engine diagnostic>` placeholder hands the
rest of the line over to the graph engine: what follows the head is the
engine's text, not Groadmap's, and the documentation may show it as the
placeholder, as a diagnostic observed from a real run, or not at all. So for
such a string the rule is: the documentation line must reproduce the HEAD --
everything before the placeholder, trailing whitespace ignored -- exactly as
any other line is matched, and whatever follows the head, if anything, is not
compared. It is one rule, applied to every corpus string of that shape; no
documentation line is exempted by name. It is the documentation-side
counterpart of test_55's TAIL_EXEMPT_KEYS, which drives such lines against the
binary and asserts them exactly up to the tail for the same reason.

# Why there is no reverse check

This module does not require every SPEC line to appear in the documentation,
and must not. The SPEC publishes many lines that are internal to a rule --
wrap-order counter-examples, lines reached only through a corrupted database
or a raced write, per-flag instances of one template -- that user
documentation has no reason to quote. A reverse check would fail legitimately
on all of them, and could pass only with an allowlist of the lines the
documentation deliberately omits, which rots silently as the SPEC grows: every
new SPEC line would land in it by default, and the list would stop meaning
anything.

# How an anchor is resolved

A heading's anchor is GitHub's: the heading's rendered text (inline code keeps
its content, a link keeps its text, HTML tags are dropped), lower-cased; every
character that is not a letter, a mark, a number, a connector punctuation
character (the underscore), a hyphen or a space removed; each space replaced by
a hyphen, with no collapsing; and a repeated anchor suffixed `-1`, `-2`, ... in
document order. ATX and setext headings both count, as do explicit `id` and
`name` attributes. Links inside fenced blocks and inline code are not links;
links with a URI scheme or protocol-relative ones are not relative and are out
of scope.

# The gates can fail

TestTheGatesCanFail runs each check against a scratch copy of DOCS/ with one
deliberate mutation -- an error line drifted from its SPEC form, an intra-
document anchor broken, a relative file link pointing at a file that does not
exist -- and requires the finding, after requiring the unmutated copy to be
clean. A check that cannot see a mutation passes a documentation tree that has
drifted.
"""

import inspect
import os
import re
import shutil
import sys
import tempfile
import unicodedata
from pathlib import Path
from urllib.parse import unquote

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import REPO_ROOT
from tests import test_55_error_string_parity as t55


DOCS_DIR = REPO_ROOT / "DOCS"
README_PATH = REPO_ROOT / "README.md"

ERROR_PREFIX = "Error: "
ENGINE_DIAGNOSTIC = "<engine diagnostic>"

# A span whose body after the prefix is an ellipsis alone names the prefix and
# publishes no line.
_ELLIPSIS_ONLY_RE = re.compile(r"^Error: (\.\.\.|…)$")


# ---------------------------------------------------------------------------
# Error lines
# ---------------------------------------------------------------------------


def documented_error_lines(text):
    """{line: [1-based line numbers]} for every string `text` publishes that
    begins `Error: `, read with test_55's extractors (the normaliser)."""
    found = {}
    for extractor in (t55.extract_table_corpus, t55.extract_fenced_corpus,
                      t55.extract_prose_corpus):
        for s, line_nos in extractor(text).items():
            if not s.startswith(ERROR_PREFIX) or _ELLIPSIS_ONLY_RE.match(s):
                continue
            found.setdefault(s, []).extend(line_nos)
    return {s: sorted(set(nos)) for s, nos in found.items()}


def _placeholder_token(placeholders):
    """A regex matching any one declared placeholder in a corpus string:
    bracketed placeholders as substrings, X/N/M/Y as whole words only."""
    bracketed = sorted((p for p in placeholders if p not in t55._WORD_TOKENS),
                       key=len, reverse=True)
    words = sorted(p for p in placeholders if p in t55._WORD_TOKENS)
    alternatives = [re.escape(p) for p in bracketed]
    if words:
        alternatives.append(r"\b(?:" + "|".join(words) + r")\b")
    return re.compile("|".join(alternatives)) if alternatives else None


def fixes_no_wording(template, placeholders):
    """Whether `template` is the prefix followed by placeholders alone, which
    would match every line (see the module docstring)."""
    token = _placeholder_token(placeholders)
    body = template[len(ERROR_PREFIX):] if template.startswith(ERROR_PREFIX) else template
    return token is not None and not token.sub("", body).strip()


def _placeholder_regex(template, placeholders):
    """The regex source for `template` with each declared placeholder a
    wildcard of at least one character and everything else literal."""
    token = _placeholder_token(placeholders)
    if token is None:
        return re.escape(template)
    out, last = [], 0
    for m in token.finditer(template):
        out.append(re.escape(template[last:m.start()]))
        out.append(".+?")
        last = m.end()
    out.append(re.escape(template[last:]))
    return "".join(out)


def compile_corpus(corpus, placeholders):
    """[(corpus string, compiled matcher)] for every corpus string that fixes
    some wording, the handover rule applied to every string that ends in
    <engine diagnostic>."""
    compiled = []
    for template in corpus:
        if fixes_no_wording(template, placeholders):
            continue
        if template.endswith(ENGINE_DIAGNOSTIC):
            head = template[:-len(ENGINE_DIAGNOSTIC)].rstrip()
            source = _placeholder_regex(head, placeholders) + r"(?:\s.*)?"
        else:
            source = _placeholder_regex(template, placeholders)
        compiled.append((template, re.compile(source, re.DOTALL)))
    return compiled


def unmatched_error_lines(paths, compiled, root=REPO_ROOT):
    """[(file:line, line)] for every documented error line of `paths` that no
    compiled corpus string matches."""
    findings = []
    for path in paths:
        text = path.read_text(encoding="utf-8")
        for line, line_nos in sorted(documented_error_lines(text).items()):
            if any(rx.fullmatch(line) for _, rx in compiled):
                continue
            where = ", ".join(f"{path.relative_to(root)}:{n}" for n in line_nos)
            findings.append((where, line))
    return findings


def spec_placeholders():
    return t55.declared_placeholders(t55.SPEC_PATH.read_text(encoding="utf-8"))


def documentation_files(docs_dir=DOCS_DIR, readme=README_PATH):
    files = sorted(docs_dir.rglob("*.md"))
    if readme is not None:
        files.append(readme)
    return files


# ---------------------------------------------------------------------------
# Links and anchors
# ---------------------------------------------------------------------------

_FENCE_RE = re.compile(r"^\s{0,3}(```|~~~)")
_ATX_RE = re.compile(r"^\s{0,3}(#{1,6})(?:\s+(.*?))?\s*$")
_SETEXT_RE = re.compile(r"^\s{0,3}(=+|-+)\s*$")
_CODE_SPAN_RE = re.compile(r"(`+)(.+?)\1")
_INLINE_LINK_RE = re.compile(r"!?\[((?:[^\[\]\\]|\\.|\[[^\]]*\])*)\]\(\s*(<[^>]*>|[^)\s]+)(?:\s+(?:\"[^\"]*\"|'[^']*'))?\s*\)")
_REF_DEF_RE = re.compile(r"^\s{0,3}\[[^\]]+\]:\s*(<[^>]*>|\S+)")
_HTML_ID_RE = re.compile(r"<[a-zA-Z][^>]*?\s(?:id|name)\s*=\s*[\"']([^\"']+)[\"']")
_HTML_TAG_RE = re.compile(r"<[^>]+>")
_SCHEME_RE = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.\-]*:")


def _render_heading(raw):
    """A heading's rendered text: closing hashes dropped, inline code kept as
    its content, a link kept as its text, HTML tags removed."""
    text = re.sub(r"\s+#+\s*$", "", raw).strip()
    text = _CODE_SPAN_RE.sub(lambda m: m.group(2).strip(), text)
    text = re.sub(r"!?\[([^\]]*)\]\([^)]*\)", r"\1", text)
    return _HTML_TAG_RE.sub("", text)


def github_slug(heading_text):
    """GitHub's anchor for a heading whose rendered text is `heading_text`,
    before the duplicate suffix."""
    kept = []
    for ch in heading_text.lower():
        category = unicodedata.category(ch)
        if ch in " -" or category[0] in "LMN" or category == "Pc":
            kept.append(ch)
    return "".join(kept).replace(" ", "-")


def _outside_fences(lines):
    """Yield (1-based number, line) for every line outside a fenced block."""
    fence = None
    for number, line in enumerate(lines, 1):
        m = _FENCE_RE.match(line)
        if m:
            if fence is None:
                fence = m.group(1)
            elif m.group(1) == fence:
                fence = None
            continue
        if fence is None:
            yield number, line


def anchors_of(text):
    """Every anchor `text` defines: its headings' slugs, duplicates suffixed as
    GitHub suffixes them, and every explicit id or name attribute."""
    anchors, seen = set(), {}
    previous = None  # the previous outside-fence line, for setext underlines

    def add(rendered):
        base = github_slug(rendered)
        count = seen.get(base, 0)
        seen[base] = count + 1
        anchors.add(base if count == 0 else f"{base}-{count}")

    for number, line in _outside_fences(text.split("\n")):
        atx = _ATX_RE.match(line)
        setext = _SETEXT_RE.match(line)
        if atx:
            add(_render_heading(atx.group(2) or ""))
            previous = None
        elif (setext and previous is not None and previous[0] == number - 1
              and previous[1].strip() and not previous[1].lstrip().startswith(("|", ">", "-", "*", "+"))
              and not _ATX_RE.match(previous[1])):
            add(_render_heading(previous[1].strip()))
            previous = None
            continue
        else:
            previous = (number, line)
        anchors.update(_HTML_ID_RE.findall(line))
    return anchors


def links_of(text):
    """[(1-based line number, target)] for every link and link reference
    definition outside fenced blocks and inline code."""
    found = []
    for number, line in _outside_fences(text.split("\n")):
        bare = _CODE_SPAN_RE.sub("", line)
        for m in _INLINE_LINK_RE.finditer(bare):
            found.append((number, m.group(2).strip("<>")))
        ref = _REF_DEF_RE.match(bare)
        if ref:
            found.append((number, ref.group(1).strip("<>")))
    return found


def broken_links(paths, root):
    """[(file:line, target, reason)] for every relative link of `paths` whose
    file does not exist or whose fragment names no anchor of its target."""
    anchor_cache = {}

    def anchors_for(target):
        if target not in anchor_cache:
            anchor_cache[target] = anchors_of(target.read_text(encoding="utf-8"))
        return anchor_cache[target]

    findings = []
    for path in paths:
        for number, target in links_of(path.read_text(encoding="utf-8")):
            if _SCHEME_RE.match(target) or target.startswith("//"):
                continue
            file_part, _, fragment = target.partition("#")
            file_part = unquote(file_part)
            if not file_part:
                resolved = path
            elif file_part.startswith("/"):
                resolved = root / file_part.lstrip("/")
            else:
                resolved = path.parent / file_part
            where = f"{path.relative_to(root)}:{number}"
            if not resolved.exists():
                findings.append((where, target, "the target file does not exist"))
                continue
            if fragment and resolved.is_file() and resolved.suffix == ".md":
                if unquote(fragment) not in anchors_for(resolved):
                    findings.append((where, target,
                                     f"no heading or anchor of {resolved.relative_to(root)} "
                                     f"has the anchor #{fragment}"))
    return findings


def _format(findings):
    return "\n".join("  " + " | ".join(f) for f in findings)


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


class TestDocumentedErrorLinesArePublished:
    """Gate 1: every documented `Error: ` line is a SPEC-published line."""

    def setup_method(self):
        self.compiled = compile_corpus(t55.CORPUS, spec_placeholders())

    def teardown_method(self):
        pass

    def test_the_documentation_publishes_error_lines(self):
        """A gate over nothing passes vacuously: the scan must find lines."""
        total = sum(len(documented_error_lines(p.read_text(encoding="utf-8")))
                    for p in documentation_files())
        assert total > 0, (
            "no `Error: ` line was found in DOCS/ or README.md; the extraction "
            "stopped reading the documentation, and this gate checks nothing")

    def test_every_documented_error_line_is_published_by_the_spec(self):
        findings = unmatched_error_lines(documentation_files(), self.compiled)
        assert not findings, (
            f"{len(findings)} documented error line(s) match no line the SPEC "
            f"publishes (test_55's CORPUS, placeholders as wildcards, the "
            f"<engine diagnostic> handover rule applied). Correct the "
            f"documentation to the SPEC's line, or publish the line in the SPEC "
            f"first:\n{_format(findings)}")


class TestDocsLinksResolve:
    """Gate 2: every relative link and anchor under DOCS/ resolves."""

    def setup_method(self):
        pass

    def teardown_method(self):
        pass

    def test_the_docs_carry_links(self):
        total = sum(len(links_of(p.read_text(encoding="utf-8")))
                    for p in documentation_files(readme=None))
        assert total > 0, "no link was found under DOCS/; the link scan checks nothing"

    def test_every_relative_link_and_anchor_resolves(self):
        findings = broken_links(documentation_files(readme=None), REPO_ROOT)
        assert not findings, (
            f"{len(findings)} link(s) under DOCS/ do not resolve:\n{_format(findings)}")


class TestTheRulesAreTheStatedOnes:
    """The matcher and the slugger implement the rules the docstring states."""

    def setup_method(self):
        self.placeholders = spec_placeholders()

    def teardown_method(self):
        pass

    def _matches(self, template, line):
        return any(rx.fullmatch(line) for _, rx in compile_corpus([template], self.placeholders))

    def test_placeholders_are_wildcards_and_nothing_else_is(self):
        template = "Error: validation error: task #N not found in sprint #M"
        assert self._matches(template, "Error: validation error: task #42 not found in sprint #7")
        assert self._matches(template, template)
        assert not self._matches(template, "Error: validation error: task #42 not found in sprint")
        assert not self._matches(template, "Error: task #42 not found in sprint #7")
        # A single-letter placeholder is a whole word only.
        assert not self._matches("Error: status is TESTING", "Error: status is TESTI9G")

    def test_a_string_that_fixes_no_wording_matches_nothing(self):
        assert fixes_no_wording("Error: <detail>", self.placeholders)
        assert not self._matches("Error: <detail>", "Error: anything at all")
        assert not fixes_no_wording("Error: database error: <detail>", self.placeholders)
        assert self._matches("Error: database error: <detail>",
                             "Error: database error: disk I/O error")

    def test_the_handover_rule_compares_the_head_and_only_the_head(self):
        template = "Error: graph engine error: graph query failed: <engine diagnostic>"
        for tail in ("", " <engine diagnostic>", " Invalid input 'RETRUN': expected RETURN"):
            line = "Error: graph engine error: graph query failed:" + tail
            assert self._matches(template, line), f"a correct head with tail {tail!r} was refused"
        assert not self._matches(
            template, "Error: graph engine error: query failed: Invalid input"), (
            "a drifted head was accepted because the tail is free")
        assert not self._matches(
            template, "Error: graph engine error: graph query failed:Invalid"), (
            "a tail not separated from the head was accepted")

    def test_an_ellipsis_alone_publishes_no_line(self):
        text = "Every `Error: ...` line is followed by a hint; `Error: …` too.\n"
        assert documented_error_lines(text) == {}
        text = "Refused with `Error: required parameter missing: --title`.\n"
        assert list(documented_error_lines(text)) == [
            "Error: required parameter missing: --title"]

    def test_the_slugger_follows_github(self):
        cases = {
            "Through the server, some diagnostics are replaced":
                "through-the-server-some-diagnostics-are-replaced",
            "`rmp task list` — options": "rmp-task-list--options",
            "Exit codes (0-6)": "exit-codes-0-6",
            "snake_case & C++": "snake_case--c",
            "Café résumé": "café-résumé",
        }
        for heading, want in cases.items():
            got = github_slug(_render_heading(heading))
            assert got == want, f"heading {heading!r}: slug {got!r}, want {want!r}"
        text = "# Usage\n\n## Usage\n\nPlain\n-----\n\n```\n# Not a heading\n```\n<a id=\"pinned\"></a>\n"
        assert anchors_of(text) == {"usage", "usage-1", "plain", "pinned"}


class TestTheGatesCanFail:
    """Each gate refuses a deliberate mutation of a scratch copy of DOCS/."""

    def setup_method(self):
        self.scratch = Path(tempfile.mkdtemp(prefix="rmp-t75-"))
        shutil.copytree(DOCS_DIR, self.scratch / "DOCS")
        self.docs = self.scratch / "DOCS"
        self.compiled = compile_corpus(t55.CORPUS, spec_placeholders())

    def teardown_method(self):
        shutil.rmtree(self.scratch, ignore_errors=True)

    def _files(self):
        return documentation_files(self.docs, readme=None)

    def test_a_drifted_error_line_fails_the_error_gate(self):
        assert unmatched_error_lines(self._files(), self.compiled, self.scratch) == [], (
            "the unmutated scratch copy already has findings; the mutation proves nothing")
        target = self.docs / "commands" / "audit.md"
        text = target.read_text(encoding="utf-8")
        line = next(iter(documented_error_lines(text)))
        drifted = line.replace("Error: ", "Error: the ", 1)
        assert text.count(line) >= 1
        target.write_text(text.replace(line, drifted), encoding="utf-8")
        findings = unmatched_error_lines(self._files(), self.compiled, self.scratch)
        assert [f[1] for f in findings] == [drifted], (
            f"the drifted line {drifted!r} was not the one finding: {findings!r}")

    def test_a_broken_anchor_fails_the_link_gate(self):
        assert broken_links(self._files(), self.scratch) == [], (
            "the unmutated scratch copy already has findings; the mutation proves nothing")
        target = self.docs / "commands" / "graph.md"
        text = target.read_text(encoding="utf-8")
        m = re.search(r"\]\(#([^)]+)\)", text)
        assert m, "graph.md carries no intra-document link to mutate"
        broken = m.group(1) + "-drifted"
        target.write_text(text[:m.start(1)] + broken + text[m.end(1):], encoding="utf-8")
        findings = broken_links(self._files(), self.scratch)
        assert [f[1] for f in findings] == ["#" + broken], (
            f"the broken anchor #{broken} was not the one finding: {findings!r}")

    def test_a_link_to_a_missing_file_fails_the_link_gate(self):
        (self.docs / "commands" / "audit.md").rename(self.docs / "commands" / "audit-moved.md")
        findings = broken_links(self._files(), self.scratch)
        assert findings and all(f[1].startswith("audit.md") for f in findings), (
            f"a link to a removed file was not reported, or not alone: {findings!r}")


def _run_all():
    """Discover and run every Test* class defined in this module."""
    passed = failed = 0
    failures = []
    classes = [
        obj for _name, obj in sorted(inspect.getmembers(sys.modules[__name__], inspect.isclass))
        if obj.__module__ == __name__ and _name.startswith("Test")
    ]
    print(f"Discovered {len(classes)} test classes: "
          f"{', '.join(cls.__name__ for cls in classes)}")
    for cls in classes:
        for m in sorted(name for name in dir(cls) if name.startswith("test_")):
            label = f"{cls.__name__}.{m}"
            instance = cls()
            instance.setup_method()
            try:
                getattr(instance, m)()
                passed += 1
                print(f"PASS {label}")
            except AssertionError as exc:
                failed += 1
                failures.append((label, exc))
                print(f"FAIL {label}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((label, exc))
                print(f"FAIL {label} (error)")
            finally:
                instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"DOCS error-line and link tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\nFAIL {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
