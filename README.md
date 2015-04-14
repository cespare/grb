# grb (Go Remote Build)

grb is a remote build server for Go packages.

(work in progress)

## Flow

* `POST /begin` with a list of package -> (filename, hash)
* get back build ID and a list of package, filename that the server needs (those that aren't cached)
* `POST /upload/<hash>` to upload each required file
* `GET /build/<build-id>` to compile and get the result

## TODO

* Check that it actually works cross-platform, including platform-specific files, asm, etc
* Better error messages and logging all around
* Parallelize uploads
* Cache build artifacts?
* Optionally log server info about the Go version
