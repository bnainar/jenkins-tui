# jenkins-tui

`jenkins-tui` is a terminal UI for running Jenkins parameterized pipelines in bulk using value permutations.

## Install (Homebrew)

Tap and install:

```bash
brew tap bnainar/tap
brew install jenkins-tui
```

## What It Does

- Loads Jenkins targets from `jenkins.yaml` in your config directory
- Browses folders/jobs lazily (Jenkins UI style)
- Supports multi-select for Jenkins `Choice` params
- Generates cartesian permutations (hard limit: `20` runs)
- Executes all generated runs with concurrency `4`
- Tracks queue/build status until completion
- Opens selected build URL in browser (`o`)
- Caches folder listings with a 24h TTL for faster browsing

## Configuration

Default config path:

- `$XDG_CONFIG_HOME/jenkins-tui/jenkins.yaml`
- Fallback: `~/.config/jenkins-tui/jenkins.yaml`

If the config file does not exist yet, the app still starts; press `m` to add targets in-app.

Override config path:

- Flag: `-config /absolute/path/to/jenkins.yaml`
- Env: `JENKINS_TUI_CONFIG=/absolute/path/to/jenkins.yaml`

Config format:

```yaml
jenkins:
  - id: prod
    host: https://jenkins.example.com
    username: your-user
    insecure_skip_tls_verify: false
    credential:
      type: keyring
      ref: jenkins-tui/prod
```

`name` is optional. When omitted, it defaults to the target `id`.

### Credential Types

- `keyring`: token is stored in OS keychain/keyring, YAML stores only reference.
- `env`: `credential.ref` is an environment variable name containing the token.

Linux note:

- `keyring` requires a Secret Service backend.
- If unavailable (for example headless sessions), use `credential.type: env`.

### Manage Targets In-App

On the server selection screen:

- `m` open target management
- `a` add target
- `e` edit selected target
- `t` rotate selected target token (keyring targets)
- `d` delete selected target

### Choice Multi-Select Shortcuts

In parameter forms for Jenkins `Choice` fields:

- `space` or `x` toggles the currently highlighted option
- `ctrl+a` toggles select all / select none
- `/` enters filter mode

Filtering note:

- When a filter is active, `ctrl+a` applies to currently visible (filtered) options.

## Cache

Default cache path:

- `$XDG_CACHE_HOME/jenkins-tui`
- Fallback: `~/.cache/jenkins-tui`

Override cache path:

- Flag: `-cache-dir /absolute/path`
- Env: `JENKINS_TUI_CACHE_DIR=/absolute/path`

Version info:

- `jenkins-tui -v` (or `jenkins-tui -version`) prints version, commit, and build time.

Cache details:

- TTL: `24h`
- Cache key: Jenkins `host + username + folder URL`

## Run From Source (Dev)

Build and run with Docker-based toolchain:

```bash
make run
```

Override config/cache for local runs:

```bash
make run CONFIG=/absolute/path/jenkins.yaml CACHE_DIR=/absolute/path/cache
```

Run tests:

```bash
make test
```

## Local Jenkins Dev Environment

Bring up local Jenkins and seed a sample parameterized pipeline:

```bash
make docker-up
./dev/scripts/wait-jenkins.sh
./dev/scripts/create-token.sh > /tmp/jenkins.yaml
```

Then export the token env var printed by the script and run:

```bash
JENKINS_TUI_CONFIG=/tmp/jenkins.yaml make run
```

When running `jenkins-tui` in Docker, use `http://host.docker.internal:8080` instead of `http://localhost:8080`.

## Make Targets

- `make build` — build binary in Docker (output for your OS)
- `make run` — build then run the binary on the host
- `make run-docker` — build and run entirely inside Docker
- `make test` — run tests in Docker
- `make tidy` — go mod tidy in Docker
- `make fmt` — go fmt in Docker
- `make clean` — remove built binary
- `make docker-up` — start local Jenkins (dev)
- `make docker-down` — stop local Jenkins
- `make docker-logs` — follow Jenkins logs
