// Package aihelp — agent-skills alignment gate (SPEC/SKILLS.md § Enforcement).
//
// TestSkills_AlignedWithContract runs the checker of
// skills_alignment_check_test.go against the skills/ directory of this
// repository, the whole-CLI contract this package generates, and the version
// constant of cmd/rmp/main.go. The remaining tests drive the same checker
// against temporary fixtures built from a small synthetic contract, so each
// failure the specification names is proved to be reported — the gate cannot
// pass vacuously.

package aihelp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// skillRepoPath resolves a path relative to the repository root; this package lives
// in internal/aihelp.
func skillRepoPath(parts ...string) string {
	return filepath.Join(append([]string{"..", ".."}, parts...)...)
}

// TestSkills_AlignedWithContract is the gate: the shipped skills must satisfy
// every rule of SPEC/SKILLS.md § Alignment Invariant against the contract and
// binary version of this working tree.
func TestSkills_AlignedWithContract(t *testing.T) {
	version, err := readBinaryVersion(skillRepoPath("cmd", "rmp", "main.go"))
	if err != nil {
		t.Fatalf("cannot read the binary version: %v", err)
	}
	out, err := Generate(ScopeAll(), testInfo())
	if err != nil {
		t.Fatalf("Generate(ScopeAll()) returned error: %v", err)
	}
	var contract Contract
	if err := json.Unmarshal(out, &contract); err != nil {
		t.Fatalf("contract does not unmarshal: %v", err)
	}
	for _, msg := range checkSkillAlignment(skillRepoPath("skills"), &contract, version) {
		t.Error(msg)
	}
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

func fixtureFlags(longs ...string) []FlagEntry {
	out := make([]FlagEntry, 0, len(longs))
	for _, l := range longs {
		out = append(out, FlagEntry{Long: l})
	}
	return out
}

func fixtureSub(name string, aliases []string, longs ...string) SubcommandEntry {
	return SubcommandEntry{Name: name, Aliases: aliases, Flags: fixtureFlags(longs...)}
}

// fixtureContract is a reduced contract carrying every command the partition
// names, with a handful of subcommands and flags. It is independent of the
// real registry, so these tests pin the checker, not the CLI.
func fixtureContract() *Contract {
	return &Contract{Commands: []CommandEntry{
		{Name: "roadmap", Aliases: []string{"road"}, Subcommands: []SubcommandEntry{
			fixtureSub("list", []string{"ls"}, "--help"),
		}},
		{Name: "task", Aliases: []string{"t"}, Subcommands: []SubcommandEntry{
			fixtureSub("list", []string{"ls"}, "--roadmap", "--type", "--help"),
			fixtureSub("stat", []string{"set-status"}, "--roadmap", "--commit-open", "--help"),
		}},
		{Name: "sprint", Aliases: []string{"s"}, Subcommands: []SubcommandEntry{
			fixtureSub("reorder", []string{"order"}, "--roadmap", "--help"),
		}},
		{Name: "backlog", Aliases: []string{"bl"}, Subcommands: []SubcommandEntry{
			fixtureSub("show-next", nil, "--roadmap", "--help"),
		}},
		{Name: "audit", Aliases: []string{"aud"}, Subcommands: []SubcommandEntry{
			fixtureSub("history", []string{"hist"}, "--roadmap", "--help"),
		}},
		{Name: "stats", Subcommands: []SubcommandEntry{fixtureSub("stats", nil, "--roadmap", "--help")}},
		{Name: "graph", Subcommands: []SubcommandEntry{
			fixtureSub("serve", nil, "--roadmap", "--socket", "--help"),
			fixtureSub("client", nil, "--roadmap", "--query", "--socket", "--help"),
		}},
		{Name: "web", Subcommands: []SubcommandEntry{fixtureSub("web", nil, "--port", "--help")}},
		{Name: "ai-help", Subcommands: []SubcommandEntry{fixtureSub("ai-help", nil, "--help")}},
	}}
}

const fixtureVersion = "9.8.7"

func fixtureFrontmatter(name, version string) string {
	return "---\nname: " + name + "\ndescription: Operates rmp for a fixture.\nmetadata:\n  rmp-version: \"" + version + "\"\n---\n\n"
}

// fixtureRoadmapManager is a minimal, fully aligned roadmap-manager SKILL.md.
const fixtureRoadmapManagerBody = "# Roadmap manager\n\n" +
	"```bash\n" +
	"rmp roadmap list\n" +
	"rmp task list -r <roadmap> --roadmap <roadmap> --type BUG\n" +
	"rmp task stat <id> DOING -r <roadmap> --commit-open <hash>\n" +
	"rmp sprint reorder <sid> <csv> -r <roadmap>\n" +
	"rmp backlog show-next -r <roadmap>\n" +
	"rmp audit history TASK <id> -r <roadmap>\n" +
	"rmp stats -r <roadmap>\n" +
	"```\n\n" +
	"Every subcommand accepts `--help`. The knowledge graph (`rmp graph …`) belongs to knowledge-authority.\n"

const fixtureKnowledgeAuthorityBody = "# Knowledge authority\n\n" +
	"```bash\n" +
	"rmp graph serve -r <roadmap> --socket <path>\n" +
	"rmp graph client -r <roadmap> --roadmap <roadmap> --query \"RETURN 1\" --socket <path>\n" +
	"```\n\n" +
	"Use `--help` for details, and `rmp ai-help` for the contract.\n"

// writeSkillFixture builds an aligned skills/ tree in a temporary directory and
// returns its path.
func writeSkillFixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "skills")
	writeSkillFile(t, filepath.Join(dir, "roadmap-manager", "SKILL.md"), fixtureFrontmatter("roadmap-manager", fixtureVersion)+fixtureRoadmapManagerBody)
	writeSkillFile(t, filepath.Join(dir, "knowledge-authority", "SKILL.md"), fixtureFrontmatter("knowledge-authority", fixtureVersion)+fixtureKnowledgeAuthorityBody)
	return dir
}

func writeSkillFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readSkillFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// editSkillFixture replaces old with new in one fixture file, failing when old is
// absent so a mutation that silently does nothing cannot pass.
func editSkillFixture(t *testing.T, path, old, replacement string) {
	t.Helper()
	s := readSkillFile(t, path)
	if !strings.Contains(s, old) {
		t.Fatalf("fixture %s does not contain %q", path, old)
	}
	writeSkillFile(t, path, strings.ReplaceAll(s, old, replacement))
}

func appendSkillFixture(t *testing.T, path, text string) {
	t.Helper()
	writeSkillFile(t, path, readSkillFile(t, path)+text)
}

// requireSkillViolation fails unless some message contains every fragment.
func requireSkillViolation(t *testing.T, msgs []string, fragments ...string) {
	t.Helper()
	for _, m := range msgs {
		all := true
		for _, f := range fragments {
			if !strings.Contains(m, f) {
				all = false
				break
			}
		}
		if all {
			return
		}
	}
	t.Fatalf("no violation contains all of %q; got:\n%s", fragments, strings.Join(msgs, "\n"))
}

func checkFixture(dir string) []string {
	return checkSkillAlignment(dir, fixtureContract(), fixtureVersion)
}

// ---------------------------------------------------------------------------
// The fixture itself passes, so every failure below is caused by its mutation.
// ---------------------------------------------------------------------------

func TestSkillsCheck_AlignedFixturePasses(t *testing.T) {
	if msgs := checkFixture(writeSkillFixture(t)); len(msgs) != 0 {
		t.Fatalf("aligned fixture reported violations:\n%s", strings.Join(msgs, "\n"))
	}
}

// ---------------------------------------------------------------------------
// Rule 1 — every subcommand is named (SPEC/SKILLS.md acceptance criterion 4).
// ---------------------------------------------------------------------------

func TestSkillsCheck_MissingSubcommandFails(t *testing.T) {
	dir := writeSkillFixture(t)
	editSkillFixture(t, filepath.Join(dir, "roadmap-manager", "SKILL.md"), "rmp sprint reorder <sid> <csv> -r <roadmap>\n", "")
	requireSkillViolation(t, checkFixture(dir), "roadmap-manager", "skills/roadmap-manager/", "rule 1", "rmp sprint reorder")
}

func TestSkillsCheck_MissingSingleActionCommandFails(t *testing.T) {
	dir := writeSkillFixture(t)
	editSkillFixture(t, filepath.Join(dir, "roadmap-manager", "SKILL.md"), "rmp stats -r <roadmap>\n", "")
	requireSkillViolation(t, checkFixture(dir), "roadmap-manager", "rule 1", "rmp stats written")
}

func TestSkillsCheck_AliasDoesNotSatisfyRuleOne(t *testing.T) {
	dir := writeSkillFixture(t)
	editSkillFixture(t, filepath.Join(dir, "roadmap-manager", "SKILL.md"), "rmp sprint reorder", "rmp s order")
	requireSkillViolation(t, checkFixture(dir), "rule 1", "rmp sprint reorder")
}

