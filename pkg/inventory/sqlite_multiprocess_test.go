package inventory_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/domain"
	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// Two real OS processes contend on one SQLite authoritative store. Goroutines in
// one process would share a single *sql.DB connection and never exercise SQLite's
// cross-process locking, so the helper re-executes this test binary.

const (
	helperDBEnv   = "AH_INVENTORY_HELPER_DB"
	helperRoleEnv = "AH_INVENTORY_HELPER_ROLE"
	helperRounds  = 40
	sharedRun     = "run-shared"
	resultPrefix  = "AHRESULT"
)

// stateFor gives each helper role a different terminal state, so every shared
// terminal key has exactly one legitimate winner.
func stateFor(role string) string {
	if role == "a" {
		return "Succeeded"
	}
	return "Failed"
}

func sharedArtifact(i int) domain.Artifact {
	return domain.Artifact{
		RunID: sharedRun, ProducerNodeID: "producer", ProducerAttemptID: "attempt-" + strconv.Itoa(i), OutputName: "dataset",
		ArtifactID: fmt.Sprintf("shared-%d", i), Digest: fmt.Sprintf("sha256:shared-%d", i), CreatedAt: time.Unix(0, 0).UTC(),
	}
}

func ownArtifact(role string, i int) domain.Artifact {
	return domain.Artifact{
		RunID: "run-" + role, ProducerNodeID: "producer", ProducerAttemptID: "attempt-" + strconv.Itoa(i), OutputName: "dataset",
		ArtifactID: fmt.Sprintf("own-%s-%d", role, i), Digest: "sha256:own", CreatedAt: time.Unix(0, 0).UTC(),
	}
}

func sharedTerminal(role string, i int) domain.NodeTerminalRecord {
	return domain.NodeTerminalRecord{RunID: sharedRun, NodeID: "producer", AttemptID: "attempt-" + strconv.Itoa(i),
		TerminalState: stateFor(role), RecordedAt: time.Unix(0, 0).UTC()}
}

// TestSQLiteHelperProcess is the child side of the two-process harness. It only
// runs when re-executed by TestSQLite_TwoProcessContention.
func TestSQLiteHelperProcess(t *testing.T) {
	path, role := os.Getenv(helperDBEnv), os.Getenv(helperRoleEnv)
	if path == "" {
		t.Skip("helper process for TestSQLite_TwoProcessContention")
	}
	s, err := inventory.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("helper %s open: %v", role, err)
	}
	defer func() { _ = s.Close() }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(path + ".start"); err == nil { //nolint:gosec // path is the parent test's t.TempDir() file
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper %s: start barrier never opened", role)
		}
		time.Sleep(time.Millisecond)
	}
	ctx := context.Background()
	for i := range helperRounds {
		fmt.Printf("%s %s %d shared-artifact %s\n", resultPrefix, role, i, outcome(s.PutArtifact(ctx, sharedArtifact(i))))
		fmt.Printf("%s %s %d own-artifact %s\n", resultPrefix, role, i, outcome(s.PutArtifact(ctx, ownArtifact(role, i))))
		fmt.Printf("%s %s %d terminal %s\n", resultPrefix, role, i, outcome(s.RecordNodeTerminal(ctx, sharedTerminal(role, i))))
	}
}

// outcome classifies a store result: ok, a domain conflict, or any other error
// (which the parent treats as a failure, including SQLITE_BUSY).
func outcome(err error) string {
	switch {
	case err == nil:
		return "ok"
	case strings.Contains(err.Error(), "terminal state conflict"):
		return "conflict"
	default:
		return "error:" + strings.ReplaceAll(err.Error(), " ", "_")
	}
}

type helperResult struct {
	role, op, outcome string
	round             int
}

func parseHelperOutput(t *testing.T, out []byte) []helperResult {
	t.Helper()
	var results []helperResult
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 5 || fields[0] != resultPrefix {
			continue
		}
		round, err := strconv.Atoi(fields[2])
		if err != nil {
			t.Fatalf("bad helper line %q", sc.Text())
		}
		results = append(results, helperResult{role: fields[1], round: round, op: fields[3], outcome: fields[4]})
	}
	return results
}

