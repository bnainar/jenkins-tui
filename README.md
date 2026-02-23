# jenkins-tui

`jenkins-tui` is a terminal UI for running Jenkins parameterized pipelines in bulk using value permutations.

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

- **Docker only** — no Go or other dev tools required. All build, test, and code targets run inside Docker.

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

```bash
make run
```

This builds the binary in Docker and runs it on your machine. If you can't run the built binary (e.g. Windows without WSL), use `make run-docker` to run entirely inside a container.

Run tests:

```bash
make test
```

Note: when using `make run-docker`, opening URLs with `o` may not launch a browser on the host.

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

Job discovery cache is stored in Docker volume `jenkins-tui_app_cache` (`/root/.cache/jenkins-tui` inside the runtime container).

- TTL: `24h`
- Cache key: Jenkins `host + username`

## Local Jenkins Dev Environment

Bring up local Jenkins and seed a sample parameterized pipeline:

```bash
make docker-up
./dev/scripts/wait-jenkins.sh
./dev/scripts/create-token.sh > jenkins.yaml
```

Then run `make run`.

Since `jenkins-tui` runs in a container, set `jenkins.yaml` host to `http://host.docker.internal:8080` instead of `http://localhost:8080`.

## Make Targets

- `make build` — build binary in Docker (output for your OS)
- `make run` — build then run the binary on the host
- `make run-docker` — build and run entirely inside Docker (use if host binary won't run)
- `make test` — run tests in Docker
- `make tidy` — go mod tidy in Docker
- `make fmt` — go fmt in Docker
- `make clean` — remove built binary
- `make docker-up` — start local Jenkins (dev)
- `make docker-down` — stop local Jenkins
- `make docker-logs` — follow Jenkins logs
