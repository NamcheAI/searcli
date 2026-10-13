package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const scope = "https://www.googleapis.com/auth/webmasters.readonly"

var authEndpoint = "https://accounts.google.com/o/oauth2/v2/auth"
var tokenEndpoint = "https://oauth2.googleapis.com/token"
var httpClient = &http.Client{Timeout: 30 * time.Second}
var validName = regexp.MustCompile("^[a-zA-Z0-9_-]{1,64}$")

type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}
type Token struct {
	ClientRef   string      `json:"client_ref,omitempty"`
	Access      string      `json:"access_token"`
	Refresh     string      `json:"refresh_token"`
	Expiry      time.Time   `json:"expiry"`
	Credentials Credentials `json:"credentials"`
}

func rootDir() (string, error) {
	if s := os.Getenv("SEARCLI_HOME"); s != "" {
		return s, nil
	}
	d, e := os.UserConfigDir()
	return filepath.Join(d, "searcli"), e
}
func privateDir(p string) error {
	if e := os.MkdirAll(p, 0700); e != nil {
		return e
	}
	s, e := os.Lstat(p)
	if e != nil {
		return e
	}
	if !s.IsDir() || s.Mode()&os.ModeSymlink != 0 {
		return errors.New("unsafe storage directory")
	}
	return os.Chmod(p, 0700)
}
func atomicJSON(p string, v any) error {
	if e := privateDir(filepath.Dir(p)); e != nil {
		return e
	}
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".tmp-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), p)
}
func accountPath(name string) (string, error) {
	if !validName.MatchString(name) {
		return "", errors.New("account must be 1-64 letters, digits, underscores or hyphens")
	}
	d, e := rootDir()
	return filepath.Join(d, "accounts", strings.ToLower(name)+".json"), e
}
func loadToken(name string) (Token, error) {
	var t Token
	p, e := accountPath(name)
	if e != nil {
		return t, e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return t, errors.New("account unavailable; use account add")
	}
	if json.Unmarshal(b, &t) != nil {
		return t, errors.New("invalid stored account")
	}
	if t.Access == "" && t.Refresh == "" {
		return t, errors.New("empty stored account")
	}
	return t, nil
}
func saveToken(name string, t Token) error {
	p, e := accountPath(name)
	if e != nil {
		return e
	}
	return atomicJSON(p, t)
}
func lock(name string) (func(), error) {
	if strings.HasPrefix(name, "account:") {
		name = strings.ToLower(name)
	}
	d, e := rootDir()
	if e != nil {
		return nil, e
	}
	d = filepath.Join(d, "locks")
	if e = privateDir(d); e != nil {
		return nil, e
	}
	h := sha256.Sum256([]byte(name))
	p := filepath.Join(d, fmt.Sprintf("%x", h))
	f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, errors.New("operation locked; if no searcli process is running, remove stale lock in SEARCLI_HOME/locks")
	}
	f.Close()
	return func() { os.Remove(p) }, nil
}
func exchange(ctx context.Context, values url.Values, c Credentials, old Token) (Token, error) {
	values.Set("client_id", c.ClientID)
	values.Set("client_secret", c.ClientSecret)
	req, e := http.NewRequestWithContext(ctx, "POST", tokenEndpoint, strings.NewReader(values.Encode()))
	if e != nil {
		return old, e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r, e := httpClient.Do(req)
	if e != nil {
		return old, errors.New("OAuth transport failed")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return old, fmt.Errorf("OAuth exchange failed (HTTP %d); reauthorize if revoked", r.StatusCode)
	}
	var v struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Expires int    `json:"expires_in"`
		Scope   string `json:"scope"`
	}
	if e = decodeJSON(r.Body, 1<<20, &v); e != nil {
		return old, errors.New("invalid OAuth response")
	}
	if v.Access == "" || v.Expires <= 0 {
		return old, errors.New("incomplete OAuth response")
	}
	if v.Scope != "" && v.Scope != scope {
		return old, errors.New("unexpected OAuth scopes")
	}
	old.Access = v.Access
	if v.Refresh != "" {
		old.Refresh = v.Refresh
	}
	old.Expiry = time.Now().Add(time.Duration(v.Expires) * time.Second)
	old.Credentials = c
	return old, nil
}
func accessToken(ctx context.Context, name string) (string, error) {
	unlock, e := lock("account:" + name)
	if e != nil {
		return "", e
	}
	defer unlock()
	t, e := loadToken(name)
	if e != nil {
		return "", e
	}
	if time.Until(t.Expiry) > time.Minute {
		return t.Access, nil
	}
	if t.Refresh == "" {
		return "", errors.New("no refresh token; reauthorize account")
	}
	c := t.Credentials
	t, e = exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {t.Refresh}}, c, t)
	if e != nil {
		return "", e
	}
	if e = saveToken(name, t); e != nil {
		return "", e
	}
	return t.Access, nil
}
func randomString() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func authorizationURL(c Credentials, redirect, state, verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	v := url.Values{"client_id": {c.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {scope}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(h[:])}, "code_challenge_method": {"S256"}, "access_type": {"offline"}, "prompt": {"consent"}}
	return authEndpoint + "?" + v.Encode()
}
func callback(state string, ch chan<- string) http.Handler {
	var once sync.Once
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid state", 400)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Authorization denied or missing code", 400)
			once.Do(func() { ch <- "" })
			return
		}
		once.Do(func() { ch <- code })
		fmt.Fprintln(w, "Authorization received. Return to searcli.")
	})
}
func addAccount(ctx context.Context, name, credentialsFile string, progress io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	name = strings.ToLower(name)
	p, e := accountPath(name)
	if e != nil {
		return e
	}
	unlock, e := lock("account:" + name)
	if e != nil {
		return e
	}
	defer unlock()
	if _, e = os.Stat(p); e == nil {
		return errors.New("account already exists; remove it before reauthorizing")
	}
	c, e := clientCredentials(ctx, credentialsFile)
	if e != nil {
		return e
	}
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return e
	}
	ch := make(chan string, 1)
	state, verifier := randomString(), randomString()
	redirect := "http://" + ln.Addr().String() + "/callback"
	srv := &http.Server{Handler: callback(state, ch), ReadHeaderTimeout: 5 * time.Second}
	defer srv.Close()
	go srv.Serve(ln)
	u := authorizationURL(c, redirect, state, verifier)
	if e = emit(progress, map[string]string{"progress": "Complete authorization in your browser (5 minute timeout)."}); e != nil {
		return errors.New("cannot write authorization progress")
	}
	if e = openBrowser(ctx, u); e != nil {
		return errors.New("browser launch failed or authorization canceled; use a desktop session with a default browser")
	}
	select {
	case code := <-ch:
		if code == "" {
			return errors.New("authorization denied")
		}
		t, e := exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {verifier}}, c, Token{})
		if e != nil {
			return e
		}
		if t.Refresh == "" {
			return errors.New("Google did not return an offline refresh token")
		}
		return saveToken(name, t)
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Minute):
		return errors.New("authorization timed out")
	}
}

var openBrowser = func(ctx context.Context, u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", u)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", u)
	}
	cmd.WaitDelay = time.Second
	return cmd.Run()
}
