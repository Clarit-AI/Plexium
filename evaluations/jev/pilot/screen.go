package pilot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
)

const redactedCredential = "[REDACTED_CREDENTIAL]"

type screenedRaw struct {
	Bytes          []byte
	OriginalSHA256 string
	Sanitized      bool
	Withheld       bool
	Reason         string
}

func sanitizeText(value string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, redactedCredential)
		}
	}
	return value
}

// sanitizeTextPreservingNumbers redacts secrets in free text (error, halt,
// and diagnostic strings) with one exact rule: a secret match is preserved
// ONLY when it is a proper part of a longer valid numeric lexeme in the text
// (e.g. secret "20" inside "3.20", "20.5", "2e20", "0.0000020", "120",
// "205", "992088"). Every other occurrence is redacted — including a bare
// numeric credential ("code 20") and alphanumeric-embedded credentials
// ("v20beta", "gpt-20turbo", "sk-20-live").
//
// A "valid numeric lexeme" is a maximal run of number characters
// ([0-9+-.eE]) that (a) parses as one JSON number token, (b) is not glued
// into a larger alphanumeric token at either boundary, and (c) strictly
// contains the match. Numeric FIELD values never pass through this
// function: billing lexemes are preserved byte-identical by the
// token-aware field screeners (screenDecimalField).
func sanitizeTextPreservingNumbers(value string, secrets []string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		value = redactSecretOutsideNumericLexemes(value, secret)
	}
	return value
}

func redactSecretOutsideNumericLexemes(value, secret string) string {
	secretLen := len(secret)
	if secretLen == 0 || len(value) < secretLen {
		return value
	}
	var result strings.Builder
	i := 0
	for i <= len(value)-secretLen {
		if value[i:i+secretLen] != secret {
			result.WriteByte(value[i])
			i++
			continue
		}
		if isProperPartOfNumericLexeme(value, i, secretLen) {
			result.WriteString(secret)
		} else {
			result.WriteString(redactedCredential)
		}
		i += secretLen
	}
	result.WriteString(value[i:])
	return result.String()
}

// isProperPartOfNumericLexeme reports whether value[match:match+matchLen] is
// strictly contained in a longer valid numeric lexeme of value.
func isProperPartOfNumericLexeme(value string, match, matchLen int) bool {
	lo, hi := match, match+matchLen
	for i := lo; i < hi; i++ {
		if !isNumberChar(value[i]) {
			return false
		}
	}
	// Maximal number-character run around the match.
	for lo > 0 && isNumberChar(value[lo-1]) {
		lo--
	}
	for hi < len(value) && isNumberChar(value[hi]) {
		hi++
	}
	// The lexeme must be a standalone numeric token, not a fragment glued
	// into a larger alphanumeric word ("v20beta", "gpt-20turbo", "120x").
	if lo > 0 && isAlphaNum(value[lo-1]) {
		return false
	}
	if hi < len(value) && isAlphaNum(value[hi]) {
		return false
	}
	// The match must be a proper part of a LONGER valid numeric lexeme.
	if hi-lo <= matchLen {
		return false
	}
	return isJSONNumberToken(value[lo:hi])
}

