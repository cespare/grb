# grb (Go Remote Build)

grb is a remote build server for Go packages.

(work in progress)

## Flow

* `POST /begin` with a list of package -> (filename, hash)
* get back build ID and a list of package, filename that the server needs (those that aren't cached)
* `POST /upload/<hash>` to upload each required file
* `GET /compile/<build-id>` to compile and get the result
