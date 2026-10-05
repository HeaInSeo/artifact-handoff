package inventory_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/HeaInSeo/artifact-handoff/pkg/inventory"
)

// Two real OS processes contend on one PostgreSQL primary (TX1-07 (1)). Each process has its
// own connection pool; goroutines in one process would not count.

const (
	pgHelperDSNEnv     = "AH_INVENTORY_HELPER_PGDSN"
	pgHelperBarrierEnv = "AH_INVENTORY_HELPER_BARRIER"
)

// TestPostgresHelperProcess is the child side of TestPostgres_TwoProcessContention.
func TestPostgresHelperProcess(t *testing.T) {
	dsn, barrier, role := os.Getenv(pgHelperDSNEnv), os.Getenv(pgHelperBarrierEnv), os.Getenv(helperRoleEnv)
	if dsn == "" {
		t.Skip("helper process for TestPostgres_TwoProcessContention")
	}
	ctx := context.Background()
	s, err := inventory.NewPostgresStore(ctx, dsn)
	if err != nil {
		t.Fatalf("helper %s open: %v", role, err)
	}
	defer func() { _ = s.Close() }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(barrier); err == nil { //nolint:gosec // barrier is the parent test's t.TempDir() file
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper %s: start barrier never opened", role)
		}
		time.Sleep(time.Millisecond)
	}
	for i := range helperRounds {
		printResult(role, i, "shared-artifact", outcome(s.PutArtifact(ctx, sharedArtifact(i))))
		printResult(role, i, "own-artifact", outcome(s.PutArtifact(ctx, ownArtifact(role, i))))
		printResult(role, i, "terminal", outcome(s.RecordNodeTerminal(ctx, sharedTerminal(role, i))))
	}
}

func printResult(role string, round int, op, result string) {
	_, _ = os.Stdout.WriteString(resultPrefix + " " + role + " " + strconv.Itoa(round) + " " + op + " " + result + "\n")
}

func TestPostgres_TwoProcessContention(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns helper processes")
	}
	dsn := pgSchemaDSN(t)
	// Create the schema once, so the helpers contend on data, not DDL.
	_ = openPG(t, dsn).Close()
	barrier := filepath.Join(t.TempDir(), "start")

	cmds := map[string]*exec.Cmd{}
	outs := map[string]*bytes.Buffer{}
	for _, role := range []string{"a", "b"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestPostgresHelperProcess$", "-test.count=1", "-test.v") //nolint:gosec // re-executes this test binary
		cmd.Env = append(os.Environ(), pgHelperDSNEnv+"="+dsn, pgHelperBarrierEnv+"="+barrier, helperRoleEnv+"="+role)
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, &buf
		if err := cmd.Start(); err != nil {
			t.Fatalf("start helper %s: %v", role, err)
		}
		cmds[role], outs[role] = cmd, &buf
	}
	if err := os.WriteFile(barrier, nil, 0o600); err != nil {
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
	verifyContentionOn(t, openPG(t, dsn), results)
}
