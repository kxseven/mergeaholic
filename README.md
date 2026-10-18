# mergeaholic

`mergeaholic` is a standalone Go CLI for deep-merging JSON, JSONC, or YAML configuration files.

Files are listed from the bottom layer to the top layer. Later files take precedence:

- objects/maps merge recursively;
- arrays/lists are replaced by the higher layer;
- scalar values and explicit `null` values replace lower-layer values.

JSON (`.json`, `.jsonc`) and YAML (`.yml`, `.yaml`) belong to separate format families and cannot
be mixed in one invocation. JSONC comments and trailing commas are accepted, but output is ordinary
formatted JSON without comments.

## Usage

```text
mergeaholic [options] <bottom-layer> <layer> [<top-layer> ...]

Options:
  -o, --output <file>  Write the result to a file instead of stdout
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
Existing output files are overwritten.

## Example

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

GitHub Actions runs formatting checks, tests, vet, and a build for pushes and
pull requests targeting `master`.

[Release Please](https://github.com/googleapis/release-please) maintains release
pull requests from [Conventional Commits](https://www.conventionalcommits.org/).
For example:

```text
feat: add a new merge option
fix: reject malformed JSONC input
feat!: change array merge semantics
```

Merging a Release Please pull request creates a GitHub release and builds
archives for Linux, macOS, and Windows on amd64 and arm64. Each archive is
published with a SHA-256 checksum file.
