package copier

import (
	"sync"
	"testing"
)

func TestRowBufferPool_GetAndPut(t *testing.T) {
	// Test getting a buffer
	buf := getRowBuffer(10)

	if buf == nil {
		t.Fatal("Expected non-nil buffer")
	}
	if len(buf) != 10 {
		t.Errorf("Expected buffer length 10, got %d", len(buf))
	}
	if cap(buf) < 10 {
		t.Errorf("Expected buffer capacity >= 10, got %d", cap(buf))
	}

	// Verify all elements are nil (cleared)
	for i, v := range buf {
		if v != nil {
			t.Errorf("Expected buf[%d] to be nil, got %v", i, v)
		}
	}

	// Return buffer to pool
	putRowBuffer(buf)
}

func TestRowBufferPool_Resize(t *testing.T) {
	// Get a small buffer first
	smallBuf := getRowBuffer(5)
	if len(smallBuf) != 5 {
		t.Errorf("Expected length 5, got %d", len(smallBuf))
	}
	putRowBuffer(smallBuf)

	// Get a larger buffer - should resize or get a new one
	largeBuf := getRowBuffer(100)
	if len(largeBuf) != 100 {
		t.Errorf("Expected length 100, got %d", len(largeBuf))
	}
	putRowBuffer(largeBuf)

	// Get a smaller buffer again - should work with existing capacity
	smallBuf2 := getRowBuffer(10)
	if len(smallBuf2) != 10 {
		t.Errorf("Expected length 10, got %d", len(smallBuf2))
	}
	putRowBuffer(smallBuf2)
}

func TestRowBufferPool_ClearsValues(t *testing.T) {
	buf := getRowBuffer(5)

	// Simulate scanning values into the buffer
	buf[0] = "string value"
	buf[1] = 42
	buf[2] = 3.14
	buf[3] = true
	buf[4] = []byte("bytes")

	// Return to pool (should clear values)
	putRowBuffer(buf)

	// Get a new buffer from pool
	newBuf := getRowBuffer(5)

	// All values should be nil after getting from pool
	for i, v := range newBuf {
		if v != nil {
			t.Errorf("Expected newBuf[%d] to be nil after getting from pool, got %v", i, v)
		}
	}

	putRowBuffer(newBuf)
}

func TestRowBufferPool_Concurrent(t *testing.T) {
	const numGoroutines = 50
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				// Vary buffer sizes
				size := (j % 50) + 1
				buf := getRowBuffer(size)

				if len(buf) != size {
					t.Errorf("Goroutine %d: Expected length %d, got %d", id, size, len(buf))
				}

				// Simulate some work
				for k := 0; k < len(buf); k++ {
					buf[k] = k
				}

				putRowBuffer(buf)
			}
		}(i)
	}

	wg.Wait()
}

func TestRowBufferPool_ZeroSize(t *testing.T) {
	buf := getRowBuffer(0)

	if buf == nil {
		t.Error("Expected non-nil buffer even for size 0")
	}
	if len(buf) != 0 {
		t.Errorf("Expected length 0, got %d", len(buf))
	}

	putRowBuffer(buf)
}

func TestRowBufferPool_LargeSize(t *testing.T) {
	// Test with a large buffer that exceeds default capacity
	buf := getRowBuffer(1000)

	if len(buf) != 1000 {
		t.Errorf("Expected length 1000, got %d", len(buf))
	}

	// Verify all nil
	for i, v := range buf {
		if v != nil {
			t.Errorf("Expected buf[%d] to be nil, got %v", i, v)
		}
	}

	putRowBuffer(buf)
}

func BenchmarkRowBufferPool_GetPut(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf := getRowBuffer(50)
		putRowBuffer(buf)
	}
}

func BenchmarkRowBufferPool_GetPut_Parallel(b *testing.B) {
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := getRowBuffer(50)
			putRowBuffer(buf)
		}
	})
}

func BenchmarkRowBufferPool_NoPool(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf := make([]any, 50)
		_ = buf
	}
}
