package cache

import (
	"sync"
	"testing"
	"time"
)

func TestCacheBasicOperations(t *testing.T) {
	c := NewCache[string](1 * time.Second)

	// Test Set and Get
	c.Set("key1", "value1")
	if val, ok := c.Get("key1"); !ok || val != "value1" {
		t.Errorf("Expected to get 'value1', got '%s' (ok=%v)", val, ok)
	}

	// Test Get non-existent key
	if _, ok := c.Get("nonexistent"); ok {
		t.Error("Expected to not find nonexistent key")
	}

	// Test Delete
	c.Delete("key1")
	if _, ok := c.Get("key1"); ok {
		t.Error("Expected key to be deleted")
	}
}

func TestCacheExpiration(t *testing.T) {
	c := NewCache[int](100 * time.Millisecond) // 100ms expiration

	c.Set("expire-key", 42)

	// Should exist immediately
	if val, ok := c.Get("expire-key"); !ok || val != 42 {
		t.Errorf("Expected to get 42, got %d (ok=%v)", val, ok)
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	if _, ok := c.Get("expire-key"); ok {
		t.Error("Expected key to be expired")
	}
}

func TestCacheCustomExpiration(t *testing.T) {
	c := NewCache[string](1 * time.Hour) // Default 1 hour

	// Set with custom 50ms expiration
	c.SetWithExp("custom-key", "custom-value", 50*time.Millisecond)

	// Should exist immediately
	if val, ok := c.Get("custom-key"); !ok || val != "custom-value" {
		t.Errorf("Expected to get 'custom-value', got '%s' (ok=%v)", val, ok)
	}

	// Wait for custom expiration
	time.Sleep(100 * time.Millisecond)

	// Should be expired
	if _, ok := c.Get("custom-key"); ok {
		t.Error("Expected key with custom expiration to be expired")
	}
}

func TestCacheCleanup(t *testing.T) {
	// Create cache with short cleanup interval
	c := NewCache[string](50*time.Millisecond, 100*time.Millisecond)

	// Add multiple items that will expire
	for i := 0; i < 10; i++ {
		c.Set("key", "value")
	}

	// Verify they exist
	if _, ok := c.Get("key"); !ok {
		t.Error("Expected key to exist before expiration")
	}

	// Wait for expiration and cleanup
	time.Sleep(200 * time.Millisecond)

	// Verify cleanup happened by checking the internal map is empty
	c.mu.RLock()
	mapSize := len(c.data)
	c.mu.RUnlock()

	if mapSize != 0 {
		t.Errorf("Expected cleanup to remove expired entries, but %d entries remain", mapSize)
	}
}

func TestCacheConcurrency(t *testing.T) {
	c := NewCache[int](1 * time.Second)
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			c.Set("concurrent-key", val)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Get("concurrent-key")
		}()
	}

	// Concurrent deletes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Delete("concurrent-key")
		}()
	}

	wg.Wait()

	// If we get here without deadlock or race condition, test passes
}

func TestCacheGenericTypes(t *testing.T) {
	// Test with struct type
	type TestStruct struct {
		Name  string
		Value int
	}

	c := NewCache[TestStruct](1 * time.Second)
	testData := TestStruct{Name: "test", Value: 42}

	c.Set("struct-key", testData)

	if val, ok := c.Get("struct-key"); !ok || val.Name != "test" || val.Value != 42 {
		t.Errorf("Expected to get TestStruct{test, 42}, got %+v (ok=%v)", val, ok)
	}

	// Test with bool type
	boolCache := NewCache[bool](1 * time.Second)
	boolCache.Set("bool-key", true)

	if val, ok := boolCache.Get("bool-key"); !ok || val != true {
		t.Errorf("Expected to get true, got %v (ok=%v)", val, ok)
	}

	// Test with slice type
	sliceCache := NewCache[[]string](1 * time.Second)
	sliceCache.Set("slice-key", []string{"a", "b", "c"})

	if val, ok := sliceCache.Get("slice-key"); !ok || len(val) != 3 {
		t.Errorf("Expected to get slice of length 3, got %+v (ok=%v)", val, ok)
	}
}

func TestCacheDefaultSettings(t *testing.T) {
	// Test with no settings (should use defaults)
	c := NewCache[string]()

	c.Set("default-key", "default-value")

	if val, ok := c.Get("default-key"); !ok || val != "default-value" {
		t.Errorf("Expected to get 'default-value', got '%s' (ok=%v)", val, ok)
	}

	// Verify default expiration is 24 hours (shouldn't expire quickly)
	time.Sleep(100 * time.Millisecond)
	if _, ok := c.Get("default-key"); !ok {
		t.Error("Expected key to still exist with default 24h expiration")
	}
}

func BenchmarkCacheSet(b *testing.B) {
	c := NewCache[string](1 * time.Hour)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c.Set("bench-key", "bench-value")
	}
}

func BenchmarkCacheGet(b *testing.B) {
	c := NewCache[string](1 * time.Hour)
	c.Set("bench-key", "bench-value")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c.Get("bench-key")
	}
}

func BenchmarkCacheConcurrent(b *testing.B) {
	c := NewCache[string](1 * time.Hour)
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				c.Set("key", "value")
			} else {
				c.Get("key")
			}
			i++
		}
	})
}
