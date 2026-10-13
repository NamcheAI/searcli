package main

import "net/url"

func escapeProperty(p string) string { return url.PathEscape(p) }
