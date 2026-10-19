package main

import (
	"bytes"
	"encoding/json"
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

func TestArrayFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		mode arrayMode
		err  string
	}{
		{args: []string{"base.json", "top.json"}, mode: arrayReplace},
		{args: []string{"-ar", "base.json", "top.json"}, mode: arrayReplace},
		{args: []string{"base.json", "-au", "top.json", "-o", "out.json"}, mode: arrayUnion},
		{args: []string{"-au", "-ar", "base.json", "top.json"}, err: "mutually exclusive"},
		{args: []string{"-ar", "-au", "base.json", "top.json"}, err: "mutually exclusive"},
	} {
		opts, err := parseArgs(tc.args)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("%v: got %v, want %q", tc.args, err, tc.err)
			}
			continue
		}
		if err != nil || opts.arrayMode != tc.mode {
			t.Fatalf("%v: opts=%+v, err=%v", tc.args, opts, err)
		}
	}
	for _, flag := range []string{"-au", "-ar"} {
		if !strings.Contains(usage, flag) {
			t.Fatalf("help missing %s", flag)
		}
	}
}

func TestArrayUnionAcrossLayers(t *testing.T) {
	layers := []any{
		map[string]any{"items": []any{json.Number("1"), "x", json.Number("1.0")}, "nested": map[string]any{"items": []any{map[string]any{"a": 1, "b": []any{1, 2}}, nil}}},
		map[string]any{"items": []any{"x", true, json.Number("1e0")}, "nested": map[string]any{"items": []any{nil, map[string]any{"b": []any{1, 2}, "a": 1}, "new"}}},
		map[string]any{"items": []any{false, true, "x"}, "nested": map[string]any{"items": []any{"new", []any{1, 1, 2}, []any{1, 2}}}},
	}
	var merged any
	for _, layer := range layers {
		merged = mergeWithMode(merged, layer, arrayUnion)
	}
	want := map[string]any{
		"items":  []any{json.Number("1"), "x", true, false},
		"nested": map[string]any{"items": []any{map[string]any{"a": 1, "b": []any{1, 2}}, nil, "new", []any{1, 2}}},
	}
	if !reflect.DeepEqual(merged, want) {
		t.Fatalf("union = %#v, want %#v", merged, want)
	}
}

func TestArrayModesAndOverrides(t *testing.T) {
	lower := map[string]any{"items": []any{1, 1}, "keep": map[string]any{"value": "old"}, "gone": []any{1}}
	higher := map[string]any{"items": []any{}, "keep": map[string]any{"extra": true}, "gone": nil, "new": []any{2, 2}}
	for _, mode := range []arrayMode{arrayReplace, arrayUnion} {
		got := mergeWithMode(lower, higher, mode).(map[string]any)
		if len(got["items"].([]any)) != 1 && mode == arrayUnion {
			t.Fatalf("union should retain lower values: %#v", got)
		}
		if mode == arrayReplace && len(got["items"].([]any)) != 0 {
			t.Fatalf("replace should use empty top array: %#v", got)
		}
		if got["gone"] != nil || got["keep"].(map[string]any)["value"] != "old" {
			t.Fatalf("non-array rules changed: %#v", got)
		}
	}
	if got := mergeWithMode([]any{1}, []any{}, arrayUnion); !reflect.DeepEqual(got, []any{1}) {
		t.Fatalf("union with empty higher array = %#v", got)
	}
	if got := mergeWithMode([]any{1}, nil, arrayUnion); got != nil {
		t.Fatalf("null override = %#v", got)
	}
	if sameValue("1", json.Number("1")) || sameValue(true, json.Number("1")) || sameValue(nil, false) {
		t.Fatal("different JSON types compared equal")
	}
}

func TestExecuteArrayModesBothFormats(t *testing.T) {
	for _, tc := range []struct{ ext, base, top string }{
		{"jsonc", `{"items":[1,1,{"a":2,"b":3}]}`, `{//comment\n"items":[{"b":3,"a":2},2,],}`},
		{"yaml", "items: [1, 1, {a: 2, b: 3}]\n", "items: [{b: 3, a: 2}, 2]\n"},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			dir := t.TempDir()
			base := writeTestFile(t, dir, "base."+tc.ext, tc.base)
			topText := strings.ReplaceAll(tc.top, `\n`, "\n")
			top := writeTestFile(t, dir, "top."+tc.ext, topText)
			for _, mode := range []arrayMode{arrayReplace, arrayUnion} {
				var stdout bytes.Buffer
				out := filepath.Join(dir, "out."+tc.ext)
				if err := execute(options{inputs: []string{base, top}, arrayMode: mode}, &stdout); err != nil {
					t.Fatal(err)
				}
				if err := execute(options{inputs: []string{base, top}, output: out, arrayMode: mode}, &bytes.Buffer{}); err != nil {
					t.Fatal(err)
				}
				file, err := os.ReadFile(out)
				if err != nil || !bytes.Equal(file, stdout.Bytes()) {
					t.Fatalf("stdout/file mismatch: %s, %v", file, err)
				}
				value, err := decode(file, extensionKindForTest(tc.ext), tc.ext == "jsonc")
				if err != nil {
					t.Fatal(err)
				}
				items := value.(map[string]any)["items"].([]any)
				want := 2
				if mode == arrayUnion {
					want = 3
				}
				if len(items) != want {
					t.Fatalf("mode %d: items = %#v", mode, items)
				}
			}
		})
	}
}

func extensionKindForTest(ext string) fileType {
	if ext == "yaml" {
		return typeYAML
	}
	return typeJSON
}

func writeTestFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
