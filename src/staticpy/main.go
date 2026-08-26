// Command staticpy builds fully static CPython interpreters for linux-musl
// targets.
//
// It is normally invoked through the ./staticpy shim, which provisions the
// toolchains, busybox and qemu it needs and then hands them over as flags.
// Run `staticpy help` for the full, self-documenting command surface.
package main

import (
	"os"

	"github.com/junikimm717/static-python/src/staticpy/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
