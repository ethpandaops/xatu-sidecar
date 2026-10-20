package cache

import "testing"

const blockRoot = "0x3a5ef0b0dc0a3a7f6a8a5f3e7f5f1c5bdb0d3c7a5d9e5f0a1b2c3d4e5f60718"

func TestBlockSlotCacheResolve(t *testing.T) {
	c := NewBlockSlotCache()

	if got := c.Resolve("", 7); got != 7 {
		t.Errorf("empty root should pass the slot through, got %d", got)
	}

	if got := c.Resolve(blockRoot, 0); got != 0 {
		t.Errorf("unknown root = %d", got)
	}

	c.Resolve(blockRoot, 12)

	if got := c.Resolve(blockRoot, 0); got != 12 {
		t.Errorf("resolved slot = %d, want 12", got)
	}
}
