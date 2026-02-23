# jenx

`jenx` is a terminal UI for running Jenkins parameterized pipelines in bulk using value permutations.

## What It Does

- Loads Jenkins instances from local `jenkins.yaml`
- Lets you pick a Jenkins host, then a parameterized pipeline
- Supports multi-select on Jenkins `Choice` params
- Generates cartesian permutations (hard limit: `20` runs)
- Executes all generated runs with concurrency `4`
- Tracks queue/build status until completion
- Shows build URLs and opens selected URL in browser (`o`)
- Caches discovered jobs locally with a 24h TTL for faster startup

## Requirements

- ASDF with `golang` plugin
- `.tool-versions` is pinned to:

```txt
golang 1.26
```

## Configuration

Create local config (not committed):

```bash
cp jenkins.example.yaml jenkins.yaml
```

`jenkins.yaml` format:

```yaml
jenkins:
  - name: prod
    host: https://jenkins.example.com
    username: your-user
    token: your-api-token
    insecure_skip_tls_verify: false
```

Notes:
- `jenkins.yaml` is ignored by git.
- Use Jenkins API token auth (`username` + `token`).

## Run

Using Make:

```bash
make build
make run
```

Or directly:

```bash
go build ./cmd/jenx
./jenx
```

## TUI Flow

1. Select Jenkins host
2. Select pipeline job (press `/` to filter)
3. Fill parameters
- `Choice` params: multi-select
- Non-choice params: single value reused across all permutations
4. Review generated permutations
5. Press `Enter` to execute all
6. Watch live progress and open links

## Keybindings

Global:
- `q` quit
- `ctrl+c` quit

List screens:
- `/` start filter
- `enter` select
- `esc` back/clear

Run screen:
- `o` open selected build URL

Done screen:
- `r` prepare rerun for failed items

## Cache

Job discovery cache is stored under your OS user cache directory (for example macOS: `~/Library/Caches/jenx`).

- TTL: `24h`
- Cache key: Jenkins `host + username`

## Local Jenkins Dev Environment

Bring up local Jenkins and seed a sample parameterized pipeline:

```bash
make docker-up
./dev/scripts/wait-jenkins.sh
./dev/scripts/create-token.sh > jenkins.yaml
```

Then run `make run` and select the seeded job.

## Make Targets

- `make build`
- `make run`
- `make test`
- `make tidy`
- `make fmt`
- `make clean`
- `make docker-up`
- `make docker-down`
- `make docker-logs`
