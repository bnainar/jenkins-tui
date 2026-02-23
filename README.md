# jenx

Minimal Jenkins permutation runner TUI built with Charmbracelet.

## Features

- Load multiple Jenkins targets from `jenkins.yaml`
- Pick one Jenkins and one parameterized pipeline job
- Multi-select `Choice` params to generate permutations
- Keep non-choice params fixed across all permutations
- Preview all generated job specs (hard limit: 20)
- Trigger and track builds with concurrency `4`
- Show queue/build URLs and open selected build in browser (`o`)
- Retry failed jobs only from completion screen (`r`)

## Requirements

- ASDF with `golang` plugin
- Go pinned via `.tool-versions` (`golang 1.26`)

## Setup

1. Create config from example:

```bash
cp jenkins.example.yaml jenkins.yaml
```

2. Build:

```bash
go build ./cmd/jenx
```

3. Run:

```bash
./jenx
```

## Local Jenkins (Docker)

Starts Jenkins and seeds one parameterized pipeline `perm-test`.

```bash
docker compose up -d --build
./dev/scripts/wait-jenkins.sh
./dev/scripts/create-token.sh > jenkins.yaml
```

Then start `jenx`, select `local`, choose `perm-test`, and run permutations.

## Controls

- `enter`: select/continue
- `esc`: back
- `q`: quit
- `o`: open selected build URL (run view)
- `r`: rebuild failed jobs only (done view)
