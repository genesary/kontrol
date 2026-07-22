// Command security-hub aggregates OpenSSF Scorecard results across a
// self-hosted GitLab instance into a single HTML report.
package main

import "github.com/genesary/security-hub/cmd"

func main() {
	cmd.Execute()
}
