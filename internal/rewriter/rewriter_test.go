package rewriter

import (
	"strings"
	"testing"

	"uri-proxy/internal/config"
)

func TestBodyRewriter_Apply(t *testing.T) {
	rules := []config.RewriteRule{
		{
			Scope:       "/pypi/simple/",
			TypeSpec:    "type:pypi",
			Pattern:     "https://files.pythonhosted.org/packages/",
			Replacement: "http://{PROXY_HOST}/pypi/packages/",
		},
	}
	types := config.DefaultTypeDefinitions()

	rw := NewBodyRewriter(rules, types)

	reqPath := "/pypi/simple/requests/"
	uri := "https://pypi.org/simple/requests/"
	ct := "application/vnd.pypi.simple.v1+json"

	if !rw.ShouldRewrite(reqPath, uri, ct) {
		t.Fatalf("expected ShouldRewrite to be true")
	}

	rawBody := []byte(`{"files":[{"url":"https://files.pythonhosted.org/packages/foo.whl"}]}`)
	rewritten := rw.Apply("127.0.0.1:8888", reqPath, uri, ct, rawBody)

	expected := `http://127.0.0.1:8888/pypi/packages/foo.whl`
	if !strings.Contains(string(rewritten), expected) {
		t.Errorf("expected rewritten body to contain %s, got: %s", expected, string(rewritten))
	}
}

func TestBodyRewriter_BinaryProtection(t *testing.T) {
	rules := []config.RewriteRule{
		{
			Scope:       "/npm/",
			TypeSpec:    "*",
			Pattern:     "http://registry.npmjs.org/",
			Replacement: "http://{PROXY_HOST}/npm/",
		},
	}
	types := config.DefaultTypeDefinitions()

	rw := NewBodyRewriter(rules, types)

	// Tarball path with binary mime
	reqPath := "/npm/lodash/-/lodash-4.17.21.tgz"
	uri := "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz"
	ct := "application/octet-stream"

	if rw.ShouldRewrite(reqPath, uri, ct) {
		t.Errorf("binary mime must not be rewritten")
	}
}
