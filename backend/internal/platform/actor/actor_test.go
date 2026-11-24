package actor

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestFromContextWithoutValidActor(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"background", context.Background()},
		{"todo", context.TODO()},
		{"nil", nil},
		{"wrong value type", context.WithValue(context.Background(), contextKey{}, 42)},
		{"invalid value", context.WithValue(context.Background(), contextKey{}, "forged")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FromContext(tt.ctx); got != System {
				t.Errorf("FromContext() = %q, want %q", got, System)
			}
		})
	}
}

func TestWithContextValidActor(t *testing.T) {
	valid := []string{
		System,
		"emp_abcd",
		"emp_a123",
		"emp_abcdefghijkl",
		"emp_123456789012",
	}
	for _, value := range valid {
		t.Run(value, func(t *testing.T) {
			ctx := WithContext(context.Background(), value)
			if got := FromContext(ctx); got != value {
				t.Errorf("FromContext() = %q, want %q", got, value)
			}
		})
	}
}

func TestWithContextInvalidActorIsIgnored(t *testing.T) {
	invalid := []string{
		"",
		"SYSTEM",
		" system",
		"system ",
		"root",
		"emp_abc",
		"emp_abcdefghijklm",
		"emp_ABCD",
		"emp_ab_cd",
		"emp_abcd\n",
	}
	for _, value := range invalid {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			ctx := WithContext(context.Background(), value)
			if got := FromContext(ctx); got != System {
				t.Errorf("FromContext() = %q, want %q", got, System)
			}
		})
	}
}

func TestWithContextInvalidActorKeepsStoredActor(t *testing.T) {
	const want = "emp_abc1"
	ctx := WithContext(context.Background(), want)
	ctx = WithContext(ctx, "forged")
	if got := FromContext(ctx); got != want {
		t.Errorf("FromContext() = %q, want %q", got, want)
	}
}

func TestWithContextNilContext(t *testing.T) {
	ctx := WithContext(nil, "emp_abc1")
	if got := FromContext(ctx); got != "emp_abc1" {
		t.Errorf("FromContext() = %q, want %q", got, "emp_abc1")
	}
	if got := FromContext(WithContext(nil, "forged")); got != System {
		t.Errorf("FromContext() = %q, want %q", got, System)
	}
}

func TestConcurrentUse(t *testing.T) {
	const workers = 64
	const iterations = 100

	shared := WithContext(context.Background(), "emp_shared")
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("emp_%04x", i)
			ctx := WithContext(shared, want)
			for j := 0; j < iterations; j++ {
				if got := FromContext(ctx); got != want {
					t.Errorf("FromContext() = %q, want %q", got, want)
					return
				}
				if got := FromContext(shared); got != "emp_shared" {
					t.Errorf("shared FromContext() = %q, want %q", got, "emp_shared")
					return
				}
			}
		}(i)
	}
	wg.Wait()
}
