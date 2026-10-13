package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
)

var version = "dev"

const help = `searcli: read-only Google Search Console CLI (all results are JSON)
Commands:
  credentials set FILE
  account add --name NAME [--credentials desktop.json]
  account list
  account remove --name NAME
  properties --account NAME
  sitemaps --account NAME --property SITE_URL
  inspect --account NAME --property SITE_URL --url URL
  check --account NAME --property SITE_URL --urls FILE [--max 2000]
  version
Flags follow the command. See README for scope, quotas and exit codes.
`

func emit(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func readURLs(p string, max int) ([]string, error) {
	if max < 1 || max > 2000 {
		return nil, errors.New("max must be 1-2000")
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, errors.New("cannot open URL list")
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 8<<20 {
		return nil, errors.New("URL list must be a regular file no larger than 8 MiB")
	}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 65536)
	out := []string{}
	seen := map[string]bool{}
	for s.Scan() {
		u := strings.TrimSpace(s.Text())
		if u == "" || strings.HasPrefix(u, "#") {
			continue
		}
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
			if len(out) > max {
				return nil, errors.New("URL list exceeds --max; refusing partial check")
			}
		}
	}
	if e = s.Err(); e != nil {
		return nil, errors.New("invalid or oversized URL list")
	}
	if len(out) == 0 {
		return nil, errors.New("empty URL list")
	}
	return out, nil
}
func run(ctx context.Context, args []string, out, errout io.Writer) int {
	fail := func(code int, e error) int {
		v := map[string]any{"error": e.Error()}
		var ae *APIError
		if errors.As(e, &ae) {
			v["http_status"] = ae.Status
			if ae.RetryAfter != "" {
				v["retry_after"] = ae.RetryAfter
			}
		}
		if emit(errout, v) != nil {
			return 1
		}
		return code
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		if _, e := fmt.Fprint(out, help); e != nil {
			return fail(1, errors.New("output write failed"))
		}
		return 0
	}
	if args[0] == "credentials" {
		if len(args) != 3 || args[1] != "set" || strings.TrimSpace(args[2]) == "" {
			return fail(2, errors.New("use credentials set FILE"))
		}
		if e := importClient(ctx, args[2]); e != nil {
			return fail(1, e)
		}
		if emit(out, map[string]bool{"credentials_saved": true}) != nil {
			return 1
		}
		return 0
	}
	cmd := args[0]
	args = args[1:]
	if cmd == "version" {
		if e := emit(out, map[string]string{"version": version}); e != nil {
			return fail(1, errors.New("output write failed"))
		}
		return 0
	}
	if cmd == "account" {
		if len(args) == 0 {
			return fail(2, errors.New("account requires add, list or remove"))
		}
		cmd = "account " + args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var a, p, u, file, name, creds string
	max := 2000
	switch cmd {
	case "account add":
		fs.StringVar(&name, "name", "", "account alias")
		fs.StringVar(&creds, "credentials", "", "optional custom Desktop client JSON (overrides imported client)")
	case "account remove":
		fs.StringVar(&name, "name", "", "account alias")
	case "account list":
	case "properties", "sitemaps", "inspect", "check":
		fs.StringVar(&a, "account", "", "account alias")
		if cmd != "properties" {
			fs.StringVar(&p, "property", "", "exact siteUrl")
		}
		if cmd == "inspect" {
			fs.StringVar(&u, "url", "", "URL")
		}
		if cmd == "check" {
			fs.StringVar(&file, "urls", "", "URL list")
			fs.IntVar(&max, "max", 2000, "maximum URLs")
		}
	default:
		return fail(2, errors.New("unknown command; use --help"))
	}
	if e := fs.Parse(args); e != nil {
		return fail(2, errors.New("invalid flags; use --help"))
	}
	explicitCredentials := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "credentials" {
			explicitCredentials = true
		}
	})
	if explicitCredentials && strings.TrimSpace(creds) == "" {
		return fail(2, errors.New("--credentials must name a nonempty file path"))
	}
	if fs.NArg() != 0 {
		return fail(2, errors.New("unexpected positional arguments"))
	}
	a = strings.ToLower(a)
	name = strings.ToLower(name)
	switch cmd {
	case "account add":
		if name == "" {
			return fail(2, errors.New("--name required"))
		}
		if e := addAccount(ctx, name, creds, errout); e != nil {
			return fail(1, e)
		}
		if e := emit(out, map[string]string{"added": name}); e != nil {
			return fail(1, errors.New("output write failed"))
		}
		return 0
	case "account list":
		d, e := rootDir()
		if e != nil {
			return fail(1, e)
		}
		entries, e := os.ReadDir(filepath.Join(d, "accounts"))
		if e != nil && !os.IsNotExist(e) {
			return fail(1, e)
		}
		names := []string{}
		for _, v := range entries {
			if !v.IsDir() && strings.HasSuffix(v.Name(), ".json") {
				names = append(names, strings.TrimSuffix(v.Name(), ".json"))
			}
		}
		sort.Strings(names)
		if e := emit(out, map[string]any{"accounts": names}); e != nil {
			return fail(1, errors.New("output write failed"))
		}
		return 0
	case "account remove":
		p, e := accountPath(name)
		if e != nil {
			return fail(2, e)
		}
		unlock, e := lock("account:" + name)
		if e != nil {
			return fail(1, e)
		}
		defer unlock()
		if e = os.Remove(p); e != nil {
			return fail(1, errors.New("cannot remove account"))
		}
		if e := emit(out, map[string]string{"removed": name}); e != nil {
			return fail(1, errors.New("output write failed"))
		}
		return 0
	}
	if !validName.MatchString(a) {
		return fail(2, errors.New("valid --account required"))
	}
	if cmd == "properties" {
		v, e := sites(ctx, a)
		if e != nil {
			return fail(1, e)
		}
		if e := emit(out, v); e != nil {
			return fail(1, errors.New("output write failed"))
		}
		return 0
	}
	if p == "" {
		return fail(2, errors.New("--property required"))
	}
	if cmd == "check" {
		urls, e := readURLs(file, max)
		if e != nil {
			return fail(2, e)
		}
		for _, u := range urls {
			if !inProperty(p, u) {
				return fail(2, errors.New("URL list contains an invalid or out-of-property URL"))
			}
		}
		s, e := check(ctx, a, p, urls)
		if e != nil {
			return fail(1, e)
		}
		if e = emit(out, s); e != nil {
			return fail(1, e)
		}
		return resultCode(s.Results)
	}
	if cmd == "inspect" && !inProperty(p, u) {
		return fail(2, errors.New("valid in-property --url required"))
	}
	if e := validateProperty(ctx, a, p); e != nil {
		return fail(1, e)
	}
	if cmd == "sitemaps" {
		var v json.RawMessage
		if e := api(ctx, a, "GET", apiBase+"/sites/"+escapeProperty(p)+"/sitemaps", nil, &v); e != nil {
			return fail(1, e)
		}
		if e := emit(out, v); e != nil {
			return fail(1, errors.New("output write failed"))
		}
		return 0
	}
	r := inspect(ctx, a, p, u)
	if e := emit(out, r); e != nil {
		return fail(1, errors.New("output write failed"))
	}
	return resultCode([]Result{r})
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
