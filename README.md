# grb (Go Remote Build)

grb is a remote build server for Go packages. It's useful as an alternative to cross-compiling, particularly
for deployment scenarios:

* CGo + cross-compiling is problematic
* With a build server, you always build production binaries with a standard Go version

## Installation

Build the server with `go build -o grbserver`. Run `grbserver -h` to see the flags options.

Install the client with `go get -u github.com/cespare/grb/cmd/grb`.

In your environment, export `GRB_SERVER_URL=https://your-server.com`.
Then you can use `grb` as you would use `go build`, except that the output artifact is built on the server.

Two `go build` options are supported: `-o` (set output name) and `-race` (for building a race detector
binary).

## Example

If your build server is on Linux/amd64, you can get a Linux/amd64 build of [Rob Pike's
ivy](http://godoc.org/robpike.io/ivy) even if your current platform is something else:

```
go get robpike.io/ivy # fetch the source and dependencies
grb -o ivy.linux robpike.io/ivy
```

## TODO

* Plumb -race through
* Better error messages and logging all around
* Parallelize uploads
* Cache build artifacts?
* Optionally log server info about the Go version
