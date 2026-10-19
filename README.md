# mergeaholic

`mergeaholic` is a standalone Go CLI for deep-merging JSON, JSONC, or YAML configuration files.

Files are listed from the bottom layer to the top layer. Later files take precedence:

- objects/maps merge recursively;
- arrays/lists are replaced by the higher layer by default; `-au` unions them, retaining each distinct value's first occurrence;
- scalar values and explicit `null` values replace lower-layer values.

JSON (`.json`, `.jsonc`) and YAML (`.yml`, `.yaml`) belong to separate format families and cannot
be mixed in one invocation. JSONC comments and trailing commas are accepted, but output is ordinary
formatted JSON without comments.

## Usage

```text
mergeaholic [options] <bottom-layer> <layer> [<top-layer> ...]

Options:
  -o, --output <file>  Write the result to a file instead of stdout
  -au                  Union arrays, keeping first occurrences in order
  -ar                  Replace arrays (default); cannot combine with -au
  -h, --help           Show help
```

Merge to stdout:

```sh
mergeaholic base.yaml environment.yaml local.yaml
```

Write to a file:

```sh
mergeaholic base.json overrides.jsonc -o merged.json
```

The output extension must belong to the same format family as the inputs.
Existing output files are overwritten. `-au` unions arrays at every depth, removing duplicates even within a single layer. Values are compared structurally (including nested objects regardless of key order); numerically equivalent values such as `1` and `1.0` count as one. A higher-layer `null` still replaces an array. `-ar` explicitly selects the default replacement behavior; `-au` and `-ar` cannot be used together.

## Examples

### JSON

Given `default.json`

```json
{"items":[1,2]}
```

and `user.json`

```json
{"items":[2,3]}
```

running:

```sh
mergeaholic -au default.json user.json
```

produces:

```json
{"items":[1,2,3]}
```

running either:

```sh
mergeaholic -ar default.json user.json
mergeaholic default.json user.json
```

produces:

```json
{"items":[2,3]}
```

### YAML

Given `base.yaml`:

```yaml
service:
  host: localhost
  port: 8080
tags:
  - base
```

and `production.yaml`:

```yaml
service:
  port: 80
tags:
  - production
```

running:

```sh
mergeaholic base.yaml production.yaml
```

produces:

```yaml
service:
  host: localhost
  port: 80
tags:
  - production
```


## Build

Go 1.22 or newer is required.

```sh
go build -o mergeaholic .
```

## Test

```sh
go test ./...
```

## Releases

[Release Please](https://github.com/googleapis/release-please) maintains release pull requests from [Conventional Commits](https://www.conventionalcommits.org/). For example:

```text
feat: add a new merge option
fix: reject malformed JSONC input
feat!: change array merge semantics
```

Merging a Release Please pull request creates a GitHub release and builds archives for Linux, macOS, and Windows on amd64 and arm64.
