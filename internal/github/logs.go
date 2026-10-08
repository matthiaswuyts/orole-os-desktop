package github

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Size limits for CI log downloads (logs are untrusted and can be huge).
const (
	MaxLogDownload = 32 << 20  // compressed bytes fetched from GitHub
	maxLogEntryRaw = 16 << 20  // uncompressed bytes read per zip entry
	maxLogTotalRaw = 64 << 20  // uncompressed bytes read per archive
	maxLogEntries  = 300       // entries considered per archive
	LogTailBytes   = 256 << 10 // text kept per file (tail)
)

// ErrLogsGone is returned when GitHub no longer has the logs (expired/deleted).
var ErrLogsGone = errors.New("github: logs are no longer available (expired or deleted)")

// logRedirect fetches path with the token but without following redirects, then
// downloads the redirect target WITHOUT credentials (the blob URL is pre-signed),
// reading at most limit bytes.
func (c *Client) logRedirect(ctx context.Context, path string, limit int64) ([]byte, error) {
	full, err := c.absURL(path, nil)
	if err != nil {
		return nil, err
	}
	if err := c.waitForKnownLimit(ctx, path); err != nil {
		return nil, err
	}
	tok, err := c.Tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	hc := *c.httpc()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Authorization", "Bearer "+tok)
	hr, err := hc.Do(req)
	if err != nil {
		return nil, errors.New(Redact(err.Error(), tok))
	}
	defer hr.Body.Close()
	c.noteRate(hr.Header)
	switch hr.StatusCode {
	case http.StatusFound, http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusSeeOther:
	case http.StatusGone, http.StatusNotFound:
		return nil, ErrLogsGone
	case http.StatusUnauthorized:
		c.Tokens.Invalidate()
		return nil, ErrUnauthorized
	default:
		if hr.StatusCode >= 200 && hr.StatusCode < 300 { // served inline
			return readLimited(hr.Body, limit)
		}
		b, _ := io.ReadAll(io.LimitReader(hr.Body, 4096))
		return nil, parseAPIError(hr.StatusCode, b, tok)
	}
	loc, err := hr.Location()
	if err != nil {
		return nil, fmt.Errorf("github: bad log redirect: %w", err)
	}
	base, _ := url.Parse(c.base())
	if loc.Scheme != "https" && loc.Host != base.Host {
		return nil, fmt.Errorf("github: refusing non-https log redirect")
	}
	dr, err := http.NewRequestWithContext(ctx, http.MethodGet, loc.String(), nil)
	if err != nil {
		return nil, err
	}
	dr.Header.Set("User-Agent", c.UserAgent)
	dh := *c.httpc()
	dh.CheckRedirect = nil
	res, err := dh.Do(dr)
	if err != nil {
		return nil, errors.New(Redact(err.Error(), tok))
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone {
		return nil, ErrLogsGone
	}
	if res.StatusCode != http.StatusOK {
		return nil, &APIError{Status: res.StatusCode, Message: "log download failed"}
	}
	return readLimited(res.Body, limit)
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("github: log download exceeds %d MB limit", limit>>20)
	}
	return b, nil
}

// tailText returns the last max bytes of b as valid UTF-8, starting at a line boundary.
func tailText(b []byte, max int) (string, bool) {
	trunc := false
	if len(b) > max {
		b = b[len(b)-max:]
		if i := bytes.IndexByte(b, '\n'); i >= 0 && i < len(b)-1 {
			b = b[i+1:]
		}
		trunc = true
	}
	return strings.ToValidUTF8(string(b), "\uFFFD"), trunc
}

// RunLogs downloads and unpacks the log archive of a run (size-limited; each
// file is tail-truncated to LogTailBytes).
func (c *Client) RunLogs(ctx context.Context, repo string, runID int64) ([]LogFile, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return nil, err
	}
	data, err := c.logRedirect(ctx, rp+"/actions/runs/"+strconv.FormatInt(runID, 10)+"/logs", MaxLogDownload)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("github: log archive is not a zip: %w", err)
	}
	files := zr.File
	if len(files) > maxLogEntries {
		files = files[:maxLogEntries]
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	var out []LogFile
	var total int64
	for _, f := range files {
		if f.FileInfo().IsDir() || total >= maxLogTotalRaw {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		budget := min(int64(maxLogEntryRaw), int64(maxLogTotalRaw)-total)
		raw, err := io.ReadAll(io.LimitReader(rc, budget))
		rc.Close()
		if err != nil {
			continue
		}
		total += int64(len(raw))
		txt, tr := tailText(raw, LogTailBytes)
		out = append(out, LogFile{Name: Untrusted(f.Name), Text: Untrusted(txt), Truncated: tr || int64(len(raw)) >= budget})
	}
	return out, nil
}

// JobLog downloads the plain-text log of one job, tail-truncated.
func (c *Client) JobLog(ctx context.Context, repo string, jobID int64) (LogFile, error) {
	rp, err := repoPath(repo)
	if err != nil {
		return LogFile{}, err
	}
	b, err := c.logRedirect(ctx, rp+"/actions/jobs/"+strconv.FormatInt(jobID, 10)+"/logs", 16<<20)
	if err != nil {
		return LogFile{}, err
	}
	txt, tr := tailText(b, LogTailBytes)
	return LogFile{Name: Untrusted("job-" + strconv.FormatInt(jobID, 10)), Text: Untrusted(txt), Truncated: tr}, nil
}
