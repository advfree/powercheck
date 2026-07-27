package buildinfo

import "fmt"

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func String(program string) string {
	return fmt.Sprintf("%s %s (commit %s, built %s)", program, Version, Commit, Date)
}
