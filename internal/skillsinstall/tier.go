package skillsinstall

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// errReappeared reports a skill path that is present again after its previous
// copy was moved aside, before the new copy was moved into it.
var errReappeared = errors.New("the path reappeared before the new copy could be moved into it")

// swapStep records what the swap has done for one skill, so the rollback can
// undo exactly that.
type swapStep struct {
	skill     string
	movedOld  bool
	installed bool
}

// tryTier attempts one tier (SPEC/SKILLS.md § Tier Replacement Procedure). It
// returns "" and nil when both skills were installed, the tier's reason when
// the tier failed and the next may be tried, or an error that ends the run: a
// failed rollback or an interruption.
func (in *installer) tryTier(t *Tier, staging string) (string, error) {
	if t.Unavailable != "" {
		return t.Unavailable, nil
	}
	if reason := in.checkDestination(t); reason != "" {
		return reason, nil
	}
	if reason := createMissing(t); reason != "" {
		return reason, nil
	}

	work, err := os.MkdirTemp(t.SkillsDir, workPattern)
	if err != nil {
		return fmt.Sprintf("cannot create a work directory in %s: %v", t.SkillsDir, err), nil
	}
	if reason := in.privateDirReason(work); reason != "" {
		// Left in place, never removed (step 2).
		return reason, nil
	}

	if s := in.pending(); s != nil {
		in.removeTemp(work)
		return "", &InterruptedError{Signal: s}
	}
	if err := copyTree(staging, filepath.Join(work, "new"), t.DirMode, t.FileMode); err != nil {
		in.removeTemp(work)
		return fmt.Sprintf("cannot copy the skills into %s: %v", work, err), nil
	}
	if err := mkdirMode(filepath.Join(work, "old"), privateDirMode); err != nil {
		in.removeTemp(work)
		return fmt.Sprintf("cannot copy the skills into %s: %v", work, err), nil
	}
	if s := in.pending(); s != nil {
		in.removeTemp(work)
		return "", &InterruptedError{Signal: s}
	}

	return in.swap(t, work)
}

