package client

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/xiangkunzhang/Velan/internal/gateway/server"
)

func TestMain(m *testing.M) {
	if os.Getenv("VELAN_TEST_GATEWAY_HELPER") == "1" {
		_ = server.Run(context.Background(), server.Options{Port: 0, WatchStdin: true})
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSupervisorStartRecoverAndStop(t *testing.T) {
	t.Setenv("VELAN_TEST_GATEWAY_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	restarted := make(chan Status, 4)
	sup := NewSupervisor(exe, []string{"-test.run=^$"}, t.Logf, func(st Status) {
		if st.Running && st.Restarts > 0 {
			select {
			case restarted <- st:
			default:
			}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sup.Stop()

	st1 := sup.Status()
	if !st1.Running || st1.PID == 0 || st1.Addr == "" {
		t.Fatalf("unexpected initial status: %+v", st1)
	}
	if err := sup.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}

	// Simulate child crash and verify auto-recovery.
	sup.KillChild()
	select {
	case st2 := <-restarted:
		if !st2.Running || st2.PID == st1.PID || st2.Restarts != 1 {
			t.Fatalf("unexpected recovered status: %+v", st2)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for gateway auto-restart")
	}

	if err := sup.Health(ctx); err != nil {
		t.Fatalf("Health after recovery: %v", err)
	}
}
