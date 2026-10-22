// Package buildinfo guarda a versão, o commit e a data do build, injetados
// via -ldflags pelo Dockerfile e pelo Makefile.
package buildinfo

import "fmt"

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String resume a versão para o log e para --version.
func String() string {
	return fmt.Sprintf("%s (commit %s, build %s)", Version, Commit, Date)
}
