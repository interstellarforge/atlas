package benchmarks_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestServerBenchmarkRunnerAvoidsKnownContainerFailures(t *testing.T) {
	const path = "server.sh"

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("server benchmark runner: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("server benchmark runner is not executable")
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	script := string(contents)

	for _, required := range []string{
		"sh -ec",
		"safe.directory /repo.git",
		"git --git-dir=/repo.git archive",
		"test -f /src/go.mod",
		`export PATH="/usr/local/go/bin:$PATH"`,
	} {
		if !strings.Contains(script, required) {
			t.Errorf("runner does not contain required safeguard %q", required)
		}
	}
	for _, forbidden := range []string{"sh -lc", "git clone"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("runner still contains failure-prone command %q", forbidden)
		}
	}

	command := exec.Command("bash", "-n", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("invalid shell syntax: %v\n%s", err, output)
	}
}
