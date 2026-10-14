package dictation

import (
	"archive/tar"
	"bufio"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// job is one running model download (the engine first when it is missing).
type job struct {
	cancel context.CancelFunc
	done   chan struct{}
	// received and total cover the whole job, engine included.
	received, total atomic.Int64
	// withEngine reports that total includes the engine archive. Only the job's goroutine touches it.
	withEngine bool
}

// dlError is a download failure: class goes to the audit, msg to the user.
type dlError struct {
	class, msg string
	err        error
}

func (e *dlError) Error() string { return e.msg }
func (e *dlError) Unwrap() error { return e.err }

const (
	msgNetwork   = "Couldn't reach the download server. Check the internet connection and try again."
	msgStalled   = "The download stopped receiving data. Check the internet connection and try again."
	msgCut       = "The download was interrupted. Try again to continue where it stopped."
	msgCorrupted = "The download was corrupted and has been deleted. Try again."
	msgDiskFull  = "The disk is full. Free some space and try again."
	msgDisk      = "Couldn't save the download on this computer. Try again."
)

var errUnsupported = Fail(CodeUnsupported, "Dictation needs Windows on an x64 processor.")

// Download starts downloading model id in the background (with the engine when it is missing).
func (s *Service) Download(ctx context.Context, id string) (VoiceStatus, error) {
	m, ok := s.model(id)
	if !ok {
		return VoiceStatus{}, errUnknownModel
	}
	if !s.supported {
		return VoiceStatus{}, errUnsupported
	}
	engine := s.engineInstalled()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return VoiceStatus{}, Fail(CodeCanceled, "Orole is shutting down.")
	}
	if s.jobs[id] != nil || s.modelReady(m) {
		s.mu.Unlock()
		return s.Status(ctx), nil
	}
	jctx, cancel := context.WithCancel(s.ctx)
	j := &job{cancel: cancel, done: make(chan struct{}), withEngine: !engine}
	total := m.SizeBytes()
	if !engine {
		total += s.engine.Archive.Size
	}
	j.total.Store(total)
	delete(s.failed, id)
	s.jobs[id] = j
	s.wg.Add(1)
	s.mu.Unlock()
	go s.run(jctx, m, j)
	s.publish()
	return s.Status(ctx), nil
}

// Cancel stops a download. Its partial files are kept so the next Download resumes.
func (s *Service) Cancel(ctx context.Context, id string) (VoiceStatus, error) {
	if _, ok := s.model(id); !ok {
		return VoiceStatus{}, errUnknownModel
	}
	s.stopJob(id)
	return s.Status(ctx), nil
}

// stopJob cancels id's download and waits for it to finish.
func (s *Service) stopJob(id string) {
	s.mu.Lock()
	j := s.jobs[id]
	if j != nil {
		j.cancel()
	}
	s.mu.Unlock()
	if j != nil {
		<-j.done
	}
}

// run performs a job. Its preference change and audit happen before it leaves the job list, so a status that
// no longer shows the download already shows its outcome.
func (s *Service) run(ctx context.Context, m Model, j *job) {
	defer s.wg.Done()
	err := s.fetchModel(ctx, m, j)
	canceled := err != nil && ctx.Err() != nil // ctx ends only through Cancel, Remove or Close
	var de *dlError
	switch {
	case err == nil:
		perr := s.updatePrefs(context.WithoutCancel(ctx), func(p *Preferences) bool {
			if cur, ok := s.model(p.Model); ok && s.modelReady(cur) {
				return false
			}
			p.Model = m.ID
			return true
		})
		if perr != nil {
			s.log.Warn("dictation model not selected after download", "model", m.ID, "err", perr)
		}
		s.audit("dictation.download", m.ID, "allowed", fmt.Sprintf("%d bytes", j.total.Load()))
	case canceled:
		s.audit("dictation.download", m.ID, "canceled", "")
	default:
		if !errors.As(err, &de) {
			de = &dlError{class: "disk", msg: msgDisk, err: err}
		}
		s.log.Warn("dictation download failed", "model", m.ID, "class", de.class, "err", de.err)
		s.audit("dictation.download", m.ID, "failed", de.class)
	}
	s.mu.Lock()
	delete(s.jobs, m.ID)
	if de != nil {
		s.failed[m.ID] = de.msg
	}
	s.mu.Unlock()
	close(j.done)
	s.publish()
}

