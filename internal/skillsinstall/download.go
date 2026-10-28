package skillsinstall

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The prefix under which the archive of main carries the skills
// (SPEC/SKILLS.md § Install Archive Validation, item 3).
const skillsPrefix = "Groadmap-main/skills/"

// regularTypeLegacy is the legacy tar type of a regular file, written as a zero
// byte (SPEC/SKILLS.md § Install Archive Validation, item 4).
const regularTypeLegacy byte = 0

// Errors that mark which bound of SPEC/SKILLS.md § Install Source, item 6, a
// stream crossed. They are internal markers: the published line replaces them.
var (
	errBodyTooLarge     = errors.New("the response body exceeds its bound")
	errStreamTooLarge   = errors.New("the decompressed stream exceeds its bound")
	errRedirectScheme   = errors.New("refusing a redirect to a URL whose scheme is not https")
	errTooManyRedirects = errors.New("stopped after 10 redirects")
	errNotHTTPS         = errors.New("refusing a URL whose scheme is not https")
	errNotADirectory    = errors.New("is not a directory")
	errNotDirOrFile     = errors.New("is neither a directory nor a regular file")
)

// readError marks a failure of the network stream itself, as opposed to a
// failure to decode what the stream delivered.
type readError struct{ err error }

func (e *readError) Error() string { return e.err.Error() }
func (e *readError) Unwrap() error { return e.err }

// boundedReader stops a stream at limit bytes and returns over once more is
// offered. When markReads is set, a failure of the underlying reader other
// than the end of the stream is wrapped in a readError.
type boundedReader struct {
	r         io.Reader
	over      error
	n         int64
	limit     int64
	markReads bool
}

func (b *boundedReader) Read(p []byte) (int, error) {
	if b.n > b.limit {
		return 0, b.over
	}
	if room := b.limit - b.n + 1; int64(len(p)) > room {
		p = p[:room]
	}
	n, err := b.r.Read(p)
	b.n += int64(n)
	if b.n > b.limit {
		return n - int(b.n-b.limit), b.over
	}
	if err != nil && b.markReads && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		err = &readError{err: err}
	}
	return n, err
}

// download fetches the source once and extracts the two skills into staging
// (SPEC/SKILLS.md § Install Source, § Install Staging Directory, and § Install
// Archive Validation). A signal received while the response is being read
// cancels the request; the caller then finds it pending.
func (in *installer) download(staging string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The watcher is owned by this call and joined before it returns, so it
	// never reads the signal channel concurrently with pending.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	got := make(chan os.Signal, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case s := <-in.sigCh:
			got <- s
			cancel()
		case <-stop:
		}
	}()

	err := in.fetch(ctx, staging)

	close(stop)
	wg.Wait()
	select {
	case s := <-got:
		in.sig = s
	default:
	}
	return err
}

// downloadError renders a failed request with its library diagnostic.
func (in *installer) downloadError(detail string) error {
	return fmt.Errorf("%w: cannot download the skills from %s: %s", utils.ErrIO, in.opts.Source, detail)
}

// fetch performs the request and reads the whole response.
func (in *installer) fetch(ctx context.Context, staging string) error {
	timeout := in.opts.timeout
	if timeout == 0 {
		timeout = downloadTimeout
	}
	client := &http.Client{
		Transport: in.opts.Transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errRedirectScheme
			}
			if len(via) >= maxRedirects {
				return errTooManyRedirects
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.opts.Source, http.NoBody)
	if err != nil {
		return in.downloadError(err.Error())
	}
	if req.URL.Scheme != "https" {
		return in.downloadError(errNotHTTPS.Error())
	}
	resp, err := client.Do(req)
	if err != nil {
		return in.downloadError(err.Error())
	}
	defer func() {
		// The body has been read to its end or abandoned on a failure that is
		// already being reported; a close error adds nothing to either.
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return in.downloadError(fmt.Sprintf("HTTP status %d", resp.StatusCode))
	}
	if resp.ContentLength > maxBodyBytes {
		return in.streamError(errBodyTooLarge)
	}

	body := &boundedReader{r: resp.Body, limit: maxBodyBytes, over: errBodyTooLarge, markReads: true}
	gz, err := gzip.NewReader(body)
	if err != nil {
		return in.streamError(err)
	}
	stream := &boundedReader{r: gz, limit: maxStreamBytes, over: errStreamTooLarge}
	x := &extractor{root: staging}
	if err := x.run(tar.NewReader(stream)); err != nil {
		return in.streamError(err)
	}
	// The archive is read to its end, so the gzip trailer's checks run over
	// the whole stream (SPEC/SKILLS.md § Install Source, item 7).
	if err := drain(stream); err != nil {
		return in.streamError(err)
	}
	if err := gz.Close(); err != nil {
		return in.streamError(err)
	}
	return x.complete()
}

