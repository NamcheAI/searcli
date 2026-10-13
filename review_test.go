package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }
func TestReviewAliasConcurrentRefreshRemoveAdd(t *testing.T) {
	setup(t)
	seed(t, "Work")
	a, _ := accountPath("Work")
	b, _ := accountPath("work")
	if a != b {
		t.Fatal("identity differs")
	}
	entries, _ := os.ReadDir(filepath.Dir(a))
	if len(entries) != 1 || entries[0].Name() != "work.json" {
		t.Fatal(entries)
	}
	probe := filepath.Join(t.TempDir(), "Case")
	os.WriteFile(probe, []byte("x"), 0600)
	_, e := os.Stat(filepath.Join(filepath.Dir(probe), "case"))
	t.Logf("case insensitive filesystem: %v", e == nil)
	entered, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		fmt.Fprint(w, `{"access_token":"new","expires_in":3600}`)
	}))
	defer srv.Close()
	tokenEndpoint = srv.URL
	saveToken("Work", Token{Refresh: "r"})
	done := make(chan error, 1)
	go func() { _, e := accessToken(context.Background(), "Work"); done <- e }()
	<-entered
	var out, err bytes.Buffer
	if c := run(context.Background(), []string{"account", "remove", "--name", "work"}, &out, &err); c != 1 {
		t.Fatal("remove bypassed lock", c)
	}
	if e := addAccount(context.Background(), "WORK", "unused", io.Discard); e == nil {
		t.Fatal("add bypassed lock")
	}
	if _, e := accessToken(context.Background(), "work"); e == nil {
		t.Fatal("refresh bypassed lock")
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	token, e := loadToken("WORK")
	if e != nil || token.Access != "new" {
		t.Fatal(e)
	}
}
func TestReviewWriteFailures(t *testing.T) {
	setup(t)
	seed(t, "work")
	status, v := 200, "PASS"
	srv := mockAPI(t, &status, &v)
	defer srv.Close()
	p := filepath.Join(t.TempDir(), "urls")
	os.WriteFile(p, []byte("https://example.com/"), 0600)
	for _, args := range [][]string{{"help"}, {"version"}, {"account", "list"}, {"properties", "--account", "work"}, {"inspect", "--account", "work", "--property", "sc-domain:example.com", "--url", "https://example.com/"}, {"check", "--account", "work", "--property", "sc-domain:example.com", "--urls", p}, {"account", "remove", "--name", "work"}} {
		var err bytes.Buffer
		if c := run(context.Background(), args, brokenWriter{}, &err); c != 1 {
			t.Fatal(args, c)
		}
	}
	if c := run(context.Background(), []string{"invalid"}, io.Discard, brokenWriter{}); c != 1 {
		t.Fatal("stderr failure", c)
	}
}
func TestReviewUsageAndStructuredFailures(t *testing.T) {
	setup(t)
	seed(t, "work")
	p := filepath.Join(t.TempDir(), "urls")
	os.WriteFile(p, []byte("https://elsewhere.com/"), 0600)
	var out, err bytes.Buffer
	args := []string{"check", "--account", "work", "--property", "sc-domain:example.com", "--urls", p}
	if run(context.Background(), args, &out, &err) != 2 {
		t.Fatal(err.String())
	}
	os.WriteFile(p, []byte("https://example.com/"), 0600)
	for _, siteFailure := range []bool{true, false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !siteFailure && r.URL.Path == "/sites" {
				fmt.Fprint(w, `{"siteEntry":[{"siteUrl":"sc-domain:example.com","permissionLevel":"siteOwner"}]}`)
				return
			}
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(429)
		}))
		apiBase = srv.URL
		for _, cmd := range []string{"properties", "sitemaps", "inspect", "check"} {
			if !siteFailure && cmd != "sitemaps" {
				continue
			}
			a := []string{cmd, "--account", "work"}
			if cmd != "properties" {
				a = append(a, "--property", "sc-domain:example.com")
			}
			if cmd == "inspect" {
				a = append(a, "--url", "https://example.com/")
			}
			if cmd == "check" {
				a = append(a, "--urls", p)
			}
			out.Reset()
			err.Reset()
			if run(context.Background(), a, &out, &err) != 1 || !strings.Contains(err.String(), `"retry_after": "120"`) || !strings.Contains(err.String(), `"http_status": 429`) {
				t.Fatal(cmd, err.String())
			}
		}
		srv.Close()
	}
}
func TestReviewStrictJSON(t *testing.T) {
	setup(t)
	seed(t, "work")
	for _, tail := range []string{"garbage", "{}", strings.Repeat(" ", 4<<20)} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"inspectionResult":{"indexStatusResult":{"verdict":"PASS"}},"access_token":"new","expires_in":3600}`+tail)
		}))
		inspectionEndpoint = srv.URL
		tokenEndpoint = srv.URL
		r := inspect(context.Background(), "work", "sc-domain:example.com", "https://example.com/")
		if r.State != "error" {
			t.Fatal(r)
		}
		if _, e := exchange(context.Background(), url.Values{}, Credentials{}, Token{}); e == nil {
			t.Fatal("token suffix accepted")
		}
		srv.Close()
	}
}
func TestReviewAmbiguousPaths(t *testing.T) {
	for _, path := range []string{"/blog/../private", "/blog/%2e%2e/private", "/blog/%252e%252e/private", "/blog/a%2f..%2fprivate", "/blog/a%5cprivate", "/blog/./a"} {
		if inProperty("https://example.com/blog/", "https://example.com"+path) {
			t.Fatal(path)
		}
	}
	if !inProperty("https://example.com/", "https://example.com") {
		t.Fatal("slashless root")
	}
}
func TestReviewBrowserCancellationAndProgress(t *testing.T) {
	setup(t)
	old := openBrowser
	defer func() { openBrowser = old }()
	p := filepath.Join(t.TempDir(), "client.json")
	os.WriteFile(p, []byte(`{"installed":{"client_id":"id"}}`), 0600)
	called := false
	openBrowser = func(ctx context.Context, u string) error {
		called = true
		if _, ok := ctx.Deadline(); !ok {
			t.Error("no deadline")
		}
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var progress bytes.Buffer
	if e := addAccount(ctx, "work", p, &progress); e == nil || !called {
		t.Fatal(e)
	}
	if !strings.Contains(progress.String(), `"progress"`) {
		t.Fatal(progress.String())
	}
	called = false
	if e := addAccount(context.Background(), "work", p, brokenWriter{}); e == nil || called {
		t.Fatal("progress failure ignored")
	}
}

func TestReviewCanonicalSnapshotsAndSitemapWriter(t *testing.T) {
	setup(t)
	seed(t, "WORK")
	status, v := 200, "PASS"
	srv := mockAPI(t, &status, &v)
	defer srv.Close()
	ctx := context.Background()
	urls := []string{"https://example.com/"}
	one, e := check(ctx, "Work", "sc-domain:example.com", urls)
	if e != nil || one.Account != "work" {
		t.Fatal(one, e)
	}
	two, e := check(ctx, "WORK", "sc-domain:example.com", urls)
	if e != nil || len(two.Changes) != 0 {
		t.Fatal(two, e)
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sites" {
			fmt.Fprint(w, `{"siteEntry":[{"siteUrl":"sc-domain:example.com","permissionLevel":"siteOwner"}]}`)
		} else {
			fmt.Fprint(w, `{"sitemap":[]}`)
		}
	}))
	defer srv2.Close()
	apiBase = srv2.URL
	var err bytes.Buffer
	if run(ctx, []string{"sitemaps", "--account", "WORK", "--property", "sc-domain:example.com"}, brokenWriter{}, &err) != 1 {
		t.Fatal("sitemap output failure ignored")
	}
}
