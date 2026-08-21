package publicapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const MaxInputBytes = 1 << 20

type VersionedInput interface {
	SchemaVersionValue() string
}

func DecodeValue(value any, expected string, target VersionedInput) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode input: %w", err)
	}
	return DecodeJSON(data, expected, target)
}

func DecodeJSON(data []byte, expected string, target VersionedInput) error {
	if len(data) == 0 {
		return fmt.Errorf("input is empty")
	}
	if len(data) > MaxInputBytes {
		return fmt.Errorf("input exceeds %d bytes", MaxInputBytes)
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid input JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return err
	}
	if target.SchemaVersionValue() != expected {
		return fmt.Errorf("unsupported schema_version %q; expected %q", target.SchemaVersionValue(), expected)
	}
	return nil
}

func DecodeToolCall(data []byte, target any) error {
	if len(data) > MaxInputBytes {
		return fmt.Errorf("tool call exceeds %d bytes", MaxInputBytes)
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid tool call: %w", err)
	}
	return ensureEOF(decoder)
}

func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := walkJSONValue(decoder); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return ensureEOF(decoder)
}

func walkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("unterminated object")
		}
	case '[':
		for decoder.More() {
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("unterminated array")
		}
	default:
		return fmt.Errorf("unexpected delimiter %q", delim)
	}
	return nil
}

func ensureEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return fmt.Errorf("multiple JSON values are not allowed")
}
