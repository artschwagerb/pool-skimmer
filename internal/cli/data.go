package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"pool-skimmer/internal/scim"
)

const maxInputBytes = 32 << 20

func readJSONArgument(command *cobra.Command, value string) (any, error) {
	var reader io.Reader
	switch {
	case value == "-":
		reader = command.InOrStdin()
	case strings.HasPrefix(value, "@"):
		path := strings.TrimPrefix(value, "@")
		if path == "" {
			return nil, errors.New("JSON file path after @ must not be empty")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("could not read JSON file: %w", err)
		}
		defer file.Close()
		reader = file
	default:
		reader = strings.NewReader(value)
	}

	data, err := io.ReadAll(io.LimitReader(reader, maxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read JSON: %w", err)
	}
	if len(data) > maxInputBytes {
		return nil, fmt.Errorf("JSON input exceeded %d bytes", maxInputBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("invalid JSON: multiple values supplied")
		}
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return result, nil
}

func objectData(command *cobra.Command, value, operation string) (map[string]any, error) {
	decoded, err := readJSONArgument(command, value)
	if err != nil {
		return nil, err
	}
	object, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s data must be a JSON object", operation)
	}
	return object, nil
}

func patchData(command *cobra.Command, data string, adds, replacements, removals []string) (map[string]any, error) {
	hasFlags := len(adds)+len(replacements)+len(removals) > 0
	if data != "" && hasFlags {
		return nil, errors.New("use either --data or operation flags, not both")
	}
	if data != "" {
		decoded, err := readJSONArgument(command, data)
		if err != nil {
			return nil, err
		}
		switch value := decoded.(type) {
		case map[string]any:
			return value, nil
		case []any:
			for _, operation := range value {
				if _, ok := operation.(map[string]any); !ok {
					return nil, errors.New("patch Operations array must contain JSON objects")
				}
			}
			return patchDocument(value), nil
		default:
			return nil, errors.New("patch data must be a PatchOp object or Operations array")
		}
	}

	operations := make([]any, 0, len(adds)+len(replacements)+len(removals))
	for _, item := range adds {
		path, value, err := pathValue(item)
		if err != nil {
			return nil, err
		}
		operations = append(operations, map[string]any{"op": "add", "path": path, "value": value})
	}
	for _, item := range replacements {
		path, value, err := pathValue(item)
		if err != nil {
			return nil, err
		}
		operations = append(operations, map[string]any{"op": "replace", "path": path, "value": value})
	}
	for _, path := range removals {
		if strings.TrimSpace(path) == "" {
			return nil, errors.New("remove path must not be empty")
		}
		operations = append(operations, map[string]any{"op": "remove", "path": path})
	}
	if len(operations) == 0 {
		return nil, errors.New("provide --add, --replace, --remove, or --data")
	}
	return patchDocument(operations), nil
}

func pathValue(item string) (string, any, error) {
	path, rawValue, ok := strings.Cut(item, "=")
	if !ok {
		return "", nil, fmt.Errorf("expected PATH=VALUE, got %q", item)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil, errors.New("attribute path must not be empty")
	}
	decoder := json.NewDecoder(strings.NewReader(rawValue))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return path, rawValue, nil
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return path, rawValue, nil
	}
	return path, value, nil
}

func patchDocument(operations []any) map[string]any {
	return map[string]any{
		"schemas":    []string{scim.PatchOpSchema},
		"Operations": operations,
	}
}
