package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const usage = `Usage: mergeaholic [options] <bottom-layer> <layer> [<top-layer> ...]

Deep merge JSON or YAML files. Files are applied from left to right, so later
files take precedence. Objects/maps are merged recursively; arrays/lists and
scalar values are replaced by the higher layer unless -au unions arrays.

Options:
  -o, --output <file>  Write the result to a file instead of stdout
  -au                  Union arrays, keeping first occurrences in order
  -ar                  Replace arrays (default); cannot combine with -au
  -h, --help           Show this help
`

type fileType int

const (
	typeJSON fileType = iota
	typeYAML
)

type arrayMode int

const (
	arrayReplace arrayMode = iota
	arrayUnion
)

type options struct {
	inputs    []string
	output    string
	help      bool
	arrayMode arrayMode
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "mergeaholic: %v\n\n%s", err, usage)
		os.Exit(2)
	}
	if opts.help {
		fmt.Print(usage)
		return
	}
	if err := execute(opts, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "mergeaholic: %v\n", err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (options, error) {
	var opts options
	positionalOnly := false
	var unionFlag, replaceFlag bool

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if positionalOnly {
			opts.inputs = append(opts.inputs, arg)
			continue
		}

		switch {
		case arg == "--":
			positionalOnly = true
		case arg == "-h" || arg == "--help":
			opts.help = true
		case arg == "-au":
			unionFlag = true
			opts.arrayMode = arrayUnion
		case arg == "-ar":
			replaceFlag = true
			opts.arrayMode = arrayReplace
		case arg == "-o" || arg == "--output":
			if i+1 >= len(args) {
				return options{}, fmt.Errorf("%s requires a filename", arg)
			}
			i++
			opts.output = args[i]
			if opts.output == "" {
				return options{}, errors.New("output filename cannot be empty")
			}
		case strings.HasPrefix(arg, "--output="):
			opts.output = strings.TrimPrefix(arg, "--output=")
			if opts.output == "" {
				return options{}, errors.New("output filename cannot be empty")
			}
		case strings.HasPrefix(arg, "-"):
			return options{}, fmt.Errorf("unknown option %q", arg)
		default:
			opts.inputs = append(opts.inputs, arg)
		}
	}

	if unionFlag && replaceFlag {
		return options{}, errors.New("-au and -ar are mutually exclusive")
	}
	if opts.help {
		return opts, nil
	}
	if len(opts.inputs) < 2 {
		return options{}, errors.New("at least two input files are required")
	}
	return opts, nil
}

func execute(opts options, stdout io.Writer) error {
	kind, err := validateExtensions(opts.inputs, opts.output)
	if err != nil {
		return err
	}

	var merged any
	for i, name := range opts.inputs {
		data, err := os.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read %q: %w", name, err)
		}
		layer, err := decode(data, kind, strings.ToLower(filepath.Ext(name)) == ".jsonc")
		if err != nil {
			return fmt.Errorf("parse %q: %w", name, err)
		}
		if i == 0 {
			merged = mergeWithMode(nil, layer, opts.arrayMode)
		} else {
			merged = mergeWithMode(merged, layer, opts.arrayMode)
		}
	}

	result, err := encode(merged, kind)
	if err != nil {
		return fmt.Errorf("encode merged result: %w", err)
	}
	if opts.output == "" {
		if _, err := stdout.Write(result); err != nil {
			return fmt.Errorf("write stdout: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(opts.output, result, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", opts.output, err)
	}
	return nil
}

func validateExtensions(inputs []string, output string) (fileType, error) {
	first, ok := extensionType(inputs[0])
	if !ok {
		return 0, fmt.Errorf("unsupported input extension for %q (use .json, .jsonc, .yml, or .yaml)", inputs[0])
	}
	for _, name := range inputs[1:] {
		kind, ok := extensionType(name)
		if !ok {
			return 0, fmt.Errorf("unsupported input extension for %q (use .json, .jsonc, .yml, or .yaml)", name)
		}
		if kind != first {
			return 0, fmt.Errorf("cannot mix JSON and YAML inputs: %q does not match %q", name, inputs[0])
		}
	}
	if output != "" {
		kind, ok := extensionType(output)
		if !ok {
			return 0, fmt.Errorf("unsupported output extension for %q (use .json, .jsonc, .yml, or .yaml)", output)
		}
		if kind != first {
			return 0, fmt.Errorf("output %q must use the same file type as the inputs", output)
		}
	}
	return first, nil
}

func extensionType(name string) (fileType, bool) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json", ".jsonc":
		return typeJSON, true
	case ".yml", ".yaml":
		return typeYAML, true
	default:
		return 0, false
	}
}

func decode(data []byte, kind fileType, jsonc bool) (any, error) {
	if kind == typeJSON {
		if jsonc {
			var err error
			data, err = normalizeJSONC(data)
			if err != nil {
				return nil, err
			}
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		if err := requireEOF(decoder); err != nil {
			return nil, err
		}
		return value, nil
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple YAML documents are not supported")
		}
		return nil, err
	}
	return normalizeYAML(value)
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values are not supported")
		}
		return err
	}
	return nil
}

