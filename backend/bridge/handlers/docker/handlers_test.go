package docker

import (
	"context"
	"testing"
)

type testContextKey string

type testEvents struct {
	result any
}

func (e *testEvents) Data([]byte) error       { return nil }
func (e *testEvents) Progress(any) error      { return nil }
func (e *testEvents) Result(result any) error { e.result = result; return nil }
func (e *testEvents) Error(error, int) error  { return nil }
func (e *testEvents) Close(string) error      { return nil }

func TestDockerNoArgCallWithContextPassesContext(t *testing.T) {
	const want = "trace-id"
	ctx := context.WithValue(context.Background(), testContextKey("key"), want)
	emit := &testEvents{}

	handler := dockerNoArgCallWithContext(func(ctx context.Context) (string, error) {
		got, _ := ctx.Value(testContextKey("key")).(string)
		if got != want {
			t.Fatalf("context value = %q, want %q", got, want)
		}
		return "ok", nil
	})

	if err := handler(ctx, nil, emit); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if emit.result != "ok" {
		t.Fatalf("emit result = %v, want ok", emit.result)
	}
}

func TestDockerOneArgCallWithContextPassesContext(t *testing.T) {
	const want = "trace-id"
	ctx := context.WithValue(context.Background(), testContextKey("key"), want)
	emit := &testEvents{}

	handler := dockerOneArgCallWithContext(nil, func(ctx context.Context, arg string) (string, error) {
		got, _ := ctx.Value(testContextKey("key")).(string)
		if got != want {
			t.Fatalf("context value = %q, want %q", got, want)
		}
		if arg != "container-1" {
			t.Fatalf("arg = %q, want container-1", arg)
		}
		return "ok", nil
	})

	if err := handler(ctx, []string{"container-1"}, emit); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if emit.result != "ok" {
		t.Fatalf("emit result = %v, want ok", emit.result)
	}
}

func TestLoggedDockerNoArgCallWithContextPassesContext(t *testing.T) {
	const want = "trace-id"
	ctx := context.WithValue(context.Background(), testContextKey("key"), want)
	emit := &testEvents{}

	handler := loggedDockerNoArgCallWithContext("message", func(ctx context.Context) (string, error) {
		got, _ := ctx.Value(testContextKey("key")).(string)
		if got != want {
			t.Fatalf("context value = %q, want %q", got, want)
		}
		return "ok", nil
	})

	if err := handler(ctx, nil, emit); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if emit.result != "ok" {
		t.Fatalf("emit result = %v, want ok", emit.result)
	}
}
