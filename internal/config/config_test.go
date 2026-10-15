package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseConfig(t *testing.T) {
	confData := `
# Comment line
alias ubuntu https://archive.ubuntu.com/ubuntu/
alias docker https://registry-1.docker.io/ plugin:docker
forward deb.debian.org http://local-gateway:8080
bypass * suffix .index
bypass /dists/ regex ^.*Release.*$ ttl:1m
cache /dists/ suffix .deb
type custom mime:application/custom suffix:.cust
rewrite /npm/ type:json http://registry.npmjs.org/ http://{PROXY_HOST}/npm/
`
	cfg, err := Parse(strings.NewReader(confData))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if cfg.Aliases["ubuntu"] != "https://archive.ubuntu.com/ubuntu/" {
		t.Errorf("expected ubuntu alias, got %s", cfg.Aliases["ubuntu"])
	}
	if cfg.AliasPlugins["docker"] != "docker" {
		t.Errorf("expected docker alias plugin, got %s", cfg.AliasPlugins["docker"])
	}
	if cfg.Forwards["deb.debian.org"] != "http://local-gateway:8080" {
		t.Errorf("expected debian forward, got %s", cfg.Forwards["deb.debian.org"])
	}

	if len(cfg.NoCacheRules) != 3 {
		t.Errorf("expected 3 nocache rules, got %d", len(cfg.NoCacheRules))
	}
	if cfg.NoCacheRules[1].TTL != time.Minute {
		t.Errorf("expected 1m TTL on release rule, got %v", cfg.NoCacheRules[1].TTL)
	}

	if len(cfg.RewriteRules) != 1 {
		t.Errorf("expected 1 rewrite rule, got %d", len(cfg.RewriteRules))
	}

	tDef, ok := cfg.Types["custom"]
	if !ok || len(tDef.Mimes) == 0 || tDef.Mimes[0] != "application/custom" {
		t.Errorf("expected custom type definition, got %+v", tDef)
	}
}

func TestNoCacheRule_Match(t *testing.T) {
	rule := NoCacheRule{
		Scope:   "/dists/",
		Action:  "bypass",
		Type:    "suffix",
		Pattern: "Release",
	}

	if !rule.MatchScope("archive.ubuntu.com", "/dists/jammy/InRelease") {
		t.Errorf("expected scope match")
	}
	if rule.MatchScope("archive.ubuntu.com", "/pool/main/a/app.deb") {
		t.Errorf("expected scope mismatch")
	}

	if !rule.MatchPattern("/dists/jammy/Release", "Release") {
		t.Errorf("expected pattern match")
	}
}

func TestWildcardRuleParsing(t *testing.T) {
	confData := "bypass /api\nbypass /static *\ncache /dists/\ncache /pool/ *\n"
	cfg, err := Parse(strings.NewReader(confData))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(cfg.NoCacheRules) != 4 {
		t.Fatalf("expected 4 rules, got %d", len(cfg.NoCacheRules))
	}
	for i, expectedAction := range []string{"bypass", "bypass", "cache", "cache"} {
		r := cfg.NoCacheRules[i]
		if r.Action != expectedAction || r.Type != "*" || r.Pattern != "*" {
			t.Errorf("rule %d mismatch: %+v", i, r)
		}
	}
}