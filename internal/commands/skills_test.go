package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/skillsinstall"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The handler of `rmp skills install` refuses every flag but --help and every
// positional argument before it creates the staging directory or makes a
// request (SPEC/COMMANDS.md § Skills Install Options). These tests point TMPDIR
// and the working directory at directories they own and assert that nothing
// was created in either.

func skillsRefusalEnv(t *testing.T) (tmp, cwd string) {
	t.Helper()
	tmp = filepath.Join(t.TempDir(), "tmp")
	cwd = filepath.Join(t.TempDir(), "checkout")
	for _, d := range []string{tmp, cwd} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TMPDIR", tmp)
	t.Chdir(cwd)
	return tmp, cwd
}

func requireEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("%s holds %v after a refusal", dir, entries)
	}
}

func TestSkillsInstall_RefusesBeforeAnything(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"install", "--yes"}, "invalid input: unknown flag: --yes"},
		{[]string{"install", "--force"}, "invalid input: unknown flag: --force"},
		{[]string{"install", "--yes=true"}, "invalid input: unknown flag: --yes"},
		{[]string{"install", "-r", "payments"}, "invalid input: unknown flag: -r"},
		{[]string{"install", "--roadmap", "payments"}, "invalid input: unknown flag: --roadmap"},
		{[]string{"install", "roadmap-manager"}, `invalid input: unexpected argument "roadmap-manager"`},
		{[]string{"install", "knowledge-authority", "--yes"}, `invalid input: unexpected argument "knowledge-authority"`},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.args), func(t *testing.T) {
			tmp, cwd := skillsRefusalEnv(t)
			err := dispatchFamily("skills", c.args)
			if err == nil || err.Error() != c.want || !errors.Is(err, utils.ErrInvalidInput) {
				t.Fatalf("dispatch %v = %v, want %q carrying ErrInvalidInput", c.args, err, c.want)
			}
			requireEmptyDir(t, tmp)
			requireEmptyDir(t, cwd)
		})
	}
}

func TestSkillsInstall_RelativeHomeIsRefusedBeforeAnything(t *testing.T) {
	tmp, cwd := skillsRefusalEnv(t)
	t.Setenv("HOME", "relative-home")
	err := dispatchFamily("skills", []string{"install"})
	want := `database error: home directory "relative-home" is not an absolute path; refusing to locate the data directory under it`
	if err == nil || err.Error() != want || !errors.Is(err, utils.ErrDatabase) {
		t.Fatalf("err = %v, want %q", err, want)
	}
	requireEmptyDir(t, tmp)
	requireEmptyDir(t, cwd)
}

func TestSkillsInstall_RelativeTempDirIsRefusedBeforeTheDownload(t *testing.T) {
	_, cwd := skillsRefusalEnv(t)
	t.Setenv("TMPDIR", "relative-staging-parent")
	err := dispatchFamily("skills", []string{"install"})
	want := `I/O error: cannot create a private staging directory: the temporary-file directory "relative-staging-parent" is not an absolute path`
	if err == nil || err.Error() != want || !errors.Is(err, utils.ErrIO) {
		t.Fatalf("err = %v, want %q", err, want)
	}
	requireEmptyDir(t, cwd)
}

func TestInterrupted(t *testing.T) {
	if _, ok := Interrupted(errors.New("plain")); ok {
		t.Fatal("a plain error was reported as an interruption")
	}
	if _, ok := Interrupted(nil); ok {
		t.Fatal("nil was reported as an interruption")
	}
	rollback := fmt.Errorf("%w: the skills replacement failed and could not be rolled back: x", utils.ErrIO)
	for _, c := range []struct {
		err  error
		want error
	}{
		{&skillsinstall.InterruptedError{Signal: syscall.SIGINT}, nil},
		{fmt.Errorf("wrapped: %w", &skillsinstall.InterruptedError{Signal: syscall.SIGTERM, Err: rollback}), rollback},
	} {
		got, ok := Interrupted(c.err)
		if !ok || !errors.Is(got, c.want) || (c.want == nil) != (got == nil) {
			t.Fatalf("Interrupted(%v) = %v, %v; want %v, true", c.err, got, ok, c.want)
		}
	}
}
