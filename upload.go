package orbits

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Limits of an upload project's source, as the API documents them.
const (
	// MaxUploadBytes is the largest zip the API takes (100 MiB).
	MaxUploadBytes = 100 << 20
	// MaxUnpackedBytes is the most a project may be once unpacked (500 MiB).
	MaxUnpackedBytes = 500 << 20
	// MaxUploadFiles is the most files a project may have.
	MaxUploadFiles = 20000
)

// Errors ZipDir and UploadDir return (wrapped) when a folder breaks the API's limits.
var (
	ErrUploadTooLarge  = errors.New("orbits: the project is larger than an upload may be")
	ErrTooManyFiles    = errors.New("orbits: the project has more files than an upload may have")
	ErrUnsupportedFile = errors.New("orbits: uploads take only plain files and folders")
	ErrNothingToUpload = errors.New("orbits: there are no files to upload")
	ErrNotAFolder      = errors.New("orbits: not a folder")
)

// ZipOptions tune ZipDir.
type ZipOptions struct {
	// Ignore adds patterns, in .orbitsignore syntax, to DefaultIgnore and the folder's .orbitsignore.
	Ignore []string
	// OnFile, when set, is called with each file added, by its slash-separated path in the zip and its size.
	OnFile func(path string, size int64)
}

// ZipStats says what ZipDir packed.
type ZipStats struct {
	// Files is how many files the zip holds.
	Files int
	// Bytes is their size unpacked.
	Bytes int64
}

// ZipDir writes a zip of the folder dir to w, as an upload project's source: the folder's contents at the zip's
// root, without DefaultIgnore, the patterns of the folder's .orbitsignore and opts.Ignore. It enforces the
// unpacked limits (MaxUnpackedBytes, MaxUploadFiles) and refuses links and special files (ErrUnsupportedFile)
// with the path, which .orbitsignore can then leave out. opts may be nil.
func ZipDir(w io.Writer, dir string, opts *ZipOptions) (ZipStats, error) {
	o := deref(opts)
	var stats ZipStats
	info, err := os.Stat(dir)
	if err != nil {
		return stats, fmt.Errorf("orbits: %w", err)
	}
	if !info.IsDir() {
		return stats, fmt.Errorf("%w: %s", ErrNotAFolder, dir)
	}
	lines := append([]string{}, DefaultIgnore...)
	if f, err := os.Open(filepath.Join(dir, IgnoreFile)); err == nil {
		more, err := readIgnoreLines(f)
		f.Close()
		if err != nil {
			return stats, fmt.Errorf("orbits: reading %s: %w", IgnoreFile, err)
		}
		lines = append(lines, more...)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return stats, fmt.Errorf("orbits: reading %s: %w", IgnoreFile, err)
	}
	lines = append(lines, o.Ignore...)
	ignore := newIgnoreMatcher(lines)

	zw := zip.NewWriter(w)
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if ignore.ignored(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			kind := "a special file"
			if d.Type()&fs.ModeSymlink != 0 {
				kind = "a link"
			}
			return fmt.Errorf("%w: %s is %s; remove it or add it to %s", ErrUnsupportedFile, rel, kind, IgnoreFile)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		stats.Files++
		stats.Bytes += fi.Size()
		if stats.Files > MaxUploadFiles {
			return fmt.Errorf("%w: more than %d (add folders that are not source, such as build output, to %s)", ErrTooManyFiles, MaxUploadFiles, IgnoreFile)
		}
		if stats.Bytes > MaxUnpackedBytes {
			return fmt.Errorf("%w: more than %d MiB unpacked (add what is not source to %s)", ErrUploadTooLarge, MaxUnpackedBytes>>20, IgnoreFile)
		}
		hdr, err := zip.FileInfoHeader(fi)
		if err != nil {
			return err
		}
		hdr.Name = rel
		hdr.Method = zip.Deflate
		fw, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, f)
		f.Close()
		if err != nil {
			return fmt.Errorf("orbits: zipping %s: %w", rel, err)
		}
		if o.OnFile != nil {
			o.OnFile(rel, fi.Size())
		}
		return nil
	})
	if walkErr != nil {
		_ = zw.Close()
		return stats, walkErr
	}
	if stats.Files == 0 {
		_ = zw.Close()
		return stats, fmt.Errorf("%w in %s", ErrNothingToUpload, dir)
	}
	if err := zw.Close(); err != nil {
		return stats, fmt.Errorf("orbits: writing the zip: %w", err)
	}
	return stats, nil
}

