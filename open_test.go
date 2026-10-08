package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeFileOnce_TimeoutClosesServer(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "test.hprof")
	if err := os.WriteFile(p, []byte("DATA"), 0o600); err != nil {
		t.Fatal(err)
	}

	port, urlFile, done, err := serveFileOnce(p, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("serveFileOnce: %v", err)
	}

	// done must close within a reasonable time without any GET
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("done channel not closed after timeout elapsed")
	}

	// server must be gone — further requests should fail
	url := fmt.Sprintf("http://localhost:%d/%s", port, urlFile)
	resp, gerr := http.Get(url) //nolint:noctx,gosec
	if gerr == nil {
		_ = resp.Body.Close()
		t.Error("expected connection refused after server shutdown, but got a response")
	}
}

func TestServeFileOnce(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "test.hprof")
	if err := os.WriteFile(p, []byte("HEAP_CONTENT"), 0o600); err != nil {
		t.Fatal(err)
	}

	port, urlFile, done, err := serveFileOnce(p, 30*time.Second)
	if err != nil {
		t.Fatalf("serveFileOnce: %v", err)
	}
	if port <= 0 {
		t.Fatalf("expected positive port, got %d", port)
	}

	// urlFile must have .hprof extension and contain only the token (no path separators)
	if !strings.HasSuffix(urlFile, ".hprof") {
		t.Errorf("urlFile %q does not end with .hprof", urlFile)
	}
	if strings.ContainsAny(urlFile, "/\\") {
		t.Errorf("urlFile %q contains path separators", urlFile)
	}

	url := fmt.Sprintf("http://localhost:%d/%s", port, urlFile)
	resp, err := http.Get(url) //nolint:noctx,gosec
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("body close: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type: want application/octet-stream, got %s", ct)
	}
	if acao := resp.Header.Get("Access-Control-Allow-Origin"); acao != "*" {
		t.Errorf("Access-Control-Allow-Origin: want *, got %s", acao)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "HEAP_CONTENT" {
		t.Errorf("body: want HEAP_CONTENT, got %s", body)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("done channel not closed after successful GET")
	}
}

func TestServeFileOnce_WrongPath404(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "test.hprof")
	if err := os.WriteFile(p, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}

	port, _, _, err := serveFileOnce(p, 30*time.Second)
	if err != nil {
		t.Fatalf("serveFileOnce: %v", err)
	}

	// Any path other than the exact random token must return 404
	for _, badPath := range []string{"/test.hprof", "/", "/other.hprof", "/../../etc/passwd"} {
		url := fmt.Sprintf("http://localhost:%d%s", port, badPath)
		resp, gerr := http.Get(url) //nolint:noctx,gosec
		if gerr != nil {
			continue // server may have shut down already, that's fine
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("GET %s: expected non-200, got 200", url)
		}
	}
}

