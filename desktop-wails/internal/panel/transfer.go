package panel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Transfers move files between the panel and the disk in Go, never through the webview's bridge (which may buffer a
// whole body) or JavaScript memory: backups, GPO backups, quarantine files and exports.

// request builds a request to apiPath ("/api/…", optionally with a query) on session gen, refused as stale when that
// session has ended.
// The request also ends with its session; done releases it.
func (p *Panel) request(ctx context.Context, gen uint64, method, apiPath string, body io.Reader) (hc *http.Client, req *http.Request, done func(), err error) {
	u, err := url.Parse(apiPath)
	if err != nil {
		return nil, nil, nil, err
	}
	path, ok := apiPathOnly(u)
	if !ok {
		return nil, nil, nil, fmt.Errorf("not a panel API path: %q", apiPath)
	}
	hc, sctx, err := p.session(gen)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(sctx, cancel)
	done = func() { stop(); cancel() }
	target := p.base.String() + path
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
	}
	if req, err = http.NewRequestWithContext(ctx, method, target, body); err != nil {
		done()
		return nil, nil, nil, err
	}
	return hc, req, done, nil
}

// apiPathOnly is apiPath for a path the SPA hands to a service: never a full URL.
func apiPathOnly(u *url.URL) (string, bool) {
	if u.Scheme != "" || u.Host != "" || u.User != nil {
		return "", false
	}
	return apiPath(u)
}

// panelError is the panel's {"error": "…"} of a failed answer, or its status.
func panelError(res *http.Response) error {
	var body struct {
		Error string `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body) == nil && body.Error != "" {
		return errors.New(body.Error)
	}
	return fmt.Errorf("HTTP %d", res.StatusCode)
}

// Download streams GET apiPath into dst, written to a private temporary file beside it and renamed over dst once
// complete (mode 0600 on Unix, also over an existing file; nothing is left behind on an error). It returns the size
// and the hex SHA-256 of what was written.
func (p *Panel) Download(ctx context.Context, gen uint64, apiPath, dst string) (int64, string, error) {
	hc, req, done, err := p.request(ctx, gen, http.MethodGet, apiPath, nil)
	if err != nil {
		return 0, "", err
	}
	defer done()
	res, err := hc.Do(req)
	if err != nil {
		return 0, "", cause(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, "", panelError(res)
	}
	return writePrivate(dst, res.Body)
}

// SaveText writes text to dst as Download does (a CSV built in the SPA).
func SaveText(dst, text string) error {
	_, _, err := writePrivate(dst, strings.NewReader(text))
	return err
}

func writePrivate(dst string, r io.Reader) (int64, string, error) {
	f, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*.part") // 0600
	if err != nil {
		return 0, "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), dst)
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return 0, "", cause(err)
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// Upload streams the file src as the raw body of POST apiPath (a backup archive), calling progress with the bytes
// sent so far and the total, and returns the panel's answer. Cancelling ctx stops the transfer.
func (p *Panel) Upload(ctx context.Context, gen uint64, apiPath, src string, progress func(sent, total int64)) ([]byte, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	body := &countingReader{r: f, total: st.Size(), progress: progress}
	hc, req, done, err := p.request(ctx, gen, http.MethodPost, apiPath, body)
	if err != nil {
		return nil, err
	}
	defer done()
	req.ContentLength = st.Size()
	req.Header.Set("Content-Type", "application/octet-stream")
	res, err := hc.Do(req)
	if err != nil {
		return nil, cause(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, panelError(res)
	}
	return io.ReadAll(io.LimitReader(res.Body, 4<<20))
}

type countingReader struct {
	r           io.Reader
	sent, total int64
	progress    func(sent, total int64)
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.sent += int64(n)
	if c.progress != nil && n > 0 {
		c.progress(c.sent, c.total)
	}
	return n, err
}
