package protocol

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// forbiddenKeys are JSON object keys that must never appear anywhere in a
// message leaving homepi-node (HL-Spec 10, MOD-001 11). The protocol types
// have no such fields, so a hit means a connector or handler leaked one.
var forbiddenKeys = []string{
	"access_token",
	"api_key",
	"apikey",
	"authorization",
	"auth_json",
	"bearer",
	"client_secret",
	"cookie",
	"credential",
	"id_token",
	"password",
	"private_key",
	"raw_response",
	"refresh_token",
	"secret",
	"session",
	"set-cookie",
	"token",
}

// AuditJSON walks a JSON document and reports every violation it finds:
// a forbidden object key, or a string value that matches one of the supplied
// literal needles (used by tests to plant a fake key and prove it never
// reaches the wire).
//
// The returned paths are safe to log: they name the location, never the value.
func AuditJSON(raw []byte, needles []string) []string {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []string{fmt.Sprintf("$: not valid JSON: %v", err)}
	}
	lowered := make([]string, 0, len(needles))
	for _, n := range needles {
		if n != "" {
			lowered = append(lowered, strings.ToLower(n))
		}
	}
	var hits []string
	walkJSON("$", doc, lowered, &hits)
	sort.Strings(hits)
	return hits
}

func walkJSON(path string, node any, needles []string, hits *[]string) {
	switch v := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := path + "." + k
			if isForbiddenKey(k) {
				*hits = append(*hits, "forbidden key at "+child)
			}
			walkJSON(child, v[k], needles, hits)
		}
	case []any:
		for i, item := range v {
			walkJSON(fmt.Sprintf("%s[%d]", path, i), item, needles, hits)
		}
	case string:
		low := strings.ToLower(v)
		for _, n := range needles {
			if strings.Contains(low, n) {
				*hits = append(*hits, "secret value at "+path)
				return
			}
		}
	}
}

func isForbiddenKey(key string) bool {
	k := strings.ToLower(key)
	for _, f := range forbiddenKeys {
		if k == f {
			return true
		}
	}
	return false
}
