# This project is archived

In the years since I created grb, the pure-Go alternatives to the cgo bits of
the standard library have improved and `-trimpath` has been added. Using
cross-compiled builds in production is therefore more reasonable, and the need
for grb has lessened. Additionally, grb was created before modules and hasn't
been updated to accomodate a non-GOPATH world. Therefore, I have archived this
project and will not be updating it in the future.

---

# grb (Go Remote Build)

grb is a remote build server for Go packages. It's useful as an alternative to cross-compiling, particularly
for deployment scenarios:

* CGo + cross-compiling is problematic
* With a build server, you always build production binaries with a standard Go version

## Installation

Go 1.9+ is required.

Build the server with `go build -o grbserver github.com/cespare/grb/cmd/grbserver`. Run `grbserver -h` to see
the flags options.

Install the client with `go get -u github.com/cespare/grb`.

In your environment, export `GRB_SERVER_URL=https://your-server.com`.
Then you can use `grb` as you would use `go build`, except that the output artifact is built on the server.

Various `go build` options are supported:

* `-o`
* `-race`
* `-ldflags`

## Example

If your build server is on Linux/amd64, you can get a Linux/amd64 build of [Rob Pike's
ivy](http://godoc.org/robpike.io/ivy) even if your current platform is something else:

```
go get robpike.io/ivy # fetch the source and dependencies
grb -o ivy.linux robpike.io/ivy
```

## TO(maybe)DO but probably not

* Parallelism (note that none of these steps take as long as just downloading a several MB binary in typical
  scenarios, so it's not a priority):
  * SHA-256 hashing of build tree
  * File uploads
  * Virtual GOPATH construction (on server side)
* Cache build artifacts
