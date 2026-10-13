package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) {
	t.Helper()
	t.Setenv("SEARCLI_HOME", t.TempDir())
	oldT, oldA, oldI := tokenEndpoint, apiBase, inspectionEndpoint
	t.Cleanup(func() { tokenEndpoint = oldT; apiBase = oldA; inspectionEndpoint = oldI })
}
func seed(t *testing.T, a string) {
	t.Helper()
	if e := saveToken(a, Token{Access: "test-access", Refresh: "test-refresh", Expiry: time.Now().Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
}
func TestOAuthPKCEAndState(t *testing.T) {
	u, _ := url.Parse(authorizationURL(Credentials{ClientID: "id"}, "http://127.0.0.1:42/callback", "state", "verifier"))
	q := u.Query()
	if q.Get("scope") != scope || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "verifier" || q.Get("access_type") != "offline" {
		t.Fatal(q)
	}
	ch := make(chan string, 1)
	h := callback("state", ch)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/callback?state=wrong&code=secret", nil))
	if w.Code != 400 || len(ch) != 0 {
		t.Fatal("state accepted")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/callback?state=state&code=code", nil))
	if <-ch != "code" {
		t.Fatal("missing code")
	}
	if strings.Contains(w.Body.String(), "code=") {
		t.Fatal("leak")
	}
}
func TestExchangeRefreshPersistenceAndSeparation(t *testing.T) {
	setup(t)
	seed(t, "other")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("refresh_token") != "refresh" || r.Form.Get("client_id") != "id" {
			t.Error("bad form")
		}
		fmt.Fprint(w, `{"access_token":"new","refresh_token":"rotated","expires_in":3600}`)
	}))
	defer srv.Close()
	tokenEndpoint = srv.URL
	saveToken("work", Token{Refresh: "refresh", Credentials: Credentials{ClientID: "id"}})
	a, e := accessToken(context.Background(), "work")
	if e != nil || a != "new" {
		t.Fatal(a, e)
	}
	got, _ := loadToken("work")
	if got.Refresh != "rotated" || time.Until(got.Expiry) < time.Minute {
		t.Fatal(got)
	}
	other, _ := loadToken("other")
	if other.Access != "test-access" {
		t.Fatal("account overwritten")
	}
	p, _ := accountPath("work")
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	if _, e = accountPath("../escape"); e == nil {
		t.Fatal("traversal")
	}
}
func TestOAuthFailureSanitizedAndPreservesRefresh(t *testing.T) {
	setup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(400)
			fmt.Fprint(w, "secret")
			return
		}
		fmt.Fprint(w, `{"access_token":"new","expires_in":3600}`)
	}))
	defer srv.Close()
	tokenEndpoint = srv.URL
	tkn, e := exchange(context.Background(), url.Values{}, Credentials{}, Token{Refresh: "old"})
	if e != nil || tkn.Refresh != "old" {
		t.Fatal(e)
	}
	tokenEndpoint = srv.URL + "/bad"
	_, e = exchange(context.Background(), url.Values{}, Credentials{}, Token{})
	if e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
}
func TestPropertyMembership(t *testing.T) {
	for _, v := range []struct {
		p, u string
		ok   bool
	}{{"sc-domain:example.com", "https://sub.example.com/a", true}, {"sc-domain:example.com", "https://evil-example.com/", false}, {"https://example.com/blog/", "https://example.com/blog/a", true}, {"https://example.com/blog/", "https://example.com/blogged/a", false}, {"https://example.com/", "http://example.com/", false}, {"sc-domain:example.com", "https://example.com/#fragment", false}, {"sc-domain:example.com", "https://x@example.com/", false}} {
		if inProperty(v.p, v.u) != v.ok {
			t.Fatal(v)
		}
	}
}
func mockAPI(t *testing.T, status *int, verdict *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access" {
			t.Error("missing auth")
		}
		if r.URL.Path == "/sites" {
			fmt.Fprint(w, `{"siteEntry":[{"siteUrl":"sc-domain:example.com","permissionLevel":"siteOwner"}]}`)
			return
		}
		if *status != 200 {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(*status)
			fmt.Fprint(w, "private API body")
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["siteUrl"] != "sc-domain:example.com" {
			t.Error(body)
		}
		fmt.Fprintf(w, `{"inspectionResult":{"indexStatusResult":{"verdict":%q,"coverageState":"state"}}}`, *verdict)
	}))
	apiBase = srv.URL
	inspectionEndpoint = srv.URL + "/inspect"
	return srv
}
func TestCheckSnapshotsDiffAndFailures(t *testing.T) {
	setup(t)
	seed(t, "work")
	status, verdict := 200, "PASS"
	srv := mockAPI(t, &status, &verdict)
	defer srv.Close()
	ctx := context.Background()
	if validateProperty(ctx, "work", "sc-domain:other.com") == nil {
		t.Fatal("unlisted property")
	}
	urls := []string{"https://example.com/a"}
	first, e := check(ctx, "work", "sc-domain:example.com", urls)
	if e != nil || resultCode(first.Results) != 0 || first.Changes[0].Kind != "added" {
		t.Fatal(first, e)
	}
	verdict = "FAIL"
	second, e := check(ctx, "work", "sc-domain:example.com", urls)
	if e != nil || resultCode(second.Results) != 3 || second.Changes[0].Kind != "changed" {
		t.Fatal(second, e)
	}
	status = 429
	third, e := check(ctx, "work", "sc-domain:example.com", append(urls, "https://example.com/b"))
	if e != nil || resultCode(third.Results) != 1 || third.Results[0].RetryAfter != "60" || third.Results[1].State != "error" {
		t.Fatal(third, e)
	}
	if strings.Contains(third.Results[0].Error, "private") {
		t.Fatal("API body leak")
	}
	d, _ := rootDir()
	files, _ := filepath.Glob(filepath.Join(d, "snapshots", "*", "*.json"))
	if len(files) != 4 {
		t.Fatal(files)
	}
}
func TestAPIErrorCases(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			setup(t)
			seed(t, "work")
			v := "PASS"
			srv := mockAPI(t, &status, &v)
			defer srv.Close()
			r := inspect(context.Background(), "work", "sc-domain:example.com", "https://example.com/")
			if r.State != "error" || r.HTTP != status {
				t.Fatal(r)
			}
		})
	}
}
func TestClassificationDiffExit(t *testing.T) {
	for _, v := range []struct {
		verdict, state string
		code           int
	}{{"PASS", "ok", 0}, {"FAIL", "problem", 3}, {"PARTIAL", "problem", 3}, {"NEUTRAL", "unknown", 4}, {"", "unknown", 4}} {
		s := classify(IndexStatus{Verdict: v.verdict})
		if s != v.state || resultCode([]Result{{State: s}}) != v.code {
			t.Fatal(v)
		}
	}
	if resultCode([]Result{{State: "unknown"}, {State: "problem"}}) != 4 || resultCode([]Result{{State: "problem"}, {State: "error"}}) != 1 {
		t.Fatal("precedence")
	}
	a := Result{URL: "a", State: "ok", Index: IndexStatus{LastCrawl: "old"}}
	b := a
	b.Index.LastCrawl = "new"
	if len(diff([]Result{a}, []Result{b})) != 0 {
		t.Fatal("crawl timestamp noise")
	}
	if diff([]Result{a}, nil)[0].Kind != "removed" {
		t.Fatal("removed")
	}
}
func TestCLIAndURLBounds(t *testing.T) {
	setup(t)
	var out, err bytes.Buffer
	if run(context.Background(), []string{"account", "list"}, &out, &err) != 0 || !strings.Contains(out.String(), "[]") {
		t.Fatal(out.String())
	}
	if run(context.Background(), []string{"inspect", "--account", "work", "--property", "sc-domain:example.com", "--url", "https://evil.com/"}, &out, &err) != 2 {
		t.Fatal("usage code")
	}
	p := filepath.Join(t.TempDir(), "urls")
	os.WriteFile(p, []byte("# comment\nhttps://example.com/\nhttps://example.com/\nhttps://example.com/b\n"), 0600)
	u, e := readURLs(p, 2)
	if e != nil || len(u) != 2 {
		t.Fatal(u, e)
	}
	if _, e = readURLs(p, 1); e == nil {
		t.Fatal("bound")
	}
}
func TestQuotaBudgetAndLock(t *testing.T) {
	setup(t)
	ctx := context.Background()
	unlock, e := lock("quota:p")
	if e != nil {
		t.Fatal(e)
	}
	if reserve(ctx, "p") == nil {
		t.Fatal("lock bypass")
	}
	unlock()
	if e = reserve(ctx, "p"); e != nil {
		t.Fatal(e)
	}
	d, _ := rootDir()
	files, _ := filepath.Glob(filepath.Join(d, "quota", "*.json"))
	calls := make([]time.Time, 2000)
	for i := range calls {
		calls[i] = time.Now()
	}
	atomicJSON(files[0], Budget{Calls: calls})
	if reserve(ctx, "p") == nil {
		t.Fatal("budget bypass")
	}
}