func normalizeYAML(value any) (any, error) {
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, child := range value {
			normalized, err := normalizeYAML(child)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case map[any]any:
		result := make(map[string]any, len(value))
		for rawKey, child := range value {
			key, ok := rawKey.(string)
			if !ok {
				return nil, fmt.Errorf("mapping key %v is not a string", rawKey)
			}
			normalized, err := normalizeYAML(child)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case []any:
		result := make([]any, len(value))
		for i, child := range value {
			normalized, err := normalizeYAML(child)
			if err != nil {
				return nil, err
			}
			result[i] = normalized
		}
		return result, nil
	default:
		return value, nil
	}
}

func deepMerge(lower, higher any) any {
	return mergeWithMode(lower, higher, arrayReplace)
}

func mergeWithMode(lower, higher any, mode arrayMode) any {
	if lowerMap, ok := lower.(map[string]any); ok {
		if higherMap, ok := higher.(map[string]any); ok {
			result := make(map[string]any, len(lowerMap)+len(higherMap))
			for key, value := range lowerMap {
				result[key] = value
			}
			for key, value := range higherMap {
				result[key] = mergeWithMode(result[key], value, mode)
			}
			return result
		}
	}
	if mode == arrayUnion {
		if higherArray, ok := higher.([]any); ok {
			var combined []any
			if lowerArray, ok := lower.([]any); ok {
				combined = append(combined, lowerArray...)
			}
			combined = append(combined, higherArray...)
			result := make([]any, 0, len(combined))
			for _, value := range combined {
				// Normalize nested arrays even when this array is a new key or first layer.
				value = mergeWithMode(nil, value, mode)
				found := false
				for _, previous := range result {
					if sameValue(previous, value) {
						found = true
						break
					}
				}
				if !found {
					result = append(result, value)
				}
			}
			return result
		}
		if higherMap, ok := higher.(map[string]any); ok {
			result := make(map[string]any, len(higherMap))
			for key, value := range higherMap {
				result[key] = mergeWithMode(nil, value, mode)
			}
			return result
		}
	}
	return higher
}

// sameValue compares structured values, including maps irrespective of key order.
// Numeric spellings (for example 1, 1.0, and 1e0) represent the same value.
func sameValue(a, b any) bool {
	if left, ok := numericValue(a); ok {
		right, ok := numericValue(b)
		return ok && left.Cmp(right) == 0
	}
	if left, ok := a.(map[string]any); ok {
		right, ok := b.(map[string]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for key, value := range left {
			other, exists := right[key]
			if !exists || !sameValue(value, other) {
				return false
			}
		}
		return true
	}
	if left, ok := a.([]any); ok {
		right, ok := b.([]any)
		if !ok || len(left) != len(right) {
			return false
		}
		for i, value := range left {
			if !sameValue(value, right[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

func numericValue(value any) (*big.Rat, bool) {
	var text string
	switch n := value.(type) {
	case json.Number:
		text = string(n)
	case int:
		text = strconv.FormatInt(int64(n), 10)
	case int64:
		text = strconv.FormatInt(n, 10)
	case uint64:
		text = strconv.FormatUint(n, 10)
	case float64:
		text = strconv.FormatFloat(n, 'g', -1, 64)
	case float32:
		text = strconv.FormatFloat(float64(n), 'g', -1, 32)
	default:
		return nil, false
	}
	n, ok := new(big.Rat).SetString(text)
	return n, ok
}

func encode(value any, kind fileType) ([]byte, error) {
	if kind == typeJSON {
		result, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(result, '\n'), nil
	}
	return yaml.Marshal(value)
}

// normalizeJSONC removes line comments, block comments, and trailing commas
// while preserving strings and line breaks. Replaced bytes become spaces so
// syntax errors retain useful line and column positions.
func normalizeJSONC(input []byte) ([]byte, error) {
	result := append([]byte(nil), input...)
	inString := false
	escaped := false

	for i := 0; i < len(result); i++ {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch result[i] {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		if result[i] == '"' {
			inString = true
			continue
		}
		if result[i] != '/' || i+1 >= len(result) {
			continue
		}

		switch result[i+1] {
		case '/':
			result[i], result[i+1] = ' ', ' '
			i += 2
			for ; i < len(result) && result[i] != '\n' && result[i] != '\r'; i++ {
				result[i] = ' '
			}
			i--
		case '*':
			start := i
			result[i], result[i+1] = ' ', ' '
			i += 2
			closed := false
			for ; i < len(result); i++ {
				if i+1 < len(result) && result[i] == '*' && result[i+1] == '/' {
					result[i], result[i+1] = ' ', ' '
					i++
					closed = true
					break
				}
				if result[i] != '\n' && result[i] != '\r' {
					result[i] = ' '
				}
			}
			if !closed {
				return nil, fmt.Errorf("unterminated block comment at byte %d", start)
			}
		}
	}

	inString, escaped = false, false
	for i := 0; i < len(result); i++ {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch result[i] {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		if result[i] == '"' {
			inString = true
			continue
		}
		if result[i] != ',' {
			continue
		}
		j := i + 1
		for j < len(result) && (result[j] == ' ' || result[j] == '\t' || result[j] == '\n' || result[j] == '\r') {
			j++
		}
		if j < len(result) && (result[j] == '}' || result[j] == ']') {
			result[i] = ' '
		}
	}
	return result, nil
}
