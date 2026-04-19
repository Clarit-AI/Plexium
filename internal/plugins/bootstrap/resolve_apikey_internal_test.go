package bootstrap

import "testing"

// TestResolveAPIKey_ResolverConsultedOnEmptyEnv is the regression test
// for the pass-3 finding: resolveAPIKey used to short-circuit on empty
// envName before invoking the caller's resolver. cmd/plexium's
// loadAPIKey can resolve a key from .plexium/credentials.json without
// an env-var name, so the short-circuit silently broke the credentials
// path for inherited embeddings. The resolver must be consulted first.
func TestResolveAPIKey_ResolverConsultedOnEmptyEnv(t *testing.T) {
	t.Parallel()
	opts := Options{APIKeyResolver: func(string) string { return "creds-key" }}
	if got := opts.resolveAPIKey(""); got != "creds-key" {
		t.Fatalf("resolveAPIKey(\"\") with resolver: want %q, got %q", "creds-key", got)
	}
}

// TestResolveAPIKey_NoResolverEmptyEnv ensures the no-resolver +
// empty-env branch still returns "" without consulting os.Getenv("").
func TestResolveAPIKey_NoResolverEmptyEnv(t *testing.T) {
	t.Parallel()
	if got := (Options{}).resolveAPIKey(""); got != "" {
		t.Fatalf("resolveAPIKey(\"\") no resolver: want %q, got %q", "", got)
	}
}

// TestResolveAPIKey_ResolverSeesEnvName confirms that when an env name
// IS supplied, it's still passed through to the resolver verbatim.
func TestResolveAPIKey_ResolverSeesEnvName(t *testing.T) {
	t.Parallel()
	var seen string
	opts := Options{APIKeyResolver: func(name string) string {
		seen = name
		return "k"
	}}
	if got := opts.resolveAPIKey("FOO"); got != "k" {
		t.Fatalf("resolveAPIKey(\"FOO\"): want %q, got %q", "k", got)
	}
	if seen != "FOO" {
		t.Fatalf("resolver received %q; want %q", seen, "FOO")
	}
}
