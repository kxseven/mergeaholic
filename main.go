package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const usage = `Usage: mergeaholic [options] <bottom-layer> <layer> [<top-layer> ...]

Deep merge JSON or YAML files. Files are applied from left to right, so later
files take precedence. Objects/maps are merged recursively; arrays/lists and
scalar values are replaced by the higher layer.

Options:
  -o, --output <file>  Write the result to a file instead of stdout
  -h, --help           Show this help
`

type fileType int

const (
	typeJSON fileType = iota
	typeYAML
)

type options struct {
	inputs []string
	output string
	help   bool
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
			merged = layer
		} else {
			merged = deepMerge(merged, layer)
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
	lowerMap, lowerOK := lower.(map[string]any)
	higherMap, higherOK := higher.(map[string]any)
	if !lowerOK || !higherOK {
		return higher
	}

	result := make(map[string]any, len(lowerMap)+len(higherMap))
	for key, value := range lowerMap {
		result[key] = value
	}
	for key, higherValue := range higherMap {
		if lowerValue, exists := result[key]; exists {
			result[key] = deepMerge(lowerValue, higherValue)
		} else {
			result[key] = higherValue
		}
	}
	return result
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
