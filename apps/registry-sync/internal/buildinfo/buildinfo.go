// Package buildinfo guarda a versão do binário, injetada no build com
// -ldflags "-X .../buildinfo.Version=1.2.0 -X .../buildinfo.Commit=... -X .../buildinfo.Date=...".
package buildinfo

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String devolve "1.2.0 (abc1234, 2026-09-28T12:00:00Z)".
func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}