func TestSkillsCheck_ProseDoesNotSatisfyRuleOne(t *testing.T) {
	dir := writeSkillFixture(t)
	path := filepath.Join(dir, "roadmap-manager", "SKILL.md")
	editSkillFixture(t, path, "rmp sprint reorder <sid> <csv> -r <roadmap>\n", "")
	appendSkillFixture(t, path, "\nIn prose, rmp sprint reorder is not code.\n")
	requireSkillViolation(t, checkFixture(dir), "rule 1", "rmp sprint reorder")
}

// ---------------------------------------------------------------------------
// Rule 2 — every flag is named (acceptance criterion 5).
// ---------------------------------------------------------------------------

func TestSkillsCheck_MissingFlagFails(t *testing.T) {
	dir := writeSkillFixture(t)
	editSkillFixture(t, filepath.Join(dir, "knowledge-authority", "SKILL.md"), " --socket <path>", "")
	requireSkillViolation(t, checkFixture(dir), "knowledge-authority", "skills/knowledge-authority/", "rule 2", "--socket")
}

func TestSkillsCheck_LongerFlagDoesNotSatisfyShorterOne(t *testing.T) {
	dir := writeSkillFixture(t)
	editSkillFixture(t, filepath.Join(dir, "roadmap-manager", "SKILL.md"), "--type BUG", "--types BUG")
	requireSkillViolation(t, checkFixture(dir), "roadmap-manager", "rule 2", "--type")
}

func TestSkillsCheck_FlagInProseSatisfiesRuleTwo(t *testing.T) {
	dir := writeSkillFixture(t)
	path := filepath.Join(dir, "roadmap-manager", "SKILL.md")
	editSkillFixture(t, path, " --commit-open <hash>", "")
	appendSkillFixture(t, path, "\nThe DOING transition takes --commit-open, always.\n")
	if msgs := checkFixture(dir); len(msgs) != 0 {
		t.Fatalf("a flag named in prose must satisfy rule 2; got:\n%s", strings.Join(msgs, "\n"))
	}
}

// ---------------------------------------------------------------------------
// Rule 3 — nothing absent is invoked (acceptance criterion 6).
// ---------------------------------------------------------------------------

func TestSkillsCheck_UnknownSubcommandFails(t *testing.T) {
	for _, skill := range []string{"roadmap-manager", "knowledge-authority"} {
		t.Run(skill, func(t *testing.T) {
			dir := writeSkillFixture(t)
			path := filepath.Join(dir, skill, "SKILL.md")
			appendSkillFixture(t, path, "\nNever run `rmp graph execute`.\n")
			line := strings.Count(readSkillFile(t, path), "\n")
			requireSkillViolation(t, checkFixture(dir), skill, "rule 3", `"rmp graph execute"`,
				skill+"/SKILL.md:"+strconv.Itoa(line))
		})
	}
}

func TestSkillsCheck_UnknownCommandFails(t *testing.T) {
	dir := writeSkillFixture(t)
	writeSkillFile(t, filepath.Join(dir, "roadmap-manager", "references", "cli.md"), "```\nrmp bogus thing -r x\n```\n")
	requireSkillViolation(t, checkFixture(dir), "roadmap-manager", "rule 3", `"rmp bogus thing"`, "roadmap-manager/references/cli.md:2")
}

func TestSkillsCheck_InvocationInNonMarkdownFileIsRecognised(t *testing.T) {
	dir := writeSkillFixture(t)
	writeSkillFile(t, filepath.Join(dir, "knowledge-authority", "references", "probe.sh"), "#!/bin/sh\nrmp graph search -r x\n")
	requireSkillViolation(t, checkFixture(dir), "knowledge-authority", "rule 3", `"rmp graph search"`, "probe.sh:2")
}

// ---------------------------------------------------------------------------
// Rule 4 — the partition holds (acceptance criteria 7 and 8).
// ---------------------------------------------------------------------------

func TestSkillsCheck_ForbiddenInvocationFails(t *testing.T) {
	cases := []struct{ skill, span, text string }{
		{"roadmap-manager", "`rmp graph client -r <roadmap>`", "rmp graph client"},
		{"knowledge-authority", "`rmp task list -r <roadmap>`", "rmp task list"},
		{"roadmap-manager", "`rmp web`", "rmp web"},
		{"knowledge-authority", "`rmp web`", "rmp web"},
	}
	for _, c := range cases {
		t.Run(c.skill+"/"+c.text, func(t *testing.T) {
			dir := writeSkillFixture(t)
			appendSkillFixture(t, filepath.Join(dir, c.skill, "SKILL.md"), "\nSee "+c.span+".\n")
			requireSkillViolation(t, checkFixture(dir), c.skill, "rule 4", `"`+c.text+`"`)
		})
	}
}

