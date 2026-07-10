# AGENTS.md

## Cursor Cloud specific instructions

### Project overview

tRPC-Agent-Go is a Go multi-module monorepo (library/framework) for building AI agent systems. It is **not** a standalone application — there is no single `main.go` to run. The root module path is `trpc.group/trpc-go/trpc-agent-go`.

### Fork-only repository policy (hard requirement)

- Treat `origin` (`https://github.com/XnLemon/trpc-agent-go.git`) as the user's fork and the **only writable remote**.
- Do not perform **any operation** against `upstream` (`https://github.com/trpc-group/trpc-agent-go.git`). This prohibition includes fetching, pulling, comparing, merging, rebasing, pushing, creating branches, opening pull requests, or modifying it through GitHub.
- Create all working branches and commits locally for publication to `origin`. All pushes, including force pushes and tags, must target `origin` unless the user explicitly overrides this policy.
- Pull requests must use branches in the user's `origin` fork for **both** the source/head branch and the target/base branch. Never open or modify a pull request involving `upstream`.
- Do not change the roles or URLs of `origin` and `upstream` without explicit user approval.

### Documentation verification and incremental PR policy (hard requirement)

- When implementation depends on current or external documentation, APIs, library behavior, platform limits, standards, release details, or other potentially changing facts, verify the latest documentation before making decisions.
- Prefer [`upstash/context7`](https://github.com/upstash/context7) for current library and framework documentation. If Context7 is unavailable, incomplete, or unsuitable, search the web and use official or primary sources.
- Never invent, assume, or rely only on memory for facts that require current documentation. Verify them first and clearly identify any remaining uncertainty.
- Divide work into small, independently reviewable increments. After completing each increment, run the relevant validation, commit it, push the branch to `origin`, and open a pull request within the user's fork.
- Every pull request must use an `origin` branch as both the source/head and target/base branch, and its description must clearly state the objective, completed changes, validation or tests performed, known risks or limitations, and any follow-up work.
- Do not batch unrelated completed increments into one pull request, and never involve `upstream` in the pull request workflow.

### Go version

The root `go.mod` requires Go 1.21. The environment has Go 1.22+ pre-installed, which is compatible. Some sub-modules (e.g. under `test/`) require Go 1.24+; `go mod download` handles this automatically via toolchain directives.

### Common commands

| Task | Command | Notes |
|------|---------|-------|
| Build | `go build ./...` | Root module only |
| Unit tests | `go test ./...` | Root module; all tests use mocks, no API keys needed |
| E2E tests | `cd test && go test ./...` | Separate module in `test/` |
| Lint | `golangci-lint run --timeout=10m` | Config in `.golangci.yml` |
| gofmt check | `gofmt -r 'interface{} -> any' -l .` | CI enforces `any` over `interface{}` |
| goimports check | `goimports -l .` | |
| All sub-module tests (CI-style) | `bash .github/scripts/run-go-tests.sh` | Runs tests across ~80 modules excluding examples/docs/test |
| Check example builds | `bash .github/scripts/check-examples.sh` | |

### Non-obvious caveats

- **GOPATH/bin must be on PATH** for `golangci-lint` and `goimports` to work. The update script handles installation, and `~/.bashrc` exports the path. If a tool is missing, run: `export PATH="$PATH:$(go env GOPATH)/bin"`.
- **No external API keys needed for tests.** The entire test suite uses mocks. API keys (e.g. `OPENAI_API_KEY`) are only needed to run the examples under `examples/`.
- **Multi-module monorepo:** There are ~80 `go.mod` files. Running `go test ./...` from the repo root only tests the root module. To test all modules, use the CI script `.github/scripts/run-go-tests.sh`.
- **SQLite CGO dependency:** The root module depends on `github.com/mattn/go-sqlite3`, which requires CGO. Ensure `CGO_ENABLED=1` (the default) and a C compiler is available.
- **License headers required on all `.go` files.** CI checks that every Go file has the Tencent Apache 2.0 header. See `CONTRIBUTING.md` for the template.
