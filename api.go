package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var apiBase = "https://www.googleapis.com/webmasters/v3"
var inspectionEndpoint = "https://searchconsole.googleapis.com/v1/urlInspection/index:inspect"

type APIError struct {
	Status     int
	RetryAfter string
}

func (e *APIError) Error() string { return fmt.Sprintf("Google API HTTP %d", e.Status) }
func api(ctx context.Context, account, method, endpoint string, body any, out any) error {
	token, e := accessToken(ctx, account)
	if e != nil {
		return e
	}
	var b []byte
	if body != nil {
		b, e = json.Marshal(body)
		if e != nil {
			return e
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	r, e := httpClient.Do(req)
	if e != nil {
		return errors.New("Google API transport failed")
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return &APIError{r.StatusCode, r.Header.Get("Retry-After")}
	}
	if e = decodeJSON(r.Body, 4<<20, out); e != nil {
		return errors.New("invalid Google API JSON")
	}
	return nil
}

type Site struct {
	URL        string `json:"siteUrl"`
	Permission string `json:"permissionLevel"`
}

func sites(ctx context.Context, a string) ([]Site, error) {
	var v struct {
		Sites []Site `json:"siteEntry"`
	}
	e := api(ctx, a, "GET", apiBase+"/sites", nil, &v)
	if v.Sites == nil {
		v.Sites = []Site{}
	}
	return v.Sites, e
}
func validateProperty(ctx context.Context, a, p string) error {
	s, e := sites(ctx, a)
	if e != nil {
		return e
	}
	for _, v := range s {
		if v.URL == p && v.Permission != "siteUnverifiedUser" {
			return nil
		}
	}
	return errors.New("property must exactly match an accessible, verified siteUrl from properties")
}
func inProperty(p, raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" {
		return false
	}
	if ambiguousPath(u) {
		return false
	}
	if strings.HasPrefix(p, "sc-domain:") {
		d := strings.ToLower(strings.TrimPrefix(p, "sc-domain:"))
		h := strings.ToLower(u.Hostname())
		return d != "" && (h == d || strings.HasSuffix(h, "."+d))
	}
	v, e := url.Parse(p)
	if e != nil || ambiguousPath(v) {
		return false
	}
	up, vp := u.EscapedPath(), v.EscapedPath()
	if up == "" {
		up = "/"
	}
	if vp == "" {
		vp = "/"
	}
	return u.Scheme == v.Scheme && strings.EqualFold(u.Host, v.Host) && strings.HasPrefix(up, vp)
}

type IndexStatus struct {
	Verdict       string `json:"verdict,omitempty"`
	Coverage      string `json:"coverageState,omitempty"`
	Robots        string `json:"robotsTxtState,omitempty"`
	Indexing      string `json:"indexingState,omitempty"`
	Fetch         string `json:"pageFetchState,omitempty"`
	Canonical     string `json:"googleCanonical,omitempty"`
	UserCanonical string `json:"userCanonical,omitempty"`
	LastCrawl     string `json:"lastCrawlTime,omitempty"`
}
type Result struct {
	URL        string      `json:"url"`
	State      string      `json:"state"`
	Index      IndexStatus `json:"index"`
	Error      string      `json:"error,omitempty"`
	HTTP       int         `json:"http_status,omitempty"`
	RetryAfter string      `json:"retry_after,omitempty"`
}

func classify(i IndexStatus) string {
	switch i.Verdict {
	case "PASS":
		return "ok"
	case "FAIL", "PARTIAL":
		return "problem"
	default:
		return "unknown"
	}
}

type Budget struct {
	Calls []time.Time `json:"calls"`
}

// Rolling 24 hours avoids assumptions about Google's daily reset time. Reserve before sending.
func reserve(ctx context.Context, p string) error {
	unlock, e := lock("quota:" + p)
	if e != nil {
		return e
	}
	defer unlock()
	d, e := rootDir()
	if e != nil {
		return e
	}
	h := sha256.Sum256([]byte(p))
	file := filepath.Join(d, "quota", fmt.Sprintf("%x.json", h))
	var b Budget
	raw, e := os.ReadFile(file)
	if e == nil {
		if json.Unmarshal(raw, &b) != nil {
			return errors.New("invalid quota ledger; refusing inspection")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	now := time.Now()
	kept := []time.Time{}
	for _, t := range b.Calls {
		if now.Sub(t) < 24*time.Hour {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 2000 {
		return errors.New("local 2000/property rolling-24-hour inspection budget exhausted")
	}
	if len(kept) > 0 {
		wait := time.Until(kept[len(kept)-1].Add(110 * time.Millisecond))
		if wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	b.Calls = append(kept, time.Now())
	return atomicJSON(file, b)
}
func inspect(ctx context.Context, a, p, u string) Result {
	r := Result{URL: u}
	if !inProperty(p, u) {
		r.State = "error"
		r.Error = "URL outside property or invalid"
		return r
	}
	if e := reserve(ctx, p); e != nil {
		r.State = "error"
		r.Error = e.Error()
		return r
	}
	var v struct {
		Inspection struct {
			Index IndexStatus `json:"indexStatusResult"`
		} `json:"inspectionResult"`
	}
	e := api(ctx, a, "POST", inspectionEndpoint, map[string]string{"inspectionUrl": u, "siteUrl": p}, &v)
	if e != nil {
		r.State = "error"
		r.Error = e.Error()
		var ae *APIError
		if errors.As(e, &ae) {
			r.HTTP = ae.Status
			r.RetryAfter = ae.RetryAfter
		}
		return r
	}
	r.Index = v.Inspection.Index
	r.State = classify(r.Index)
	return r
}

type Change struct {
	URL    string  `json:"url"`
	Kind   string  `json:"kind"`
	Before *Result `json:"before,omitempty"`
	After  *Result `json:"after,omitempty"`
}
type Snapshot struct {
	Version  int       `json:"schema_version"`
	Account  string    `json:"account"`
	Property string    `json:"property"`
	Time     time.Time `json:"time"`
	Results  []Result  `json:"results"`
	Changes  []Change  `json:"changes"`
}

func diff(old, now []Result) []Change {
	m := map[string]Result{}
	for _, r := range old {
		m[r.URL] = r
	}
	out := []Change{}
	for _, r := range now {
		r := r
		if b, ok := m[r.URL]; !ok {
			out = append(out, Change{URL: r.URL, Kind: "added", After: &r})
		} else {
			a, bb := r, b
			a.Index.LastCrawl = ""
			bb.Index.LastCrawl = ""
			if a != bb {
				b := b
				out = append(out, Change{URL: r.URL, Kind: "changed", Before: &b, After: &r})
			}
			delete(m, r.URL)
		}
	}
	for _, b := range old {
		if _, ok := m[b.URL]; ok {
			b := b
			out = append(out, Change{URL: b.URL, Kind: "removed", Before: &b})
		}
	}
	return out
}
func resultCode(rs []Result) int {
	code := 0
	for _, r := range rs {
		switch r.State {
		case "error":
			return 1
		case "unknown":
			code = 4
		case "problem":
			if code == 0 {
				code = 3
			}
		}
	}
	return code
}
func check(ctx context.Context, a, p string, urls []string) (Snapshot, error) {
	a = strings.ToLower(a)
	s := Snapshot{Version: 1, Account: a, Property: p, Time: time.Now().UTC(), Results: []Result{}}
	if len(urls) == 0 || len(urls) > 2000 {
		return s, errors.New("URL list must contain 1-2000 unique URLs")
	}
	for _, u := range urls {
		if !inProperty(p, u) {
			return s, errors.New("URL list contains an invalid or out-of-property URL")
		}
	}
	unlock, e := lock("check:" + a + ":" + p)
	if e != nil {
		return s, e
	}
	defer unlock()
	if e = validateProperty(ctx, a, p); e != nil {
		return s, e
	}
	d, e := rootDir()
	if e != nil {
		return s, e
	}
	h := sha256.Sum256([]byte(a + ":" + p))
	dir := filepath.Join(d, "snapshots", fmt.Sprintf("%x", h))
	latest := filepath.Join(dir, "latest.json")
	var old Snapshot
	b, e := os.ReadFile(latest)
	if e == nil {
		if json.Unmarshal(b, &old) != nil {
			return s, errors.New("invalid previous snapshot")
		}
	} else if !os.IsNotExist(e) {
		return s, e
	}
	halt := ""
	for _, u := range urls {
		if halt != "" {
			s.Results = append(s.Results, Result{URL: u, State: "error", Error: "not attempted after " + halt})
			continue
		}
		r := inspect(ctx, a, p, u)
		s.Results = append(s.Results, r)
		if r.State == "error" {
			halt = r.Error
		}
	}
	s.Changes = diff(old.Results, s.Results)
	if e = atomicJSON(filepath.Join(dir, s.Time.Format("20060102T150405.000000000Z")+".json"), s); e != nil {
		return s, e
	}
	return s, atomicJSON(latest, s)
}