// isNumberChar reports whether c can appear inside a JSON number token.
func isNumberChar(c byte) bool {
	return (c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-' || c == 'e' || c == 'E'
}

func isAlphaNum(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func containsSecret(value string, secrets []string) bool {
	for _, secret := range secrets {
		if secret != "" && strings.Contains(value, secret) {
			return true
		}
	}
	return false
}

func screenRawJSON(value []byte, secrets []string) screenedRaw {
	result := screenedRaw{OriginalSHA256: hashBytes(value)}
	if len(value) == 0 || len(secrets) == 0 {
		result.Bytes = append([]byte(nil), value...)
		return result
	}
	dec := json.NewDecoder(bytes.NewReader(value))
	dec.UseNumber()
	var out bytes.Buffer
	redacted, err := screenJSONValue(dec, &out, secrets)
	if err == nil {
		var trailing any
		err = dec.Decode(&trailing)
		if !errors.Is(err, io.EOF) {
			if err == nil {
				err = errors.New("multiple top-level JSON values")
			}
		} else {
			// Valid JSON with no trailing content - clear the EOF error
			err = nil
		}
	}
	if err != nil {
		result.Withheld = true
		result.Reason = "original response withheld: JSON could not be semantically screened; original SHA-256 retained"
		marker, _ := json.Marshal(map[string]any{
			"withheld": true, "originalRawSha256": result.OriginalSHA256,
			"reason": result.Reason,
		})
		result.Bytes = marker
		return result
	}
	if redacted {
		result.Bytes = out.Bytes()
		result.Sanitized = true
		result.Reason = "credential-redacted JSON response; original bytes withheld and represented by originalRawSha256"
		return result
	}
	result.Bytes = append([]byte(nil), value...)
	result.Reason = "credential-screened JSON response; persisted bytes equal original bounded response"
	return result
}

func screenJSONValue(dec *json.Decoder, out *bytes.Buffer, secrets []string) (bool, error) {
	token, err := dec.Token()
	if err != nil {
		return false, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			out.WriteByte('{')
			redacted := false
			for index := 0; dec.More(); index++ {
				if index > 0 {
					out.WriteByte(',')
				}
				keyToken, err := dec.Token()
				if err != nil {
					return false, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return false, errors.New("object key is not a string")
				}
				if containsSecret(key, secrets) {
					return false, errors.New("credential appears in a decoded JSON object key")
				}
				encodedKey, _ := json.Marshal(key)
				out.Write(encodedKey)
				out.WriteByte(':')
				childRedacted, err := screenJSONValue(dec, out, secrets)
				if err != nil {
					return false, err
				}
				redacted = redacted || childRedacted
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim('}') {
				return false, errors.New("unterminated JSON object")
			}
			out.WriteByte('}')
			return redacted, nil
		case '[':
			out.WriteByte('[')
			redacted := false
			for index := 0; dec.More(); index++ {
				if index > 0 {
					out.WriteByte(',')
				}
				childRedacted, err := screenJSONValue(dec, out, secrets)
				if err != nil {
					return false, err
				}
				redacted = redacted || childRedacted
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim(']') {
				return false, errors.New("unterminated JSON array")
			}
			out.WriteByte(']')
			return redacted, nil
		default:
			return false, fmt.Errorf("unexpected JSON delimiter %q", value)
		}
	case string:
		safe := sanitizeText(value, secrets)
		encoded, _ := json.Marshal(safe)
		out.Write(encoded)
		return safe != value, nil
	case json.Number:
		out.WriteString(value.String())
		return false, nil
	case bool:
		if value {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
		return false, nil
	case nil:
		out.WriteString("null")
		return false, nil
	default:
		return false, fmt.Errorf("unsupported JSON token %T", token)
	}
}

func isJSONNumberToken(raw string) bool {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return false
	}
	if _, ok := value.(json.Number); !ok {
		return false
	}
	var trailing any
	return errors.Is(dec.Decode(&trailing), io.EOF)
}

func screenDecimalField(field adapter.DecimalField, secrets []string) adapter.DecimalField {
	field.Error = sanitizeText(field.Error, secrets)
	if field.Raw == "" || isJSONNumberToken(field.Raw) {
		return field
	}
	screened := screenRawJSON([]byte(field.Raw), secrets)
	if screened.Withheld {
		field.Raw = ""
		field.Error = appendDiagnostic(field.Error, screened.Reason+"; rawSha256="+screened.OriginalSHA256)
		return field
	}
	field.Raw = string(screened.Bytes)
	if screened.Sanitized {
		field.Error = appendDiagnostic(field.Error, "invalid nonnumeric billing evidence credential-redacted")
	}
	return field
}

func screenBooleanField(field adapter.BooleanField, secrets []string) adapter.BooleanField {
	field.Error = sanitizeText(field.Error, secrets)
	trimmed := strings.TrimSpace(field.Raw)
	if field.Raw == "" || trimmed == "true" || trimmed == "false" {
		return field
	}
	screened := screenRawJSON([]byte(field.Raw), secrets)
	if screened.Withheld {
		field.Raw = ""
		field.Error = appendDiagnostic(field.Error, screened.Reason+"; rawSha256="+screened.OriginalSHA256)
		return field
	}
	field.Raw = string(screened.Bytes)
	if screened.Sanitized {
		field.Error = appendDiagnostic(field.Error, "invalid nonboolean billing evidence credential-redacted")
	}
	return field
}

func appendDiagnostic(existing, next string) string {
	if existing == "" {
		return next
	}
	if next == "" {
		return existing
	}
	return existing + "; " + next
}

func screenBillingObservation(b adapter.BillingObservation, secrets []string) adapter.BillingObservation {
	b.Cost = screenDecimalField(b.Cost, secrets)
	b.InputTokens = screenDecimalField(b.InputTokens, secrets)
	b.OutputTokens = screenDecimalField(b.OutputTokens, secrets)
	b.TotalTokens = screenDecimalField(b.TotalTokens, secrets)
	b.PromptTokenDetails.CachedTokens = screenDecimalField(b.PromptTokenDetails.CachedTokens, secrets)
	b.PromptTokenDetails.CacheWriteTokens = screenDecimalField(b.PromptTokenDetails.CacheWriteTokens, secrets)
	b.PromptTokenDetails.AudioTokens = screenDecimalField(b.PromptTokenDetails.AudioTokens, secrets)
	b.PromptTokenDetails.VideoTokens = screenDecimalField(b.PromptTokenDetails.VideoTokens, secrets)
	b.PromptTokenDetails.Error = sanitizeText(b.PromptTokenDetails.Error, secrets)
	b.CompletionTokenDetails.ReasoningTokens = screenDecimalField(b.CompletionTokenDetails.ReasoningTokens, secrets)
	b.CompletionTokenDetails.ImageTokens = screenDecimalField(b.CompletionTokenDetails.ImageTokens, secrets)
	b.CompletionTokenDetails.AudioTokens = screenDecimalField(b.CompletionTokenDetails.AudioTokens, secrets)
	b.CompletionTokenDetails.AcceptedPredictionTokens = screenDecimalField(b.CompletionTokenDetails.AcceptedPredictionTokens, secrets)
	b.CompletionTokenDetails.RejectedPredictionTokens = screenDecimalField(b.CompletionTokenDetails.RejectedPredictionTokens, secrets)
	b.CompletionTokenDetails.Error = sanitizeText(b.CompletionTokenDetails.Error, secrets)
	b.CostDetails.UpstreamInferenceCost = screenDecimalField(b.CostDetails.UpstreamInferenceCost, secrets)
	b.CostDetails.UpstreamInferencePromptCost = screenDecimalField(b.CostDetails.UpstreamInferencePromptCost, secrets)
	b.CostDetails.UpstreamInferenceCompletionsCost = screenDecimalField(b.CostDetails.UpstreamInferenceCompletionsCost, secrets)
	b.CostDetails.Error = sanitizeText(b.CostDetails.Error, secrets)
	b.IsBYOK = screenBooleanField(b.IsBYOK, secrets)
	b.RateSemanticsDiscrepancy = sanitizeText(b.RateSemanticsDiscrepancy, secrets)
	b.Error = sanitizeText(b.Error, secrets)
	return b
}

func screenObservation(obs adapter.AttemptObservation, secrets []string) (adapter.AttemptObservation, screenedRaw) {
	raw := screenRawJSON(obs.RawResponse, secrets)
	obs.RawResponse = raw.Bytes
	obs.RawSHA256 = raw.OriginalSHA256
	obs.RequestID = sanitizeText(obs.RequestID, secrets)
	obs.ResponseModel = sanitizeText(obs.ResponseModel, secrets)
	obs.ResponseProvider = sanitizeText(obs.ResponseProvider, secrets)
	obs.ReadError = sanitizeText(obs.ReadError, secrets)
	obs.RetryAfter = sanitizeText(obs.RetryAfter, secrets)
	headers := make(map[string]string, len(obs.ResponseHeaders))
	for key, value := range obs.ResponseHeaders {
		if containsSecret(key, secrets) {
			continue
		}
		headers[key] = sanitizeText(value, secrets)
	}
	obs.ResponseHeaders = headers
	obs.Billing = screenBillingObservation(obs.Billing, secrets)
	if obs.Decision != nil {
		copy := *obs.Decision
		copy.Choice = sanitizeText(copy.Choice, secrets)
		if copy.Probabilities != nil {
			probabilities := make(map[string]float64, len(copy.Probabilities))
			for key, value := range copy.Probabilities {
				probabilities[sanitizeText(key, secrets)] = value
			}
			copy.Probabilities = probabilities
		}
		obs.Decision = &copy
	}
	if obs.Chat != nil {
		copy := *obs.Chat
		if copy.Content != nil {
			content := make(map[string]any, len(copy.Content))
			for key, value := range copy.Content {
				safeKey := sanitizeText(key, secrets)
				if text, ok := value.(string); ok {
					value = sanitizeText(text, secrets)
				}
				content[safeKey] = value
			}
			copy.Content = content
		}
		obs.Chat = &copy
	}
	return obs, raw
}
