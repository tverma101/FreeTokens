package freebuff

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeDB(t *testing.T, root, project, schema string) string {
	t.Helper()
	dir := filepath.Join(root, project)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "desktop-v2.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range strings.Split(schema, ";\n") {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	return path
}

func TestRealSQLiteRestoresClearedModelFromSnapshotOnly(t *testing.T) {
	root := t.TempDir()
	ts := testNow.Add(-time.Minute).UnixMilli()
	usageJSON := `{"usage":{"inputTokens":100,"outputTokens":50,"cachedInputTokens":80}}`
	path := makeDB(t, root, "p1", fmt.Sprintf(`CREATE TABLE threads(id TEXT, model TEXT, harness_id TEXT, world_snapshot TEXT);
CREATE TABLE messages(thread_id TEXT, seq INTEGER, request_id TEXT, ts INTEGER, metrics_json TEXT, role TEXT);
INSERT INTO threads VALUES ('restored', NULL, 'codebuff', '{"model":"deepseek/deepseek-v4-flash","unrelated":"never projected"}');
INSERT INTO threads VALUES ('explicit', 'm-fast', 'codebuff', '{"model":"deepseek/deepseek-v4-flash"}');
INSERT INTO threads VALUES ('other', 'x', 'claude', '{}');
INSERT INTO messages VALUES ('restored', 1, NULL, %[1]d, '%[2]s', 'assistant');
INSERT INTO messages VALUES ('explicit', 1, NULL, %[1]d, '%[2]s', 'assistant');
INSERT INTO messages VALUES ('explicit', 2, NULL, %[1]d, '{"text":"user words"}', 'user');
INSERT INTO messages VALUES ('other', 1, NULL, %[1]d, '%[2]s', 'assistant')`, ts, usageJSON))

	rows, err := readDesktopRows(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if strings.Contains(r.metrics+r.selectedModel, "never projected") {
			t.Fatal("snapshot content beyond the model was projected")
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v (no receipts table: assistant-only fallback expected)", rows)
	}
	s := &DesktopScanner{Roots: []string{root}}
	f, err := s.Scan(context.Background(), testNow, testPricing(t), testCatalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	d := f.Days[0]
	if len(d.Models) != 1 || d.Models[0].Name != "DeepSeek V4.1 Flash" || d.Tokens != 300 {
		t.Fatalf("day = %+v", d)
	}
	if d.CostUSD == nil || !near(*d.CostUSD, 0.000640) || len(d.UnknownModels) != 0 {
		t.Fatalf("cost = %v unknown = %v", d.CostUSD, d.UnknownModels)
	}
	// Unchanged revision is served from cache.
	if _, err := s.Scan(context.Background(), testNow, testPricing(t), testCatalog(), nil); err != nil {
		t.Fatal(err)
	}
	if s.retained != 2 {
		t.Fatalf("retained = %d", s.retained)
	}
}

func TestOldSchemaWithoutSnapshotColumnAndManagerReceipts(t *testing.T) {
	root := t.TempDir()
	ts := testNow.Add(-time.Minute).UnixMilli()
	makeDB(t, root, "old", fmt.Sprintf(`CREATE TABLE threads(id TEXT, model TEXT, harness_id TEXT);
CREATE TABLE messages(thread_id TEXT, seq INTEGER, request_id TEXT, ts INTEGER, metrics_json TEXT, role TEXT);
INSERT INTO threads VALUES ('a', 'deepseek/deepseek-v4-flash', 'codebuff');
INSERT INTO messages VALUES ('a', 1, 'cloud:run1', %d, '{"usage":{"inputTokens":10,"outputTokens":5}}', 'assistant')`, ts))
	makeDB(t, root, "new", fmt.Sprintf(`CREATE TABLE threads(id TEXT, model TEXT, harness_id TEXT, world_snapshot TEXT);
CREATE TABLE messages(thread_id TEXT, seq INTEGER, request_id TEXT, ts INTEGER, metrics_json TEXT, role TEXT);
CREATE TABLE auto_run_decision_receipts(id INTEGER, thread_id TEXT, created_at INTEGER, manager_json TEXT);
INSERT INTO threads VALUES ('b', 'deepseek/deepseek-v4-flash', 'codebuff', NULL);
INSERT INTO auto_run_decision_receipts VALUES (1, 'b', %[1]d, '{"harnessId":"codebuff","model":"m-normal","startedAt":%[1]d,"usage":{"inputTokens":20,"outputTokens":10}}')`, ts))
	s := &DesktopScanner{Roots: []string{root}}
	// The hosted copy of cloud:run1 wins over the Desktop row.
	hosted := receipt("cloud:run1", "m-normal")
	f, err := s.Scan(context.Background(), testNow, testPricing(t), testCatalog(), []Receipt{hosted})
	if err != nil {
		t.Fatal(err)
	}
	if d := f.Days[0]; d.Tokens != 150+30 {
		t.Fatalf("tokens = %d, want 180 (hosted 150 + manager 30, Desktop duplicate skipped)", d.Tokens)
	}
}

func TestNoDatabasesAndNoReceiptsIsEmpty(t *testing.T) {
	s := &DesktopScanner{Roots: []string{filepath.Join(t.TempDir(), "missing")}}
	f, err := s.Scan(context.Background(), testNow, testPricing(t), nil, nil)
	if err != nil || len(f.Days) != 0 {
		t.Fatalf("f=%+v err=%v", f, err)
	}
}