// drain reads r to its end.
func drain(r io.Reader) error {
	buf := make([]byte, 32<<10)
	for {
		_, err := r.Read(buf)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// streamError renders a failure met while reading the response body. A
// published error (an archive refusal) passes through unchanged.
func (in *installer) streamError(err error) error {
	var published *archiveError
	var re *readError
	switch {
	case errors.As(err, &published):
		return published.err
	case errors.Is(err, errBodyTooLarge):
		return fmt.Errorf("%w: the skills archive exceeds 64 MiB; nothing was installed", utils.ErrIO)
	case errors.Is(err, errStreamTooLarge):
		return fmt.Errorf("%w: the skills archive expands to more than 256 MiB; nothing was installed", utils.ErrIO)
	case errors.As(err, &re):
		return in.downloadError(re.err.Error())
	default:
		return fmt.Errorf("%w: the skills archive is not a valid gzip-compressed tar archive: %v", utils.ErrIO, err)
	}
}

// archiveError carries a published refusal through the tar loop.
type archiveError struct{ err error }

func (e *archiveError) Error() string { return e.err.Error() }
func (e *archiveError) Unwrap() error { return e.err }

func malformed(format string, args ...any) error {
	return &archiveError{err: fmt.Errorf("%w: the skills archive is malformed: %s", utils.ErrIO, fmt.Sprintf(format, args...))}
}

func extractFailure(err error) error {
	return &archiveError{err: fmt.Errorf("%w: cannot extract the skills archive into the staging directory: %v", utils.ErrIO, err)}
}

// extractor writes the selected entries of the archive under root.
type extractor struct {
	root    string
	bytes   int64
	entries int
}

// run reads every entry of the archive.
func (x *extractor) run(tr *tar.Reader) error {
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		// A non-local name is reported by tar only under a GODEBUG setting; the
		// name rules below judge every name themselves, with the published
		// lines, so the header is used either way.
		if errors.Is(err, tar.ErrInsecurePath) && hdr != nil {
			err = nil
		}
		if err != nil {
			return err
		}
		if err := x.entry(hdr, tr); err != nil {
			return err
		}
	}
}

// entry validates one entry and extracts it when it is selected.
func (x *extractor) entry(hdr *tar.Header, r io.Reader) error {
	// A pax global extended header is metadata about the archive, not an
	// entry (item 1).
	if hdr.Typeflag == tar.TypeXGlobalHeader {
		return nil
	}
	name := hdr.Name
	if strings.HasPrefix(name, "/") {
		return malformed("entry %q has an absolute name", name)
	}
	for _, c := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		if c == "" || c == "." || c == ".." {
			return malformed("entry %q has an empty, \".\" or \"..\" path component", name)
		}
	}

	rel, ok := selectEntry(strings.TrimSuffix(name, "/"))
	if !ok {
		return nil
	}
	for _, c := range strings.Split(rel, "/") {
		if strings.HasPrefix(c, ".") {
			return nil // not part of a skill's file set
		}
	}

	isDir := hdr.Typeflag == tar.TypeDir
	if !isDir && hdr.Typeflag != tar.TypeReg && hdr.Typeflag != regularTypeLegacy {
		return malformed("entry %q is neither a directory nor a regular file", name)
	}

	x.entries++
	if x.entries > maxSelectedEntries {
		return &archiveError{err: fmt.Errorf("%w: the skills in the archive exceed 1,000 entries; nothing was installed", utils.ErrIO)}
	}

	if isDir {
		return x.mkdirs(rel, name)
	}

	if hdr.Size < 0 || x.bytes+hdr.Size > maxSelectedBytes {
		return &archiveError{err: fmt.Errorf("%w: the skills in the archive exceed 16 MiB; nothing was installed", utils.ErrIO)}
	}
	x.bytes += hdr.Size
	if dir := filepath.Dir(filepath.FromSlash(rel)); dir != "." {
		if err := x.mkdirs(filepath.ToSlash(dir), ""); err != nil {
			return err
		}
	}
	return x.writeFile(rel, name, r)
}