func TestMalformedAPIAndMissingIndex(t *testing.T) {
	setup(t)
	seed(t, "work")
	payload := "{broken"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, payload) }))
	defer srv.Close()
	inspectionEndpoint = srv.URL
	r := inspect(context.Background(), "work", "sc-domain:example.com", "https://example.com/")
	if r.State != "error" {
		t.Fatal(r)
	}
	payload = "{}"
	r = inspect(context.Background(), "work", "sc-domain:example.com", "https://example.com/")
	if r.State != "unknown" {
		t.Fatal(r)
	}
}
func TestAccountLifecycleAndCheckCLI(t *testing.T) {
	setup(t)
	seed(t, "work")
	seed(t, "personal")
	var out, err bytes.Buffer
	if run(context.Background(), []string{"account", "remove", "--name", "personal"}, &out, &err) != 0 {
		t.Fatal(err.String())
	}
	if _, e := loadToken("work"); e != nil {
		t.Fatal(e)
	}
	if _, e := loadToken("personal"); e == nil {
		t.Fatal("not removed")
	}
	status, verdict := 200, "NEUTRAL"
	srv := mockAPI(t, &status, &verdict)
	defer srv.Close()
	p := filepath.Join(t.TempDir(), "urls.txt")
	os.WriteFile(p, []byte("https://example.com/\n"), 0600)
	out.Reset()
	err.Reset()
	code := run(context.Background(), []string{"check", "--account", "work", "--property", "sc-domain:example.com", "--urls", p}, &out, &err)
	var snap Snapshot
	if code != 4 || json.Unmarshal(out.Bytes(), &snap) != nil || len(snap.Results) != 1 {
		t.Fatal(code, out.String(), err.String())
	}
}
