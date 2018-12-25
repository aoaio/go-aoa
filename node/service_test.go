package node

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestContextDatabases(t *testing.T) {

	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("failed to create temporary data directory: %v", err)
	}
	defer os.RemoveAll(dir)

	if _, err := os.Stat(filepath.Join(dir, "database")); err == nil {
		t.Fatalf("non-created database already exists")
	}

	ctx := &ServiceContext{config: &Config{Name: "unit-test", DataDir: dir}}
	db, err := ctx.OpenDatabase("persistent", 0, 0)
	if err != nil {
		t.Fatalf("failed to open persistent database: %v", err)
	}
	db.Close()

	if _, err := os.Stat(filepath.Join(dir, "unit-test", "persistent")); err != nil {
		t.Fatalf("persistent database doesn't exists: %v", err)
	}

	ctx = &ServiceContext{config: &Config{DataDir: ""}}
	db, err = ctx.OpenDatabase("ephemeral", 0, 0)
	if err != nil {
		t.Fatalf("failed to open ephemeral database: %v", err)
	}
	db.Close()

	if _, err := os.Stat(filepath.Join(dir, "ephemeral")); err == nil {
		t.Fatalf("ephemeral database exists")
	}
}

func TestContextServices(t *testing.T) {
	stack, err := New(testNodeConfig())
	if err != nil {
		t.Fatalf("failed to create protocol stack: %v", err)
	}

	verifier := func(ctx *ServiceContext) (Service, error) {
		var objA *NoopServiceA
		if ctx.Service(&objA) != nil {
			return nil, fmt.Errorf("former service not found")
		}
		var objB *NoopServiceB
		if err := ctx.Service(&objB); err != ErrServiceUnknown {
			return nil, fmt.Errorf("latters lookup error mismatch: have %v, want %v", err, ErrServiceUnknown)
		}
		return new(NoopService), nil
	}

	if err := stack.Register(NewNoopServiceA); err != nil {
		t.Fatalf("former failed to register service: %v", err)
	}
	if err := stack.Register(verifier); err != nil {
		t.Fatalf("failed to register service verifier: %v", err)
	}
	if err := stack.Register(NewNoopServiceB); err != nil {
		t.Fatalf("latter failed to register service: %v", err)
	}

	if err := stack.Start(); err != nil {
		t.Fatalf("failed to start stack: %v", err)
	}
	defer stack.Stop()
}
