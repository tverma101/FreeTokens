package pricing

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPricingCacheKeepsLastGoodCopyWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openrouter-rates.json")
	if err := os.WriteFile(path, []byte("old-prices"), 0o600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("destination locked")
	attempts := 0
	err := writeAtomicWithRename(path, []byte("new-prices"), func(from, to string) error {
		attempts++
		if to != path || from == path {
			t.Fatalf("unexpected staging rename: %q -> %q", from, to)
		}
		return failure
	})
	if !errors.Is(err, failure) || attempts != 1 {
		t.Fatalf("rename = %v after %d attempts, want unchanged failure", err, attempts)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "old-prices" {
		t.Fatalf("previous pricing cache removed: got=%q err=%v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "openrouter-rates.json" {
		t.Fatalf("temporary pricing cache left behind: entries=%+v err=%v", entries, err)
	}
	if err := writeAtomic(path, []byte("new-prices")); err != nil {
		t.Fatalf("subsequent update: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != "new-prices" {
		t.Fatalf("retry did not save refreshed pricing: got=%q err=%v", got, err)
	}
}

// A failed rename is expected to return a recoverable error on some operating
// systems. Verify the staging paths are distinct without requiring concurrent
// replacement of the same destination to succeed on Windows.
func TestPricingCacheConcurrentWritersUsePrivateStagingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing_supplement.json")
	const count = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[string]bool{}
	failure := errors.New("deliberate staging test rejection")
	errorsByWriter := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			err := writeAtomicWithRename(path, []byte{byte('a' + n)}, func(from, to string) error {
				if to != path {
					return errors.New("unexpected target")
				}
				mu.Lock()
				if seen[from] {
					mu.Unlock()
					return errors.New("reused staging filename")
				}
				seen[from] = true
				mu.Unlock()
				return failure
			})
			if !errors.Is(err, failure) {
				errorsByWriter <- err
			}
		}(i)
	}
	wg.Wait()
	close(errorsByWriter)
	for err := range errorsByWriter {
		t.Errorf("staging path was not unique: %v", err)
	}
	if len(seen) != count {
		t.Errorf("got %d distinct staging paths, want %d", len(seen), count)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed staging writes leaked files: entries=%+v err=%v", entries, err)
	}
}
