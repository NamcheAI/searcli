package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientImportOverride(t *testing.T) {
	setup(t)
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "input.json")
	os.WriteFile(p, []byte(`{"installed":{"client_id":"synthetic.apps.googleusercontent.com","client_secret":"synthetic-client"},"extra":"discard"}`), 0600)
	var out, err bytes.Buffer
	if run(ctx, []string{"credentials", "set", p}, &out, &err) != 0 {
		t.Fatal(err.String())
	}
	if strings.Contains(out.String(), "synthetic") {
		t.Fatal("output leak")
	}
	c, e := clientCredentials(ctx, "")
	if e != nil || !validClient(c) {
		t.Fatal(e)
	}
	d, _ := rootDir()
	st, _ := os.Stat(filepath.Join(d, "client.json"))
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	b, _ := os.ReadFile(filepath.Join(d, "client.json"))
	if strings.Contains(string(b), "discard") {
		t.Fatal("extra persisted")
	}
	os.WriteFile(p, []byte("{}"), 0600)
	if _, e = clientCredentials(ctx, p); e == nil {
		t.Fatal("invalid override fell back")
	}
	if _, e = clientCredentials(ctx, ""); e != nil {
		t.Fatal("default changed")
	}
}
func TestEmptyExplicitOverrideUsage(t *testing.T) {
	setup(t)
	for _, flags := range [][]string{{"--credentials="}, {"--credentials", ""}} {
		var out, err bytes.Buffer
		if run(context.Background(), append([]string{"account", "add", "--name", "work"}, flags...), &out, &err) != 2 {
			t.Fatal("usage")
		}
	}
}
func TestRepositoryCredentialAbsence(t *testing.T) {
	if _, e := os.Stat("oauth_client.json"); !os.IsNotExist(e) {
		t.Fatal("embedded file exists")
	}
	files, _ := filepath.Glob("*.go")
	for _, p := range files {
		b, _ := os.ReadFile(p)
		for _, pattern := range []string{"//go:" + "embed", "GOC" + "SPX-", "ops" + "_", "ya" + "29."} {
			if strings.Contains(string(b), pattern) {
				t.Fatal("forbidden source pattern", p)
			}
		}
	}
}