func TestSkillsCheck_MentionsAndSharedCommandDoNotFail(t *testing.T) {
	dir := writeSkillFixture(t)
	appendSkillFixture(t, filepath.Join(dir, "roadmap-manager", "SKILL.md"),
		"\nThe graph (`rmp graph …`, `rmp graph`, `rmp task <subcommand>`, `rmp task --help`, `rmp --ai-help`) is not ours; run `rmp ai-help`.\n")
	appendSkillFixture(t, filepath.Join(dir, "knowledge-authority", "SKILL.md"), "\nRun `rmp ai-help`.\n")
	if msgs := checkFixture(dir); len(msgs) != 0 {
		t.Fatalf("mentions and rmp ai-help must not fail; got:\n%s", strings.Join(msgs, "\n"))
	}
}

// ---------------------------------------------------------------------------
// Rule 5 — the version is declared and current (acceptance criterion 9).
// ---------------------------------------------------------------------------

func TestSkillsCheck_VersionMismatchFails(t *testing.T) {
	msgs := checkSkillAlignment(writeSkillFixture(t), fixtureContract(), "9.8.8")
	requireSkillViolation(t, msgs, "roadmap-manager", "rule 5", "9.8.7", "9.8.8")
	requireSkillViolation(t, msgs, "knowledge-authority", "rule 5", "9.8.7", "9.8.8")
}

func TestSkillsCheck_MalformedVersionDeclarationFails(t *testing.T) {
	cases := map[string]string{
		"leading v":        "  rmp-version: \"v9.8.7\"",
		"unquoted":         "  rmp-version: 9.8.7",
		"wrong indent":     "    rmp-version: \"9.8.7\"",
		"two components":   "  rmp-version: \"9.8\"",
		"outside metadata": "rmp-version: \"9.8.7\"",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			dir := writeSkillFixture(t)
			editSkillFixture(t, filepath.Join(dir, "knowledge-authority", "SKILL.md"), "  rmp-version: \"9.8.7\"", line)
			requireSkillViolation(t, checkFixture(dir), "knowledge-authority", "rule 5", "no well-formed version declaration")
		})
	}
}

func TestSkillsCheck_DuplicateVersionDeclarationFails(t *testing.T) {
	dir := writeSkillFixture(t)
	editSkillFixture(t, filepath.Join(dir, "roadmap-manager", "SKILL.md"), "  rmp-version: \"9.8.7\"\n", "  rmp-version: \"9.8.7\"\n  rmp-version: \"9.8.7\"\n")
	requireSkillViolation(t, checkFixture(dir), "roadmap-manager", "rule 5", "exactly one")
}

func TestSkillsCheck_ReadBinaryVersion(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.go")
	writeSkillFile(t, good, "package main\n\nconst (\n\tversion = \"1.2.3\"\n)\n")
	if v, err := readBinaryVersion(good); err != nil || v != "1.2.3" {
		t.Fatalf("readBinaryVersion = %q, %v; want 1.2.3", v, err)
	}
	none := filepath.Join(dir, "none.go")
	writeSkillFile(t, none, "package main\n\nvar version = \"1.2.3\"\n")
	if _, err := readBinaryVersion(none); err == nil {
		t.Fatal("a file with no version constant must be an error")
	}
	msgs := checkSkillAlignment(writeSkillFixture(t), fixtureContract(), "")
	requireSkillViolation(t, msgs, "binary version could not be read")
}

// ---------------------------------------------------------------------------
// Structure — SPEC/SKILLS.md § Location and Identity items 1, 2, 3 and 5.
// ---------------------------------------------------------------------------

