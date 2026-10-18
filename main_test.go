package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeepMerge(t *testing.T) {
	lower := map[string]any{
		"name": "lower",
		"nested": map[string]any{
			"kept":  true,
			"value": "old",
		},
		"items": []any{"a", "b"},
	}
	higher := map[string]any{
		"name": "higher",
		"nested": map[string]any{
			"value": "new",
			"added": 42,
		},
		"items": []any{"c"},
		"empty": nil,
	}
	want := map[string]any{
		"name": "higher",
		"nested": map[string]any{
			"kept":  true,
			"value": "new",
			"added": 42,
		},
		"items": []any{"c"},
		"empty": nil,
	}

	if got := deepMerge(lower, higher); !reflect.DeepEqual(got, want) {
		t.Fatalf("deepMerge() = %#v, want %#v", got, want)
	}
}

func TestDeepMergeHigherNonMapReplacesMap(t *testing.T) {
	lower := map[string]any{"nested": map[string]any{"key": "value"}}
	higher := map[string]any{"nested": "replacement"}
	want := map[string]any{"nested": "replacement"}

	if got := deepMerge(lower, higher); !reflect.DeepEqual(got, want) {
		t.Fatalf("deepMerge() = %#v, want %#v", got, want)
	}
}

func TestNormalizeJSONC(t *testing.T) {
	input := []byte(`{
  // a line comment
  "url": "https://example.com/a//b",
  "text": "not /* a comment */",
  "nested": {
    "value": 1, /* a block comment */
  },
  "items": [1, 2,],
}`)

	normalized, err := normalizeJSONC(input)
	if err != nil {
		t.Fatal(err)
	}
	value, err := decode(normalized, typeJSON, false)
	if err != nil {
		t.Fatalf("normalized JSON did not parse: %v\n%s", err, normalized)
	}
	got := value.(map[string]any)
	if got["url"] != "https://example.com/a//b" || got["text"] != "not /* a comment */" {
		t.Fatalf("strings were altered: %#v", got)
	}
}

func TestNormalizeJSONCRejectsUnterminatedComment(t *testing.T) {
	_, err := normalizeJSONC([]byte(`{"value": 1 /* nope`))
	if err == nil || !strings.Contains(err.Error(), "unterminated block comment") {
		t.Fatalf("got error %v", err)
	}
}

func TestParseArgsAllowsOutputAnywhere(t *testing.T) {
	opts, err := parseArgs([]string{"base.json", "-o", "result.json", "top.json"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.output != "result.json" || !reflect.DeepEqual(opts.inputs, []string{"base.json", "top.json"}) {
		t.Fatalf("unexpected options: %#v", opts)
	}
}

func TestValidateExtensions(t *testing.T) {
	tests := []struct {
		name    string
		inputs  []string
		output  string
		wantErr string
	}{
		{name: "JSON and JSONC", inputs: []string{"a.json", "b.JSONC"}, output: "out.json"},
		{name: "YML and YAML", inputs: []string{"a.yml", "b.YAML"}, output: "out.yml"},
		{name: "mixed inputs", inputs: []string{"a.json", "b.yaml"}, wantErr: "cannot mix"},
		{name: "mismatched output", inputs: []string{"a.json", "b.json"}, output: "out.yaml", wantErr: "same file type"},
		{name: "unsupported input", inputs: []string{"a.toml", "b.toml"}, wantErr: "unsupported input extension"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateExtensions(tt.inputs, tt.output)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestExecuteJSONToStdout(t *testing.T) {
	dir := t.TempDir()
	base := writeTestFile(t, dir, "base.json", `{
  "service": {"host": "localhost", "port": 8080},
  "tags": ["base"]
}`)
	top := writeTestFile(t, dir, "top.jsonc", `{
  // retain host, replace port and tags
  "service": {"port": 9090,},
  "tags": ["top"],
}`)

	var stdout bytes.Buffer
	if err := execute(options{inputs: []string{base, top}}, &stdout); err != nil {
		t.Fatal(err)
	}
	const want = `{
  "service": {
    "host": "localhost",
    "port": 9090
  },
  "tags": [
    "top"
  ]
}
`
	if stdout.String() != want {
		t.Fatalf("output:\n%s\nwant:\n%s", stdout.String(), want)
	}
}

func TestExecuteYAMLToFile(t *testing.T) {
	dir := t.TempDir()
	base := writeTestFile(t, dir, "base.yml", "service:\n  host: localhost\n  port: 8080\ntags:\n  - base\n")
	top := writeTestFile(t, dir, "top.yaml", "service:\n  port: 9090\ntags:\n  - top\n")
	output := filepath.Join(dir, "merged.yaml")

	var stdout bytes.Buffer
	if err := execute(options{inputs: []string{base, top}, output: output}, &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout should be empty, got %q", stdout.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	const want = "service:\n    host: localhost\n    port: 9090\ntags:\n    - top\n"
	if string(data) != want {
		t.Fatalf("output:\n%s\nwant:\n%s", data, want)
	}
}

func writeTestFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
