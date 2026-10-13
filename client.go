package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func validClient(c Credentials) bool {
	return strings.HasSuffix(c.ClientID, ".apps.googleusercontent.com") && len(c.ClientID) > len(".apps.googleusercontent.com") && !strings.ContainsAny(c.ClientID, " \t\r\n") && strings.TrimSpace(c.ClientSecret) != "" && !strings.ContainsAny(c.ClientSecret, "\r\n")
}
func clientCredentials(ctx context.Context, file string) (Credentials, error) {
	if file == "" {
		d, e := rootDir()
		if e != nil {
			return Credentials{}, e
		}
		file = filepath.Join(d, "client.json")
	}
	b, e := os.ReadFile(file)
	if e != nil {
		return Credentials{}, errors.New("Desktop client unavailable; run credentials set FILE or supply --credentials FILE")
	}
	var v struct {
		Installed Credentials `json:"installed"`
	}
	if json.Unmarshal(b, &v) != nil || !validClient(v.Installed) {
		return Credentials{}, errors.New("invalid Desktop OAuth JSON")
	}
	return v.Installed, nil
}
func importClient(ctx context.Context, file string) error {
	c, e := clientCredentials(ctx, file)
	if e != nil {
		return e
	}
	d, e := rootDir()
	if e != nil {
		return e
	}
	return atomicJSON(filepath.Join(d, "client.json"), map[string]Credentials{"installed": c})
}
