package gograph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type commandRunner interface {
	Run(ctx context.Context, executable string, args ...string) commandResult
}

type commandResult struct {
	stdout          []byte
	stderr          []byte
	exitCode        int
	err             error
	stdoutTruncated bool
	stderrTruncated bool
}

type osCommandRunner struct{}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	available := b.limit - b.buffer.Len()
	if available > 0 {
		if len(data) > available {
			_, _ = b.buffer.Write(data[:available])
		} else {
			_, _ = b.buffer.Write(data)
		}
	}
	if written > available {
		b.truncated = true
	}
	return written, nil
}

func (osCommandRunner) Run(ctx context.Context, executable string, args ...string) commandResult {
	stdout := &limitedBuffer{limit: maxProcessOutput}
	stderr := &limitedBuffer{limit: maxProcessOutput}
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		}
	}
	return commandResult{
		stdout: stdout.buffer.Bytes(), stderr: stderr.buffer.Bytes(), exitCode: exitCode, err: err,
		stdoutTruncated: stdout.truncated, stderrTruncated: stderr.truncated,
	}
}

func decodeStrict(data []byte, target any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("empty JSON document")
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("JSON document is not valid UTF-8")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(decoder, "$", nil); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func scanJSONValue(decoder *json.Decoder, location string, current json.Token) error {
	token := current
	var err error
	if token == nil {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("invalid object key at %s", location)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON key %q at %s", key, location)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, location+"."+key, nil); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := scanJSONValue(decoder, fmt.Sprintf("%s[%d]", location, index), nil); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("multiple JSON values are not allowed")
	}
	return err
}

func requireFields(data []byte, fields ...string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return fmt.Errorf("JSON document must be an object")
	}
	for _, field := range fields {
		if _, present := object[field]; !present {
			return fmt.Errorf("missing required field %s", field)
		}
	}
	return nil
}

func requireValidationFields(data []byte) error {
	top, err := rawObject(data, "validation document")
	if err != nil {
		return err
	}
	if err := requireRawFields(top, "validation document", "schema_version", "command", "gograph_version", "generated_at", "repository", "analysis", "request", "evaluation", "evidence"); err != nil {
		return err
	}
	for _, nested := range []struct {
		field string
		name  string
	}{
		{field: "repository", name: "repository"},
		{field: "analysis", name: "analysis"},
		{field: "request", name: "request"},
		{field: "evaluation", name: "evaluation"},
		{field: "evidence", name: "evidence"},
	} {
		if _, err := rawObject(top[nested.field], nested.name); err != nil {
			return err
		}
	}
	repository, _ := rawObject(top["repository"], "repository")
	if err := requireRawFields(repository, "repository", "root"); err != nil {
		return err
	}
	request, _ := rawObject(top["request"], "request")
	if err := requireRawFields(request, "request", "binding_fingerprint", "binding"); err != nil {
		return err
	}
	bindingObject, err := rawObject(request["binding"], "binding")
	if err != nil {
		return err
	}
	if err := requireRawFields(bindingObject, "binding", "schema_version", "predicate", "subject", "required_precision"); err != nil {
		return err
	}
	if _, err := rawObject(bindingObject["subject"], "binding subject"); err != nil {
		return err
	}
	evaluation, _ := rawObject(top["evaluation"], "evaluation")
	if err := requireRawFields(evaluation, "evaluation", "outcome", "reason", "diagnostics"); err != nil {
		return err
	}
	evidence, _ := rawObject(top["evidence"], "evidence")
	return requireRawFields(evidence, "evidence", "matched_relations")
}

func rawObject(data []byte, name string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%s must be a JSON object", name)
	}
	return object, nil
}

func requireRawFields(object map[string]json.RawMessage, name string, fields ...string) error {
	for _, field := range fields {
		if _, present := object[field]; !present {
			return fmt.Errorf("%s is missing required field %s", name, field)
		}
	}
	return nil
}

func canonicalDirectory(directory string) (string, error) {
	if strings.TrimSpace(directory) == "" {
		return "", fmt.Errorf("repository root is required")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository root is not a directory")
	}
	return filepath.Clean(real), nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}
