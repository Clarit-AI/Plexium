# Testing Patterns

## Running Tests

```bash
go test ./...                          # Full test suite
go test ./internal/agent/...           # Single package
go test ./internal/agent/... -v        # Verbose output
go test ./internal/agent/... -run TestName  # Single test
go test -count=1 ./...                 # Force re-run (skip cache)
```

## Test Organization

- Test files live next to source: `foo.go` → `foo_test.go`
- Package-level tests: same package (can test unexported functions)
- No separate `_test` package convention used here

## Assertion Library

All tests use testify:

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestExample(t *testing.T) {
    assert.Equal(t, expected, actual)      // continues on failure
    require.NoError(t, err)                 // stops test on failure
    assert.Contains(t, haystack, needle)
    assert.Len(t, slice, 3)
}
```

Use `require` for preconditions (test can't continue without this). Use `assert` for actual assertions.

## Dependency Injection Pattern

Injectable functions are the primary mocking strategy:

```go
// Production: pass real function
provider := NewOllamaProvider(endpoint, model, DefaultOllamaHTTPPost)

// Test: pass mock
provider := NewOllamaProvider(endpoint, model, func(ctx context.Context, url, body string) (string, int, error) {
    return `{"response":"mock"}`, 10, nil
})
```

This pattern is used for: HTTP transport (cascade.go), runners (daemon/runner.go), command execution (various).

## httptest for HTTP Tests

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(200)
    w.Write([]byte(`{"response":"test","eval_count":5}`))
}))
defer srv.Close()

result, tokens, err := DefaultOllamaHTTPPost(context.Background(), srv.URL, `{}`)
```

## Temp Directories

Tests that write files use `t.TempDir()`:

```go
func TestSomething(t *testing.T) {
    dir := t.TempDir()  // auto-cleaned after test
    // write files to dir, test against them
}
```

## Validation Suite

`validation/` contains cross-phase integration tests:

- `golden_test.go` — golden file tests for deterministic output
- Safety invariant tests (source immutability, ownership protection, etc.)
- Determinism guarantees (manifest sort stability, hash consistency, etc.)
- Cross-phase contract tests (struct fields, exit codes, config validation)

These run as part of `go test ./...` and take ~6 seconds.
