// Command security-hub aggregates OpenSSF Scorecard results across a
// self-hosted GitLab instance into a single HTML report.
package main

import (
	"fmt"
	"os"
)

func main() {
	_, err := fmt.Fprintln(os.Stdout, "security-hub: not yet implemented")
	if err != nil {
		os.Exit(1)
	}
}
