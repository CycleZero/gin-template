package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryCacheCompareAndDeleteWrongThenRight(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := NewMemoryCache()
	if err := c.Set(ctx, "verification", []byte("right"), time.Minute); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	matched, err := c.CompareAndDelete(ctx, "verification", []byte("wrong"))
	if err != nil {
		t.Fatalf("wrong CompareAndDelete() error = %v", err)
	}
	if matched {
		t.Fatal("wrong value must not match")
	}

	matched, err = c.CompareAndDelete(ctx, "verification", []byte("right"))
	if err != nil {
		t.Fatalf("right CompareAndDelete() error = %v", err)
	}
	if !matched {
		t.Fatal("right value should still be available after a mismatch")
	}

	_, err = c.CompareAndDelete(ctx, "verification", []byte("right"))
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("consumed value error = %v, want ErrKeyNotFound", err)
	}
}

func TestMemoryCacheCompareAndDeleteExpired(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	c := NewMemoryCache()
	if err := c.Set(ctx, "expired", []byte("value"), time.Nanosecond); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	time.Sleep(time.Millisecond)

	_, err := c.CompareAndDelete(ctx, "expired", []byte("value"))
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expired value error = %v, want ErrKeyNotFound", err)
	}
}
