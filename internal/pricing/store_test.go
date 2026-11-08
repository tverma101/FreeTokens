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

func TestPricingCacheConcurrentWritersUsePrivateStagingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing_supplement.json")
	const count = 12
	var wg sync.WaitGroup
	errorsByWriter := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			data := []byte{byte('a' + n)}
			if err := writeAtomic(path, data); err != nil {
				errorsByWriter <- err
			}
		}(i)
	}
	wg.Wait()
	close(errorsByWriter)
	for err := range errorsByWriter {
		t.Errorf("concurrent cache write failed: %v", err)
	}
	if t.Failed() {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) != 1 || b[0] < 'a' || b[0] >= 'a'+count {
		t.Fatalf("cache output corrupted: got=%q err=%v", b, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "pricing_supplement.json" {
		t.Fatalf("staging files leaked: entries=%+v err=%v", entries, err)
	}
}
