package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/tortoise10h/buddy/internal/backend"
)

func TestStubT3Engine(t *testing.T) {
	eng := NewStubT3Engine()
	if eng.Name() != "t3" {
		t.Fatalf("expected Name() = 't3', got %s", eng.Name())
	}

	ctx := context.Background()

	// Dispatch returns ErrNotImplemented
	_, err := eng.Dispatch(ctx, DispatchRequest{
		TaskSlug: "feat-auth",
		Prompt:   "Add oauth login",
	})
	if err == nil {
		t.Fatal("expected Dispatch to fail, got nil")
	}
	var notImpl *ErrNotImplemented
	if !errors.As(err, &notImpl) {
		t.Fatalf("expected errors.As ErrNotImplemented, got %T: %v", err, err)
	}
	if notImpl.Engine != "t3" || notImpl.Operation != "dispatch" {
		t.Errorf("unexpected ErrNotImplemented contents: %+v", notImpl)
	}
	if !errors.Is(err, &ErrNotImplemented{}) {
		t.Errorf("expected errors.Is to match ErrNotImplemented")
	}

	// Capture returns ErrNotImplemented
	_, err = eng.Capture(ctx, "sess", "block", 10)
	if !errors.As(err, &notImpl) || notImpl.Operation != "capture" {
		t.Errorf("expected ErrNotImplemented for capture, got %v", err)
	}

	// Send returns ErrNotImplemented
	err = eng.Send(ctx, "sess", "block", backend.Input{Text: "hello"})
	if !errors.As(err, &notImpl) || notImpl.Operation != "send" {
		t.Errorf("expected ErrNotImplemented for send, got %v", err)
	}

	// Close returns ErrNotImplemented
	err = eng.Close(ctx, "sess", "win")
	if !errors.As(err, &notImpl) || notImpl.Operation != "close" {
		t.Errorf("expected ErrNotImplemented for close, got %v", err)
	}

	// OpenDevSplit returns ErrNotImplemented
	_, err = eng.OpenDevSplit(ctx, "sess", "block", "/tmp", "npm run dev")
	if !errors.As(err, &notImpl) || notImpl.Operation != "open_dev_split" {
		t.Errorf("expected ErrNotImplemented for open_dev_split, got %v", err)
	}

	// Focus is a clean no-op returning nil
	if err := eng.Focus(ctx, "sess", "win"); err != nil {
		t.Errorf("expected Focus() to return nil, got %v", err)
	}
}