// fetchModel installs the engine when missing, then downloads and verifies every file of m and moves them
// to models/<id>.
func (s *Service) fetchModel(ctx context.Context, m Model, j *job) error {
	if err := s.installEngine(ctx, j); err != nil {
		return err
	}
	dl := s.downloadDir(m.ID)
	if err := os.MkdirAll(dl, 0o700); err != nil {
		return diskErr(err)
	}
	for _, f := range m.Files {
		part := filepath.Join(dl, f.Name+".part")
		// A file an earlier run verified but did not move yet goes back to .part, where it is hashed again.
		if _, err := os.Stat(filepath.Join(dl, f.Name)); err == nil {
			if err := os.Rename(filepath.Join(dl, f.Name), part); err != nil {
				return diskErr(err)
			}
		}
		if err := s.fetch(ctx, j, f, part); err != nil {
			return err
		}
	}
	for _, f := range m.Files {
		if err := os.Rename(filepath.Join(dl, f.Name+".part"), filepath.Join(dl, f.Name)); err != nil {
			return diskErr(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(s.dir, "models"), 0o700); err != nil {
		return diskErr(err)
	}
	if err := os.RemoveAll(s.modelDir(m.ID)); err != nil {
		return diskErr(err)
	}
	if err := os.Rename(dl, s.modelDir(m.ID)); err != nil {
		return diskErr(err)
	}
	return nil
}

// installEngine downloads the engine archive and extracts its DLLs to engine/<version> unless they are there.
func (s *Service) installEngine(ctx context.Context, j *job) error {
	select {
	case s.engineSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.engineSem }()
	size := s.engine.Archive.Size
	if s.engineInstalled() {
		if j.withEngine { // another job installed it meanwhile
			j.total.Add(-size)
			j.withEngine = false
		}
		return nil
	}
	if !j.withEngine {
		j.total.Add(size)
		j.withEngine = true
	}
	staging := s.engineStaging()
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return diskErr(err)
	}
	archive := filepath.Join(staging, "archive.part")
	if err := s.fetch(ctx, j, s.engine.Archive, archive); err != nil {
		return err
	}
	out := filepath.Join(staging, "lib")
	if err := os.RemoveAll(out); err != nil {
		return diskErr(err)
	}
	if err := s.extractEngine(archive, out); err != nil {
		return err
	}
	final := s.engineDir()
	if err := os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
		return diskErr(err)
	}
	if err := os.RemoveAll(final); err != nil {
		return diskErr(err)
	}
	if err := os.Rename(out, final); err != nil {
		return diskErr(err)
	}
	s.removeAll(staging)
	return nil
}

// extractEngine copies the engine DLLs from the verified archive into dir.
func (s *Service) extractEngine(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return diskErr(err)
	}
	defer f.Close()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return diskErr(err)
	}
	want := map[string]bool{}
	for _, name := range s.engine.DLLs {
		want[name] = true
	}
	corrupted := func(err error) error {
		os.Remove(archive)
		return &dlError{class: "corrupted", msg: msgCorrupted, err: err}
	}
	tr := tar.NewReader(bzip2.NewReader(bufio.NewReader(f)))
	for len(want) > 0 {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return corrupted(err)
		}
		name, ok := strings.CutPrefix(strings.TrimPrefix(hdr.Name, "./"), s.engine.Prefix)
		if !ok || !want[name] || !hdr.FileInfo().Mode().IsRegular() {
			continue
		}
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return diskErr(err)
		}
		w := &errWriter{w: out}
		n, err := io.Copy(w, tr)
		if cerr := out.Close(); w.err == nil && cerr != nil {
			w.err = cerr
		}
		if w.err != nil {
			return diskErr(w.err)
		}
		if err != nil {
			return corrupted(err)
		}
		if n != hdr.Size {
			return corrupted(fmt.Errorf("%s: %d of %d bytes", name, n, hdr.Size))
		}
		delete(want, name)
	}
	if len(want) > 0 {
		return corrupted(fmt.Errorf("archive lacks %d engine files", len(want)))
	}
	return nil
}