func TestServeFileOnce_GzExtension(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "test.hprof.gz")
	if err := os.WriteFile(p, []byte("GZ"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, urlFile, _, err := serveFileOnce(p, 30*time.Second)
	if err != nil {
		t.Fatalf("serveFileOnce: %v", err)
	}
	if !strings.HasSuffix(urlFile, ".hprof.gz") {
		t.Errorf("urlFile %q does not end with .hprof.gz", urlFile)
	}
}

func TestServeFileOnce_MissingFile(t *testing.T) {
	_, _, _, err := serveFileOnce("/nonexistent/path/dump.hprof", 30*time.Second)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestServeFileOnce_OptionsPreflight(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "test.hprof")
	if err := os.WriteFile(p, []byte("DATA"), 0o600); err != nil {
		t.Fatal(err)
	}

	port, urlFile, done, err := serveFileOnce(p, 30*time.Second)
	if err != nil {
		t.Fatalf("serveFileOnce: %v", err)
	}

	url := fmt.Sprintf("http://localhost:%d/%s", port, urlFile)

	// OPTIONS preflight must not trigger shutdown
	req, _ := http.NewRequest(http.MethodOptions, url, nil) //nolint:noctx
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS %s: %v", url, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("OPTIONS: want 204, got %d", resp.StatusCode)
	}
	select {
	case <-done:
		t.Error("done channel closed after OPTIONS — server shut down prematurely")
	default:
	}

	// Subsequent GET must still succeed and close done
	resp2, err := http.Get(url) //nolint:noctx,gosec
	if err != nil {
		t.Fatalf("GET after OPTIONS: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("GET after OPTIONS: want 200, got %d", resp2.StatusCode)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("done channel not closed after GET")
	}
}

func TestBuildOpenURL(t *testing.T) {
	cases := []struct {
		base     string
		port     int
		filename string
		want     string
	}{
		{
			"https://parttimenerd.github.io/hprof-analyzer",
			54321,
			"myapp-heapdump-abc.hprof",
			"https://parttimenerd.github.io/hprof-analyzer/?file=http%3A%2F%2Flocalhost%3A54321%2Fmyapp-heapdump-abc.hprof",
		},
		{
			"https://parttimenerd.github.io/hprof-analyzer/",
			9000,
			"dump.hprof.gz",
			"https://parttimenerd.github.io/hprof-analyzer/?file=http%3A%2F%2Flocalhost%3A9000%2Fdump.hprof.gz",
		},
		{
			"https://parttimenerd.github.io/hprof-analyzer",
			0,
			"dump.hprof",
			"https://parttimenerd.github.io/hprof-analyzer/?file=http%3A%2F%2Flocalhost%3APORT%2Fdump.hprof",
		},
	}
	for _, tc := range cases {
		got := buildOpenURL(tc.base, tc.port, tc.filename)
		if got != tc.want {
			t.Errorf("buildOpenURL(%q, %d, %q)\n  want %q\n   got %q", tc.base, tc.port, tc.filename, tc.want, got)
		}
	}
}

func TestBuildOpenURL_TrailingSlash(t *testing.T) {
	url := buildOpenURL("https://example.com/analyzer/", 1234, "dump.hprof")
	if strings.Contains(url, "//?") {
		t.Errorf("double slash before ?: %s", url)
	}
}

func TestBuildOpenURL_EscapesFilenameAndPreservesExistingQuery(t *testing.T) {
	got := buildOpenURL("https://example.com/analyzer/?theme=dark", 1234, "dump name+#1.hprof.gz")
	want := "https://example.com/analyzer/?file=http%3A%2F%2Flocalhost%3A1234%2Fdump%2520name%2B%25231.hprof.gz&theme=dark"
	if got != want {
		t.Fatalf("buildOpenURL escaped URL mismatch\nwant: %s\n got: %s", want, got)
	}
}

func TestServeFileOnce_RejectsDirectory(t *testing.T) {
	tmp := t.TempDir()
	_, _, _, err := serveFileOnce(tmp, time.Second)
	if err == nil {
		t.Fatal("expected error for directory input, got nil")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected regular file error, got: %v", err)
	}
}

func TestServeFileOnce_RejectsPathVariants(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "test.hprof")
	if err := os.WriteFile(p, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}

	port, urlFile, done, err := serveFileOnce(p, 30*time.Second)
	if err != nil {
		t.Fatalf("serveFileOnce: %v", err)
	}

	badURLs := []string{
		fmt.Sprintf("http://localhost:%d//%s", port, urlFile),
		fmt.Sprintf("http://localhost:%d/%s/", port, urlFile),
		fmt.Sprintf("http://localhost:%d/%s%%2f", port, urlFile),
		fmt.Sprintf("http://localhost:%d/%s?extra=1", port, url.QueryEscape(urlFile)),
	}

	for _, rawURL := range badURLs {
		resp, gerr := http.Get(rawURL) //nolint:noctx,gosec
		if gerr != nil {
			t.Fatalf("GET %s: %v", rawURL, gerr)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s: want 404, got %d", rawURL, resp.StatusCode)
		}
	}

	select {
	case <-done:
		t.Fatal("done channel closed after non-exact path variant request")
	default:
	}

	goodURL := fmt.Sprintf("http://localhost:%d/%s", port, urlFile)
	resp, err := http.Get(goodURL) //nolint:noctx,gosec
	if err != nil {
		t.Fatalf("GET %s: %v", goodURL, err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: want 200, got %d", goodURL, resp.StatusCode)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("done channel not closed after exact path GET")
	}
}

func TestServeFileOnce_ConcurrentGETsNoPanic(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "test.hprof")
	if err := os.WriteFile(p, []byte("CONCURRENT"), 0o600); err != nil {
		t.Fatal(err)
	}

	port, urlFile, done, err := serveFileOnce(p, 30*time.Second)
	if err != nil {
		t.Fatalf("serveFileOnce: %v", err)
	}

	url := fmt.Sprintf("http://localhost:%d/%s", port, urlFile)

	// Fire two GETs simultaneously; neither should panic and done must close exactly once.
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			resp, gerr := http.Get(url) //nolint:noctx,gosec
			if gerr == nil {
				_, _ = io.ReadAll(resp.Body)
				_ = resp.Body.Close()
			}
			errs <- gerr
		}()
	}

	// Collect both results — one may get a connection-refused after server shuts down
	for range 2 {
		<-errs
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("done channel not closed after concurrent GETs")
	}
}

func TestServeFileOnce_ClientDisconnectDoesNotConsumeSingleServe(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "test.hprof")
	// 16 MB — must exceed the combined TCP send+receive buffer so io.Copy blocks
	// when the client disconnects mid-transfer, preventing a spurious shutdown trigger.
	content := strings.Repeat("HEAP_CONTENT", 1<<20)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	port, urlFile, done, err := serveFileOnce(p, 30*time.Second)
	if err != nil {
		t.Fatalf("serveFileOnce: %v", err)
	}

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_, _ = fmt.Fprintf(conn, "GET /%s HTTP/1.1\r\nHost: localhost\r\n\r\n", urlFile)
	_ = conn.Close()

	select {
	case <-done:
		t.Fatal("done channel closed after client disconnected before successful transfer")
	case <-time.After(200 * time.Millisecond):
	}

	goodURL := fmt.Sprintf("http://localhost:%d/%s", port, urlFile)
	resp, err := http.Get(goodURL) //nolint:noctx,gosec
	if err != nil {
		t.Fatalf("GET %s: %v", goodURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: want 200, got %d", goodURL, resp.StatusCode)
	}
	if string(body) != content {
		t.Fatalf("unexpected body length/content after retry")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("done channel not closed after successful retry GET")
	}
}
