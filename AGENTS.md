# Repository Guidelines

## Project Structure & Module Organization
`Modulebalancingclient.go` is the main entrypoint for the Windows client process. Core logic is split into small packages: `api/` handles gRPC-facing download and analysis flows, `env/` holds shared configuration and utility code, `grpc/` contains generated protobuf stubs plus `ModuleBalancing.proto`, and `logmanager/` provides file-based business logs. Runtime configuration lives in `conf/config.yaml`. Generated binaries and runtime output should stay in `bin/`, `logs/`, and `temp/` rather than in source folders.

## Build, Test, and Development Commands
Use the Go toolchain directly; there is no `Makefile` in this repo.

- `go build -o bin/ModuleBalancingClient.exe .` builds the client binary.
- `go run .` starts the client with the local `conf/config.yaml`.
- `go test ./...` runs all package tests. Add tests with new logic even though the repo currently has limited coverage.
- `go fmt ./...` formats all Go packages before review.
- `protoc --go_out=. --go-grpc_out=. grpc/ModuleBalancing.proto` regenerates gRPC files after changing the proto schema.

## Coding Style & Naming Conventions
Follow standard Go formatting with tabs and `gofmt` output as the source of truth. Keep package names short and lowercase (`api`, `env`, `logmanager`). Exported identifiers use PascalCase; unexported helpers use camelCase. Prefer focused functions over large inline blocks, and keep configuration keys aligned with the existing YAML structure in `conf/config.yaml`.

## Testing Guidelines
Place tests next to the code they cover with `_test.go` filenames and `TestXxx` functions. Favor table-driven tests for parsing, file handling, and checksum logic in `api/` and `env/`. Run `go test ./...` before opening a PR. If a change touches generated gRPC code, verify both regenerated files and client startup still succeed.

## Commit & Pull Request Guidelines
Recent history uses short, task-focused messages, often in Chinese, such as `fix bugs: ...` or `增加download module初始化的大小`. Keep commits small and descriptive, ideally scoped to one fix or feature. PRs should include the behavior change, config or protocol impacts, manual verification steps, and log or console snippets when the change affects download, monitoring, or upgrade flows.

## Configuration & Runtime Notes
Do not commit environment-specific paths or secrets. `conf/config.yaml` contains local directories like `Common` and `Chkdir`; keep sample values generic when sharing changes. Treat `grpc/*.pb.go` as generated artifacts and edit `grpc/ModuleBalancing.proto` instead of patching generated code directly.
