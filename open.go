/*
 * Copyright (c) 2024 SAP SE or an SAP affiliate company. All rights reserved.
 * This file is licensed under the Apache Software License, v. 2 except as noted
 * otherwise in the LICENSE file at the root of the repository.
 */

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// serveFileOnce starts a local HTTP server on a random port that serves path
// exactly once. The file is exposed under a random token path (e.g. /a3f9c2.hprof)
// so the local filename is never leaked and only the holder of the URL can fetch it.
// Returns the bound port, the randomised URL path segment, and a channel that
// closes when the first GET request completes or timeout elapses.
// timeout 0 means no timeout.
func serveFileOnce(path string, timeout time.Duration) (port int, urlFile string, done <-chan struct{}, err error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		return 0, "", nil, fmt.Errorf("file not found: %w", statErr)
	}
	if !info.Mode().IsRegular() {
		return 0, "", nil, fmt.Errorf("path is not a regular file: %s", path)
	}

	// Build a random token + preserve only the file extension (.hprof or .hprof.gz).
	var tokenBytes [8]byte
	if _, err = rand.Read(tokenBytes[:]); err != nil {
		return 0, "", nil, fmt.Errorf("could not generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes[:])
	ext := extHprof
	if strings.HasSuffix(path, extHprofGz) {
		ext = extHprofGz
	}
	urlFile = token + ext
	exactPath := "/" + urlFile
	exactEscapedPath := (&url.URL{Path: exactPath}).EscapedPath()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, "", nil, fmt.Errorf("could not bind local port: %w", err)
	}
	port = ln.Addr().(*net.TCPAddr).Port

	doneCh := make(chan struct{})
	var shutdownOnce sync.Once
	var srv *http.Server
	srv = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != exactPath || r.URL.EscapedPath() != exactEscapedPath || r.URL.RawQuery != "" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", "*")
			// Answer CORS preflight without serving the file or triggering shutdown.
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			f, ferr := os.Open(path) //nolint:gosec // path comes from plugin internals, not user input
			if ferr != nil {
				http.Error(w, "file unavailable", http.StatusInternalServerError)
				return
			}
			defer func() { _ = f.Close() }()
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			if _, copyErr := io.Copy(w, f); copyErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed while serving heap dump: %v\n", copyErr)
				return
			}
			// Use a fresh context: r.Context() is canceled when the handler returns,
			// but Shutdown must outlive the request.
			// sync.Once ensures concurrent GETs can't double-close doneCh (panic).
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:contextcheck
			go func(ctx context.Context, cancel context.CancelFunc) {                               //nolint:contextcheck
				defer cancel()
				shutdownOnce.Do(func() {
					close(doneCh)
					_ = srv.Shutdown(ctx)
				})
			}(shutdownCtx, shutdownCancel)
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       30 * time.Second,
		// No WriteTimeout: heap dumps can be several GB; a deadline would abort
		// the in-progress transfer to the browser on slow machines or large files.
	}

	go func() { _ = srv.Serve(ln) }()

	if timeout > 0 {
		go func() {
			timer := time.NewTimer(timeout)
			defer timer.Stop()
			select {
			case <-doneCh:
			case <-timer.C:
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:contextcheck
				defer shutdownCancel()
				shutdownOnce.Do(func() {
					fmt.Fprintf(os.Stderr, "Timed out waiting for browser to fetch heap dump; closing local server.\n")
					close(doneCh)
					_ = srv.Shutdown(shutdownCtx)
				})
			}
		}()
	}

	return port, urlFile, doneCh, nil
}

// buildOpenURL constructs the hprof-analyzer URL with the ?file= parameter.
// base is the analyzer base URL (trailing slash optional).
// Use port=0 to produce a PORT placeholder (for dry-run output).
func buildOpenURL(base string, port int, filename string) string {
	hadTrailingSlash := strings.HasSuffix(base, "/")
	base = strings.TrimRight(base, "/")
	portStr := fmt.Sprintf("%d", port)
	if port == 0 {
		portStr = "PORT"
	}

	parsedBase, err := url.Parse(base)
	if err != nil || parsedBase.Scheme == "" || parsedBase.Host == "" {
		return fmt.Sprintf("%s/?file=%s", base, url.QueryEscape(fmt.Sprintf("http://localhost:%s/%s", portStr, filename)))
	}

	fileURL := &url.URL{
		Scheme: "http",
		Host:   "localhost:" + portStr,
		Path:   "/" + filename,
	}
	query := parsedBase.Query()
	query.Set("file", fileURL.String())
	parsedBase.RawQuery = query.Encode()
	if hadTrailingSlash && !strings.HasSuffix(parsedBase.Path, "/") {
		parsedBase.Path += "/"
	}
	if parsedBase.RawPath == "" {
		parsedBase.RawPath = parsedBase.Path
	}
	if !hadTrailingSlash && parsedBase.RawQuery != "" && !strings.HasSuffix(parsedBase.Path, "/") && parsedBase.RawPath == parsedBase.Path {
		parsedBase.Path += "/"
		parsedBase.RawPath += "/"
	}
	return parsedBase.String()
}

// openBrowser opens url in the system default browser.
// If launch fails, prints the URL to stdout so the user can open it manually.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case osWindows:
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Printf("Could not open browser (%v). Open manually: %s\n", err, url)
	}
}
