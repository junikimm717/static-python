// Package buildinfo holds values stamped into the staticpy binary at link
// time. The ./staticpy shim and any CI that `go build`s the executable pass
// the same -X flag; a binary built without it reports an empty revision.
package buildinfo

// GitRevision is the commit the running executable was built from.
// Set with:
//
//	-ldflags "-X github.com/junikimm717/static-python/src/staticpy/internal/buildinfo.GitRevision=<sha>"
var GitRevision string