// swap replaces both skills, rolling back on a failure or a signal (steps 4
// to 7, and SPEC/SKILLS.md § Install Interruption).
func (in *installer) swap(t *Tier, work string) (string, error) {
	steps := make([]swapStep, 0, len(skillNames))
	var failedPath string
	var failure error
	var interrupted os.Signal

	for i, skill := range skillNames {
		path := filepath.Join(t.SkillsDir, skill)
		steps = append(steps, swapStep{skill: skill})
		st := &steps[len(steps)-1]

		if _, err := os.Lstat(path); err == nil {
			if err := in.rename(path, filepath.Join(work, "old", skill)); err != nil {
				failedPath, failure = path, err
				break
			}
			st.movedOld = true
			if s := in.pending(); s != nil {
				interrupted = s
				break
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			failedPath, failure = path, err
			break
		}

		if _, err := os.Lstat(path); err == nil {
			failedPath, failure = path, errReappeared
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			failedPath, failure = path, err
			break
		}
		if err := in.rename(filepath.Join(work, "new", skill), path); err != nil {
			failedPath, failure = path, err
			break
		}
		st.installed = true
		// After the last rename the tier's installation is complete: a signal
		// received now no longer undoes it (item 4).
		if i+1 < len(skillNames) {
			if s := in.pending(); s != nil {
				interrupted = s
				break
			}
		}
	}

	if failure == nil && interrupted == nil {
		in.removeTemp(work)
		return "", nil
	}

	undoErrs, kept := in.rollback(t, work, steps)
	if len(undoErrs) > 0 {
		cause := "the run was interrupted by " + signalName(interrupted)
		if failure != nil {
			cause = failure.Error()
		}
		detail := strings.Join(append([]string{cause + "; undoing it failed: " + strings.Join(undoErrs, "; ")}, kept...), "; ")
		fatal := fmt.Errorf("%w: the skills replacement failed and could not be rolled back: %s", utils.ErrIO, detail)
		if interrupted != nil {
			return "", &InterruptedError{Signal: interrupted, Err: fatal}
		}
		return "", fatal
	}

	in.removeTemp(work)
	if interrupted != nil {
		return "", &InterruptedError{Signal: interrupted}
	}
	return fmt.Sprintf("cannot replace %s, and the previous skills were restored: %v", failedPath, failure), nil
}

// rollback undoes every completed swap step in reverse order (step 5). It
// returns the undo steps that failed and, for each previous copy that could
// not be restored, the path in the work directory that still holds it.
func (in *installer) rollback(t *Tier, work string, steps []swapStep) (undoErrs, kept []string) {
	for i := len(steps) - 1; i >= 0; i-- {
		s := steps[i]
		path := filepath.Join(t.SkillsDir, s.skill)
		oldPath := filepath.Join(work, "old", s.skill)
		if s.installed {
			if err := in.rename(path, filepath.Join(work, "new", s.skill)); err != nil {
				undoErrs = append(undoErrs, err.Error())
				if s.movedOld {
					kept = append(kept, fmt.Sprintf("the previous %s is kept at %s", s.skill, oldPath))
				}
				continue
			}
		}
		if s.movedOld {
			if err := in.rename(oldPath, path); err != nil {
				undoErrs = append(undoErrs, err.Error())
				kept = append(kept, fmt.Sprintf("the previous %s is kept at %s", s.skill, oldPath))
			}
		}
	}
	return undoErrs, kept
}

// signalName names a signal for a composed detail.
func signalName(s os.Signal) string {
	if s == nil {
		return "a signal"
	}
	return s.String()
}

// checkDestination applies SPEC/SKILLS.md § Tier Destination Rules, rules 2 to
// 6 (rule 1 is resolved into Tier.Unavailable). It returns the reason the
// destination is refused, or "".
func (in *installer) checkDestination(t *Tier) string {
	for _, dir := range []string{t.ConfigDir, t.SkillsDir} {
		fi, err := os.Stat(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Sprintf("cannot examine %s: %v", dir, err)
		}
		if !fi.IsDir() {
			return dir + " is not a directory"
		}
		if dir != t.SkillsDir {
			continue
		}
		if reason := in.ownerReason(fi, dir, t.AcceptRoot); reason != "" {
			return reason
		}
		if reason := worldWritableReason(fi, dir); reason != "" {
			return reason
		}
	}
	for _, skill := range skillNames {
		path := filepath.Join(t.SkillsDir, skill)
		fi, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Sprintf("cannot examine %s: %v", path, err)
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return path + " is a symbolic link"
		}
		if !fi.IsDir() {
			return path + " is not a directory"
		}
		if reason := in.ownerReason(fi, path, t.AcceptRoot); reason != "" {
			return reason
		}
	}
	return ""
}

// createMissing creates each missing directory of the tier with the tier's
// directory mode. It returns the reason the tier fails, or "".
func createMissing(t *Tier) string {
	for _, dir := range t.Create {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			continue
		}
		if err := mkdirMode(dir, t.DirMode); err != nil {
			return fmt.Sprintf("cannot create %s: %v", dir, err)
		}
	}
	return ""
}

// mkdirMode creates one directory and sets its mode explicitly, so the
// process umask does not alter it.
func mkdirMode(dir string, mode os.FileMode) error {
	if err := os.Mkdir(dir, mode); err != nil {
		return err
	}
	return os.Chmod(dir, mode)
}

// copyTree copies the validated tree src into dst, which must not exist, with
// the given modes. Files are created exclusively, so none is written through a
// symbolic link (step 3).
func copyTree(src, dst string, dirMode, fileMode os.FileMode) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return mkdirMode(target, dirMode)
		case d.Type().IsRegular():
			return copyFile(path, target, fileMode)
		default:
			return fmt.Errorf("%s %w", path, errNotDirOrFile)
		}
	})
}

// copyFile copies one regular file into a new file created exclusively.
func copyFile(src, dst string, mode os.FileMode) (err error) {
	in, err := os.Open(filepath.Clean(src))
	if err != nil {
		return err
	}
	defer func() {
		// Only read from; a close error loses nothing.
		_ = in.Close()
	}()
	out, err := os.OpenFile(filepath.Clean(dst), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if err := out.Chmod(mode); err != nil {
		_ = out.Close() // the chmod failure is the one reported
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close() // the copy failure is the one reported
		return err
	}
	return out.Close()
}