// UploadOptions tune UploadDir and UploadZip.
type UploadOptions struct {
	// Filename names the upload in the deployment's message ("Uploaded site.zip"). The default is the folder's
	// (or zip's) name.
	Filename string
	// Ignore adds patterns, in .orbitsignore syntax, to DefaultIgnore and the folder's .orbitsignore.
	Ignore []string
	// OnFile, when set, is called with each file added to the zip.
	OnFile func(path string, size int64)
	// OnProgress, when set, is called as the zip is sent, with the bytes sent so far and the total.
	OnProgress func(sent, total int64)
	// IdempotencyKey, when set, is the Idempotency-Key of the call that starts the upload, so a caller that retries
	// the whole deploy (after a confirmation, say) gets the same upload back instead of a second one. The default is
	// a new key per call. It does not apply to completing the upload, which is asked again on purpose.
	IdempotencyKey string
}

// completeTries bounds how often CompleteUpload is asked again while storage has not seen the whole file.
const completeTries = 10

// UploadDir deploys a folder to an upload project (one created with SourceKind upload): it zips the folder (see
// ZipDir), starts an upload, PUTs the zip to the presigned URL, completes the upload, and returns the queued
// production deployment. Follow it with Wait or StreamLogs. opts may be nil.
//
// A Git project answers 409 not_an_upload_project; deploy it with Create or Redeploy, or by pushing.
func (s *DeploymentsService) UploadDir(ctx context.Context, team, project, dir string, opts *UploadOptions) (*Deployment, error) {
	o := deref(opts)
	tmp, err := os.CreateTemp("", "orbits-upload-*.zip")
	if err != nil {
		return nil, fmt.Errorf("orbits: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := ZipDir(tmp, dir, &ZipOptions{Ignore: o.Ignore, OnFile: o.OnFile}); err != nil {
		return nil, err
	}
	size, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, fmt.Errorf("orbits: %w", err)
	}
	if o.Filename == "" {
		abs, _ := filepath.Abs(dir)
		o.Filename = filepath.Base(abs) + ".zip"
	}
	return s.UploadZip(ctx, team, project, tmp, size, &o)
}

// UploadZip deploys a zip that is ready, of size bytes, to an upload project: it starts an upload, PUTs the zip
// to the presigned URL, completes the upload, and returns the queued production deployment. A single top-level
// folder in the zip is stripped by Orbits, so zipping a folder or its contents both work. If sending the file
// fails, the upload is cancelled. opts may be nil (opts.Ignore and opts.OnFile do not apply).
func (s *DeploymentsService) UploadZip(ctx context.Context, team, project string, zip io.ReaderAt, size int64, opts *UploadOptions) (*Deployment, error) {
	o := deref(opts)
	if size <= 0 {
		return nil, ErrNothingToUpload
	}
	if size > MaxUploadBytes {
		return nil, fmt.Errorf("%w: the zip is %d MiB, and may be up to %d MiB", ErrUploadTooLarge, size>>20, MaxUploadBytes>>20)
	}
	req := StartUploadRequest{Size: size}
	if o.Filename != "" {
		name := o.Filename
		if len(name) > 255 {
			name = name[:255]
		}
		req.Filename = &name
	}
	var startOpts []RequestOption
	if o.IdempotencyKey != "" {
		startOpts = append(startOpts, WithIdempotencyKey(o.IdempotencyKey))
	}
	up, err := s.StartUpload(ctx, team, project, req, startOpts...)
	if err != nil {
		return nil, err
	}
	cancel := func() {
		// Best effort, even when ctx is already done.
		cctx, done := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer done()
		_ = s.CancelUpload(cctx, team, project, up.ID)
	}
	if up.MaxBytes > 0 && size > up.MaxBytes {
		cancel()
		return nil, fmt.Errorf("%w: the zip is %d bytes, and may be up to %d", ErrUploadTooLarge, size, up.MaxBytes)
	}
	if err := s.putArchive(ctx, up, zip, size, o.OnProgress); err != nil {
		cancel()
		return nil, err
	}
	for try := 1; ; try++ {
		d, err := s.CompleteUpload(ctx, team, project, up.ID)
		if err == nil {
			return d, nil
		}
		if !IsCode(err, "upload_incomplete") || try >= completeTries {
			return nil, err
		}
		if err := s.c.sleep(ctx, time.Second); err != nil {
			return nil, fmt.Errorf("orbits: completing the upload: %w", err)
		}
	}
}