func TestSkillsCheck_Structure(t *testing.T) {
	t.Run("missing skills directory", func(t *testing.T) {
		requireSkillViolation(t, checkFixture(filepath.Join(t.TempDir(), "skills")), "skills/", "cannot be read")
	})
	t.Run("missing skill", func(t *testing.T) {
		dir := writeSkillFixture(t)
		if err := os.RemoveAll(filepath.Join(dir, "knowledge-authority")); err != nil {
			t.Fatal(err)
		}
		requireSkillViolation(t, checkFixture(dir), "knowledge-authority", "skill directory is missing")
	})
	t.Run("extra skill", func(t *testing.T) {
		dir := writeSkillFixture(t)
		writeSkillFile(t, filepath.Join(dir, "other-skill", "SKILL.md"), "x")
		requireSkillViolation(t, checkFixture(dir), "skills/", `"other-skill"`)
	})
	t.Run("hidden entries are ignored", func(t *testing.T) {
		dir := writeSkillFixture(t)
		writeSkillFile(t, filepath.Join(dir, ".DS_Store"), "x")
		writeSkillFile(t, filepath.Join(dir, "roadmap-manager", ".hidden", "notes.md"), "`rmp web`")
		if msgs := checkFixture(dir); len(msgs) != 0 {
			t.Fatalf("hidden entries must be ignored; got:\n%s", strings.Join(msgs, "\n"))
		}
	})
	t.Run("missing SKILL.md", func(t *testing.T) {
		dir := writeSkillFixture(t)
		path := filepath.Join(dir, "roadmap-manager", "SKILL.md")
		writeSkillFile(t, filepath.Join(dir, "roadmap-manager", "PDS.md"), readSkillFile(t, path))
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		requireSkillViolation(t, checkFixture(dir), "roadmap-manager", "SKILL.md is missing")
	})
	t.Run("no frontmatter", func(t *testing.T) {
		dir := writeSkillFixture(t)
		editSkillFixture(t, filepath.Join(dir, "roadmap-manager", "SKILL.md"), "---\nname:", "--\nname:")
		requireSkillViolation(t, checkFixture(dir), "roadmap-manager", "no frontmatter")
	})
	t.Run("name differs", func(t *testing.T) {
		dir := writeSkillFixture(t)
		editSkillFixture(t, filepath.Join(dir, "knowledge-authority", "SKILL.md"), "name: knowledge-authority", "name: ka")
		requireSkillViolation(t, checkFixture(dir), "knowledge-authority", "frontmatter name")
	})
	t.Run("empty description", func(t *testing.T) {
		dir := writeSkillFixture(t)
		editSkillFixture(t, filepath.Join(dir, "roadmap-manager", "SKILL.md"), "description: Operates rmp for a fixture.", "description: \"\"")
		requireSkillViolation(t, checkFixture(dir), "roadmap-manager", "description is empty")
	})
	t.Run("unexpected entry in a skill", func(t *testing.T) {
		dir := writeSkillFixture(t)
		writeSkillFile(t, filepath.Join(dir, "knowledge-authority", "PDS.md"), "x")
		requireSkillViolation(t, checkFixture(dir), "knowledge-authority", "item 2", `"PDS.md"`)
	})
	t.Run("vacuous contract", func(t *testing.T) {
		msgs := checkSkillAlignment(writeSkillFixture(t), &Contract{}, fixtureVersion)
		requireSkillViolation(t, msgs, "vacuous")
	})
}

// ---------------------------------------------------------------------------
// Recognition — SPEC/SKILLS.md § Invocation.
// ---------------------------------------------------------------------------

func TestSkillsRecognition(t *testing.T) {
	cli := newSkillCLI(fixtureContract())
	found := func(name, content string) []string {
		var out []string
		for _, seg := range recognisedSegments(name, content) {
			for _, inv := range findInvocations(cli, seg, name) {
				out = append(out, inv.text())
			}
		}
		return out
	}
	cases := []struct {
		name, file, content string
		want                []string
	}{
		{"prose is not scanned", "a.md", "Run rmp task list now.\n", nil},
		{"inline span", "a.md", "Run `rmp task list -r x` now.\n", []string{"rmp task list"}},
		{"double-backtick span", "a.md", "Run ``rmp t ls`` now.\n", []string{"rmp t ls"}},
		{"unmatched backtick is literal", "a.md", "a ` rmp task list\n", nil},
		{"tilde fence", "a.md", "~~~\nrmp sprint reorder 1 2\n~~~\nrmp task list\n", []string{"rmp sprint reorder"}},
		{"unclosed fence runs to the end", "a.md", "```\nx\nrmp task stat 1\n", []string{"rmp task stat"}},
		{"shorter run does not close a fence", "a.md", "````\n```\nrmp task list\n````\n", []string{"rmp task list"}},
		{"single action is complete", "a.md", "`rmp stats -r x`", []string{"rmp stats"}},
		{"mention without subcommand", "a.md", "`rmp graph …` and `rmp graph` and `rmp task --help`", nil},
		{"word boundaries", "a.md", "`xrmp task list`, `rmp_conf task list`, `rmp-skills task list`", nil},
		{"two invocations on one line", "a.md", "`rmp task list && rmp graph client -q x`", []string{"rmp task list", "rmp graph client"}},
		{"whole text of other files", "x.sh", "rmp graph serve -r x\n", []string{"rmp graph serve"}},
		{"unknown command with subcommand", "a.md", "`rmp graph execute`", []string{"rmp graph execute"}},
		{"placeholder is neither", "a.md", "`rmp <command> <sub>`", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := found(c.file, c.content)
			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Fatalf("found %q, want %q", got, c.want)
			}
		})
	}
}
