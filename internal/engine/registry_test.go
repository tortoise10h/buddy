package engine

import (
	"context"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
)

type dummyEngine struct {
	name string
}

func (d *dummyEngine) Name() string                                                             { return d.name }
func (d *dummyEngine) Dispatch(ctx context.Context, req DispatchRequest) (*DispatchResult, error) { return nil, nil }
func (d *dummyEngine) Capture(ctx context.Context, s, b string, l int) (string, error)          { return "", nil }
func (d *dummyEngine) Send(ctx context.Context, s, b string, in backend.Input) error           { return nil }
func (d *dummyEngine) Close(ctx context.Context, s, w string) error                             { return nil }
func (d *dummyEngine) Focus(ctx context.Context, s, w string) error                             { return nil }
func (d *dummyEngine) OpenDevSplit(ctx context.Context, s, b, c, cmd string) (string, error)    { return "", nil }

func TestRegistry_MultiEngine(t *testing.T) {
	reg := NewRegistry("rex")
	rexEng := &dummyEngine{name: "rex"}
	t3Eng := &dummyEngine{name: "t3"}

	reg.Register("rex", rexEng)
	reg.Register("t3", t3Eng)

	// 1. Default should resolve to rex
	eng, err := reg.Get("")
	if err != nil {
		t.Fatalf("failed to get default engine: %v", err)
	}
	if eng.Name() != "rex" {
		t.Errorf("expected rex, got %s", eng.Name())
	}

	// 2. Explicit t3
	eng, err = reg.Get("t3")
	if err != nil {
		t.Fatalf("failed to get t3 engine: %v", err)
	}
	if eng.Name() != "t3" {
		t.Errorf("expected t3, got %s", eng.Name())
	}

	// 3. Unknown engine
	_, err = reg.Get("unknown")
	if err == nil {
		t.Fatalf("expected error for unknown engine")
	}
}