// putArchive sends the zip to the presigned URL with exactly the headers it was signed with, and nothing else:
// no Authorization, no cookies. A connection error or a 5xx is tried again, twice.
func (s *DeploymentsService) putArchive(ctx context.Context, up *SourceUpload, zip io.ReaderAt, size int64, progress func(sent, total int64)) error {
	method := up.Method
	if method == "" {
		method = http.MethodPut
	}
	hc := s.c.streamClient()
	const tries = 3
	var lastErr error
	for attempt := 0; attempt < tries; attempt++ {
		if attempt > 0 {
			if err := s.c.sleep(ctx, s.c.backoff(attempt-1, 0)); err != nil {
				return fmt.Errorf("orbits: sending the zip: %w", err)
			}
		}
		var body io.Reader = io.NewSectionReader(zip, 0, size)
		if progress != nil {
			body = &progressReader{r: body, total: size, fn: progress}
		}
		req, err := http.NewRequestWithContext(ctx, method, up.URL, body)
		if err != nil {
			return fmt.Errorf("orbits: the upload URL: %w", err)
		}
		req.ContentLength = size
		// An empty User-Agent keeps net/http from adding its own: only the signed headers go.
		req.Header = http.Header{"User-Agent": {""}}
		for k, v := range up.Headers {
			req.Header.Set(k, v)
		}
		if req.Header.Get("Content-Length") != "" {
			req.Header.Del("Content-Length") // net/http sends ContentLength itself
		}
		res, err := hc.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("orbits: sending the zip: %w", ctx.Err())
			}
			lastErr = fmt.Errorf("orbits: sending the zip: %w", redactURL(err, up.URL))
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		res.Body.Close()
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("orbits: storage refused the zip: %s%s", res.Status, storageReason(data))
		if res.StatusCode < 500 {
			return lastErr
		}
	}
	return lastErr
}

// storageReason picks the <Message> out of an object store's XML error, which is safe to show.
func storageReason(body []byte) string {
	s := string(body)
	start := strings.Index(s, "<Message>")
	end := strings.Index(s, "</Message>")
	if start < 0 || end <= start {
		return ""
	}
	return ": " + strings.TrimSpace(s[start+len("<Message>"):end])
}

// redactURL keeps the presigned URL, which works as a credential until it expires, out of an error.
func redactURL(err error, u string) error {
	msg := err.Error()
	if u != "" && strings.Contains(msg, u) {
		cut := u
		if i := strings.IndexByte(cut, '?'); i >= 0 {
			cut = cut[:i]
		}
		return errors.New(strings.ReplaceAll(msg, u, cut+"?…"))
	}
	return err
}

type progressReader struct {
	r     io.Reader
	sent  int64
	total int64
	fn    func(sent, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.sent += int64(n)
		p.fn(p.sent, p.total)
	}
	return n, err
}