func TestSQLite_TwoProcessContention(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns helper processes")
	}
	path := filepath.Join(t.TempDir(), "contended.db")
	// Create and migrate the schema once, so the helpers contend on data, not DDL.
	s, err := inventory.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	_ = s.Close()

	cmds := map[string]*exec.Cmd{}
	outs := map[string]*bytes.Buffer{}
	for _, role := range []string{"a", "b"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSQLiteHelperProcess$", "-test.count=1", "-test.v") //nolint:gosec // re-executes this test binary
		cmd.Env = append(os.Environ(), helperDBEnv+"="+path, helperRoleEnv+"="+role)
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, &buf
		if err := cmd.Start(); err != nil {
			t.Fatalf("start helper %s: %v", role, err)
		}
		cmds[role], outs[role] = cmd, &buf
	}
	if err := os.WriteFile(path+".start", nil, 0o600); err != nil {
		t.Fatalf("open start barrier: %v", err)
	}
	var results []helperResult
	for role, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper %s failed: %v\n%s", role, err, outs[role].String())
		}
		results = append(results, parseHelperOutput(t, outs[role].Bytes())...)
	}
	if len(results) != 2*3*helperRounds {
		t.Fatalf("helper results = %d, want %d", len(results), 2*3*helperRounds)
	}
	verifyContention(t, path, results)
}

func verifyContention(t *testing.T, path string, results []helperResult) {
	t.Helper()
	s, err := inventory.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s.Close() }()
	verifyContentionOn(t, s, results)
}

// verifyContentionOn checks the two-process outcome against a reopened store of any backend.
func verifyContentionOn(t *testing.T, s inventory.Store, results []helperResult) {
	t.Helper()
	terminalOutcome := map[string]string{} // "role/round" -> outcome
	for _, r := range results {
		switch {
		case r.op == "terminal":
			terminalOutcome[r.role+"/"+strconv.Itoa(r.round)] = r.outcome
		case r.outcome != "ok":
			t.Errorf("helper %s round %d %s: %s", r.role, r.round, r.op, r.outcome)
		}
	}
	ctx := context.Background()
	for i := range helperRounds {
		got, ok, err := s.GetArtifact(ctx, sharedRun, "producer", "attempt-"+strconv.Itoa(i), "dataset")
		if err != nil || !ok || got.Digest != sharedArtifact(i).Digest {
			t.Errorf("shared artifact %d: ok=%v err=%v digest=%q", i, ok, err, got.Digest)
		}
		rec, ok, err := s.GetNodeTerminal(ctx, sharedRun, "producer", "attempt-"+strconv.Itoa(i))
		if err != nil || !ok {
			t.Fatalf("shared terminal %d: ok=%v err=%v", i, ok, err)
		}
		// Exactly one process won: the one whose state was stored saw ok, the other a
		// terminal state conflict. Neither may see any other error.
		for _, role := range []string{"a", "b"} {
			want := "conflict"
			if stateFor(role) == rec.TerminalState {
				want = "ok"
			}
			if got := terminalOutcome[role+"/"+strconv.Itoa(i)]; got != want {
				t.Errorf("terminal %d stored %s: helper %s saw %q, want %q", i, rec.TerminalState, role, got, want)
			}
		}
	}
	for _, role := range []string{"a", "b"} {
		list, err := s.ListArtifactsByRun(ctx, "run-"+role)
		if err != nil || len(list) != helperRounds {
			t.Errorf("helper %s own artifacts = %d err=%v, want %d", role, len(list), err, helperRounds)
		}
	}
	terms, err := s.ListNodeTerminalsByRun(ctx, sharedRun)
	if err != nil || len(terms) != helperRounds {
		t.Errorf("shared terminals = %d err=%v, want %d (one per key)", len(terms), err, helperRounds)
	}
}
