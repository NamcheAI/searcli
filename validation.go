package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
)

// Read limit+1 to distinguish real EOF from truncation, then require one complete JSON value.
func decodeJSON(r io.Reader, limit int64, out any) error {
	b, e := io.ReadAll(io.LimitReader(r, limit+1))
	if e != nil {
		return e
	}
	if int64(len(b)) > limit {
		return errors.New("JSON response too large")
	}
	return json.Unmarshal(b, out)
}

// Reject ambiguous paths rather than guessing server/browser normalization.
func ambiguousPath(u *url.URL) bool {
	p := strings.ToLower(u.EscapedPath())
	if strings.Contains(p, "%2f") || strings.Contains(p, "%5c") || strings.Contains(p, "%25") || strings.Contains(u.Path, "\\") {
		return true
	}
	for _, s := range strings.Split(u.Path, "/") {
		if s == "." || s == ".." {
			return true
		}
	}
	return false
}