// selectEntry returns the relative name of an entry under one of the two
// skills, or false when the entry is not selected (item 3).
func selectEntry(trimmed string) (string, bool) {
	for _, skill := range skillNames {
		top := skillsPrefix + skill
		if trimmed == top || strings.HasPrefix(trimmed, top+"/") {
			return strings.TrimPrefix(trimmed, skillsPrefix), true
		}
	}
	return "", false
}

// mkdirs creates every directory of rel under root with mode 0700, including
// a parent a file's entry implies (item 6). entryName, when not empty, is the
// directory entry being extracted, so a name that already names a file is
// reported as a duplicate.
func (x *extractor) mkdirs(rel, entryName string) error {
	path := x.root
	parts := strings.Split(rel, "/")
	for i, c := range parts {
		path = filepath.Join(path, c)
		err := mkdirMode(path, privateDirMode)
		if err == nil {
			continue
		}
		if !errors.Is(err, fs.ErrExist) {
			return extractFailure(err)
		}
		fi, lerr := os.Lstat(path)
		if lerr != nil {
			return extractFailure(lerr)
		}
		if !fi.IsDir() {
			if i == len(parts)-1 && entryName != "" {
				return malformed("entry %q appears more than once", entryName)
			}
			return extractFailure(fmt.Errorf("%s %w", path, errNotADirectory))
		}
	}
	return nil
}

// writeFile creates one regular file exclusively, mode 0600, with the entry's
// content.
func (x *extractor) writeFile(rel, entryName string, r io.Reader) error {
	path := filepath.Join(x.root, filepath.FromSlash(rel))
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if errors.Is(err, fs.ErrExist) {
		return malformed("entry %q appears more than once", entryName)
	}
	if err != nil {
		return extractFailure(err)
	}
	if err := f.Chmod(privateFileMode); err != nil {
		_ = f.Close() // the chmod failure is the one reported
		return extractFailure(err)
	}
	if err := copyStream(f, r); err != nil {
		_ = f.Close() // the copy failure is the one reported
		return err
	}
	if err := f.Close(); err != nil {
		return extractFailure(err)
	}
	return nil
}

// copyStream copies r to f. A failure to write is an extraction failure; a
// failure to read is the stream's and is returned unchanged for streamError.
func copyStream(f *os.File, r io.Reader) error {
	buf := make([]byte, 32<<10)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return extractFailure(werr)
			}
		}
		if errors.Is(rerr, io.EOF) {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// complete checks that both skills and their SKILL.md were extracted (item 7).
func (x *extractor) complete() error {
	for _, skill := range skillNames {
		dir, derr := os.Lstat(filepath.Join(x.root, skill))
		fi, ferr := os.Lstat(filepath.Join(x.root, skill, "SKILL.md"))
		if derr != nil || !dir.IsDir() || ferr != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("%w: the skills archive is malformed: it carries no regular file skills/%s/SKILL.md", utils.ErrIO, skill)
		}
	}
	return nil
}
