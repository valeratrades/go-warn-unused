// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package auth provides access to user-provided authentication credentials.
package auth

import (
	"cmd/go/internal/base"
	"cmd/go/internal/cfg"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

var (
	credentialCache sync.Map // prefix → http.Header
	authOnce        sync.Once
)

// AddCredentials populates the request header with the user's credentials
// as specified by the GOAUTH environment variable.
// It returns whether any matching credentials were found.
// req must use HTTPS or this function will panic.
func AddCredentials(client *http.Client, req *http.Request, prefix string) bool {
	if req.URL.Scheme != "https" {
		panic("GOAUTH called without https")
	}
	if cfg.GOAUTH == "off" {
		return false
	}
	// Run all GOAUTH commands at least once.
	authOnce.Do(func() {
		runGoAuth(client, "")
	})
	if prefix != "" {
		// First fetch must have failed; re-invoke GOAUTH commands with prefix.
		runGoAuth(client, prefix)
	}
	currentPrefix := strings.TrimPrefix(req.URL.String(), "https://")
	// Iteratively try prefixes, moving up the path hierarchy.
	for currentPrefix != "/" && currentPrefix != "." && currentPrefix != "" {
		if loadCredential(req, currentPrefix) {
			return true
		}

		// Move to the parent directory.
		currentPrefix = path.Dir(currentPrefix)
	}
	return false
}

// runGoAuth executes authentication commands specified by the GOAUTH
// environment variable handling 'off', 'netrc', and 'git' methods specially,
// and storing retrieved credentials for future access.
func runGoAuth(client *http.Client, prefix string) {
	var cmdErrs []error // store GOAUTH command errors to log later.
	goAuthCmds := strings.Split(cfg.GOAUTH, ";")
	// The GOAUTH commands are processed in reverse order to prioritize
	// credentials in the order they were specified.
	slices.Reverse(goAuthCmds)
	for _, cmdStr := range goAuthCmds {
		cmdStr = strings.TrimSpace(cmdStr)
		cmdParts := strings.Fields(cmdStr)
		if len(cmdParts) == 0 {
			base.Fatalf("GOAUTH encountered an empty command (GOAUTH=%s)", cfg.GOAUTH)
		}
		switch cmdParts[0] {
		case "off":
			if len(goAuthCmds) != 1 {
				base.Fatalf("GOAUTH=off cannot be combined with other authentication commands (GOAUTH=%s)", cfg.GOAUTH)
			}
			return
		case "netrc":
			lines, err := readNetrc()
			if err != nil {
				base.Fatalf("could not parse netrc (GOAUTH=%s): %v", cfg.GOAUTH, err)
			}
			for _, l := range lines {
				r := http.Request{Header: make(http.Header)}
				r.SetBasicAuth(l.login, l.password)
				storeCredential([]string{l.machine}, r.Header)
			}
		case "git":
			if len(cmdParts) != 2 {
				base.Fatalf("GOAUTH=git dir method requires an absolute path to the git working directory")
			}
			dir := cmdParts[1]
			if !filepath.IsAbs(dir) {
				base.Fatalf("GOAUTH=git dir method requires an absolute path to the git working directory, dir is not absolute")
			}
			fs, err := os.Stat(dir)
			if err != nil {
				base.Fatalf("GOAUTH=git encountered an error; cannot stat %s: %v", dir, err)
			}
			if !fs.IsDir() {
				base.Fatalf("GOAUTH=git dir method requires an absolute path to the git working directory, dir is not a directory")
			}

			if prefix == "" {
				// Skip the initial GOAUTH run since we need to provide an
				// explicit prefix to runGitAuth.
				continue
			}
			prefix, header, err := runGitAuth(client, dir, prefix)
			if err != nil {
				// Save the error, but don't print it yet in case another
				// GOAUTH command might succeed.
				cmdErrs = append(cmdErrs, fmt.Errorf("GOAUTH=%s: %v", cmdStr, err))
			} else {
				storeCredential([]string{strings.TrimPrefix(prefix, "https://")}, header)
			}
		default:
			base.Fatalf("unimplemented: %s", cmdStr)
		}
	}
	// If no GOAUTH command provided a credential for the given prefix
	// and an error occurred, log the error.
	if cfg.BuildX && prefix != "" {
		if _, ok := credentialCache.Load(prefix); !ok && len(cmdErrs) > 0 {
			log.Printf("GOAUTH encountered errors for %s:", prefix)
			for _, err := range cmdErrs {
				log.Printf("  %v", err)
			}
		}
	}
}

// loadCredential retrieves cached credentials for the given url prefix and adds
// them to the request headers.
func loadCredential(req *http.Request, prefix string) bool {
	headers, ok := credentialCache.Load(prefix)
	if !ok {
		return false
	}
	for key, values := range headers.(http.Header) {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	return true
}

// storeCredential caches or removes credentials (represented by HTTP headers)
// associated with given URL prefixes.
func storeCredential(prefixes []string, header http.Header) {
	for _, prefix := range prefixes {
		if len(header) == 0 {
			credentialCache.Delete(prefix)
		} else {
			credentialCache.Store(prefix, header)
		}
	}
}
