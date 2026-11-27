// This module exists so that `go build ./...` and `go vet ./...` in the repository root
// skip web/, whose node_modules can hold Go files of npm packages.
module removarr.invalid/web

go 1.27
