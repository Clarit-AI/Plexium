package pilot

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestW1R2_ScreenRawJSONWithholdsValidJSON reproduces W1-R2:
// screenRawJSON leaves io.EOF in err after a valid trailing-content check,
// so ALL nonempty valid response JSON is withheld when secrets are configured.
func TestW1R2_ScreenRawJSONWithholdsValidJSON(t *testing.T) {
	const secret = "test-secret-key"

	// Valid JSON response with no trailing content
	validJSON := []byte(`{"id":"resp-1","model":"test-model","usage":{"input_tokens":10,"output_tokens":5,"cost":0.001}}`)

	result := screenRawJSON(validJSON, []string{secret})

	if result.Withheld {
		t.Fatalf("W1-R2 REPRODUCED: Valid JSON was withheld. Reason: %s", result.Reason)
	}
	if !bytes.Equal(result.Bytes, validJSON) {
		t.Fatalf("W1-R2 REPRODUCED: Valid JSON bytes not preserved. Got: %s", string(result.Bytes))
	}
	t.Log("W1-R2 FIXED: Valid JSON correctly preserved")

	// Test with trailing content (should still be withheld)
	trailingJSON := []byte(`{"id":"resp-1"}{"extra":"data"}`)
	result = screenRawJSON(trailingJSON, []string{secret})
	if !result.Withheld {
		t.Fatal("Trailing content JSON should be withheld")
	}
	t.Log("Trailing content correctly withheld")

	// Test with malformed JSON (should be withheld)
	malformedJSON := []byte(`{"id":"resp-1"`)
	result = screenRawJSON(malformedJSON, []string{secret})
	if !result.Withheld {
		t.Fatal("Malformed JSON should be withheld")
	}
	t.Log("Malformed JSON correctly withheld")

	// Test valid JSON with secret in string value (should be sanitized)
	jsonWithSecret := []byte(`{"diagnostic":"reflected ` + secret + `"}`)
	result = screenRawJSON(jsonWithSecret, []string{secret})
	if result.Withheld {
		t.Fatalf("JSON with secret should be sanitized, not withheld: %s", result.Reason)
	}
	if strings.Contains(string(result.Bytes), secret) {
		t.Fatalf("Secret not redacted in sanitized JSON: %s", string(result.Bytes))
	}
	if !result.Sanitized {
		t.Fatal("Expected sanitized=true for JSON with secret")
	}
	t.Log("JSON with secret correctly sanitized")

	// Test valid JSON with secret in key (should be withheld - credential in key is unsafe)
	jsonWithSecretInKey := []byte(`{"` + secret + `":"value"}`)
	result = screenRawJSON(jsonWithSecretInKey, []string{secret})
	if !result.Withheld {
		t.Fatal("JSON with secret in key should be withheld (unsafe)")
	}
	t.Log("JSON with secret in key correctly withheld")
}

// TestW1R2_ScreenRawJSON_EOFErrorBug tests the specific EOF bug
func TestW1R2_ScreenRawJSON_EOFErrorBug(t *testing.T) {
	const secret = "test-secret"
	validJSON := []byte(`{"data":"value"}`)

	// Trace through the logic manually to show the bug
	dec := json.NewDecoder(bytes.NewReader(validJSON))
	dec.UseNumber()
	var out bytes.Buffer

	// This simulates what screenJSONValue does - it consumes all tokens
	redacted, err := screenJSONValue(dec, &out, []string{secret})
	if err != nil {
		t.Fatalf("screenJSONValue failed: %v", err)
	}

	// Now check trailing - this is where the bug is
	var trailing any
	err = dec.Decode(&trailing)

	// After successful decode of valid JSON with no trailing, err IS io.EOF
	// The current code checks: if !errors.Is(err, io.EOF) -> treat as error
	// But err IS io.EOF, so errors.Is(err, io.EOF) is TRUE
	// So !errors.Is(err, io.EOF) is FALSE
	// This means the bug might be elsewhere...

	t.Logf("After decode, err=%v, errors.Is(err, io.EOF)=%v, redacted=%v", err, errors.Is(err, io.EOF), redacted)

	// The bug: the code does:
	// if err == nil { err = errors.New("multiple top-level JSON values") }
	// if err != nil { withhold }
	// But when err == io.EOF, it's not nil, so it goes to withhold!

	if err != nil && !errors.Is(err, io.EOF) {
		t.Log("Non-EOF error - correctly withheld")
	} else if errors.Is(err, io.EOF) {
		t.Log("EOF error - this is the bug! EOF should not cause withholding")
	} else {
		t.Log("No error - multiple values")
	}
}