// fetch downloads f into part, continuing from the bytes already there, and verifies size and SHA-256.
// A corrupted file is deleted; an interrupted one is kept for the next attempt.
func (s *Service) fetch(ctx context.Context, j *job, f File, part string) error {
	h := sha256.New()
	have, err := hashExisting(part, f.Size, h)
	if err != nil {
		return diskErr(err)
	}
	j.received.Add(have)
	s.progress()
	if have == f.Size {
		return verify(part, f, h, have)
	}

	reqCtx, stop := context.WithCancel(ctx)
	defer stop()
	var stalled atomic.Bool
	watchdog := time.AfterFunc(s.stall, func() { stalled.Store(true); stop() })
	defer watchdog.Stop()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, f.URL, nil)
	if err != nil {
		return &dlError{class: "network", msg: msgNetwork, err: err}
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return netErr(ctx, &stalled, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK: // the server ignored the range: start over
		j.received.Add(-have)
		have = 0
		h.Reset()
	case http.StatusRequestedRangeNotSatisfiable:
		return verify(part, f, h, have)
	default:
		return &dlError{
			class: fmt.Sprintf("http_%d", resp.StatusCode),
			msg:   fmt.Sprintf("The download server answered %d %s. Try again later.", resp.StatusCode, http.StatusText(resp.StatusCode)),
		}
	}

	flags := os.O_WRONLY | os.O_CREATE | os.O_APPEND
	if have == 0 {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	out, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return diskErr(err)
	}
	body := io.LimitReader(resp.Body, f.Size-have+1) // one byte over the pinned size is enough to reject it
	buf := make([]byte, 256<<10)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			watchdog.Reset(s.stall)
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				return diskErr(werr)
			}
			h.Write(buf[:n])
			have += int64(n)
			j.received.Add(int64(n))
			s.progress()
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return netErr(ctx, &stalled, rerr)
		}
	}
	if err := out.Close(); err != nil {
		return diskErr(err)
	}
	if have < f.Size {
		return &dlError{class: "network", msg: msgCut, err: io.ErrUnexpectedEOF}
	}
	return verify(part, f, h, have)
}

// hashExisting feeds an existing partial download into h and returns its length. A file longer than size
// cannot be resumed and is deleted.
func hashExisting(part string, size int64, h hash.Hash) (int64, error) {
	f, err := os.Open(part)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if fi.Size() > size {
		f.Close()
		return 0, os.Remove(part)
	}
	return io.Copy(h, f)
}

// verify checks a complete download against its pin and deletes it when it does not match.
func verify(part string, f File, h hash.Hash, have int64) error {
	if have == f.Size && hex.EncodeToString(h.Sum(nil)) == strings.ToLower(f.SHA256) {
		return nil
	}
	os.Remove(part)
	return &dlError{class: "corrupted", msg: msgCorrupted, err: fmt.Errorf("%s does not match its pinned size and SHA-256", f.Name)}
}

// netErr classifies a failed request or body read. A cancelled job returns its context's error.
func netErr(ctx context.Context, stalled *atomic.Bool, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if stalled.Load() {
		return &dlError{class: "stalled", msg: msgStalled, err: err}
	}
	return &dlError{class: "network", msg: msgNetwork, err: err}
}

// diskErr classifies a local file failure.
func diskErr(err error) error {
	if isDiskFull(err) {
		return &dlError{class: "disk_full", msg: msgDiskFull, err: err}
	}
	return &dlError{class: "disk", msg: msgDisk, err: err}
}

// errWriter remembers a write failure so a copy can tell it from a read failure.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	n, err := e.w.Write(p)
	if err != nil {
		e.err = err
	}
	return n, err
}
