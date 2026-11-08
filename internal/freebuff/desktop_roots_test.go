package freebuff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"database/sql"
)

const usageSchema = `CREATE TABLE threads(id TEXT, model TEXT, harness_id TEXT, world_snapshot TEXT);
CREATE TABLE messages(thread_id TEXT, seq INTEGER, request_id TEXT, ts INTEGER, metrics_json TEXT, role TEXT);`

func writeDBAt(t *testing.T, path, schema string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
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
}

// A Linux install keeps everything in ~/.config/Freebuff, with the project databases below it and
// Electron's own state files beside them. Only the usage databases may be picked up.
func TestDesktopDatabasesFindsElectronLayout(t *testing.T) {
	root := t.TempDir()
	// Sorted, so "data" comes before "projects".
	want := []string{
		filepath.Join(root, "data", "cache", "usage.db"),
		filepath.Join(root, "projects", "abc", "desktop-v2.db"),
	}
	writeDBAt(t, want[0], usageSchema)
	writeDBAt(t, want[1], usageSchema)

	// Junk that must not be mistaken for history.
	writeDBAt(t, filepath.Join(root, "Cache", "cached.db"), usageSchema)
	writeDBAt(t, filepath.Join(root, "state.db"), "CREATE TABLE prefs(k TEXT);")
	writeDBAt(t, filepath.Join(root, "Local Storage", "leveldb", "history.db"), usageSchema)
	if err := os.WriteFile(filepath.Join(root, "Network Persistent State"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := DesktopDatabases([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A cap on the number of databases per root used to stop the walk at 32, so a machine with more
// projects silently lost everything past that point while the totals still looked complete.
func TestDesktopDatabasesHasNoResultCap(t *testing.T) {
	root := t.TempDir()
	var want []string
	for i := 0; i < 40; i++ {
		path := filepath.Join(root, "projects", fmt.Sprintf("p%02d", i), "desktop-v2.db")
		writeDBAt(t, path, usageSchema)
		want = append(want, path)
	}
	got, err := DesktopDatabases([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("found %d of %d databases", len(got), len(want))
	}
}

// Some installs keep history in the workspace (<project>/.freebuff/desktop-v2.db), outside every
// app directory. Nothing else can point at them, so the user opts in by naming the project.
func TestDesktopDatabasesSearchesOptInWorkspace(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".freebuff", "desktop-v2.db")
	writeDBAt(t, path, usageSchema)

	t.Setenv(HistoryDirsEnv, "")
	if roots := DesktopRoots(); slicesContain(roots, filepath.Join(project, ".freebuff")) {
		t.Fatalf("workspace searched without opting in: %v", roots)
	}
	if got, _ := DesktopDatabases(DesktopRoots()); slicesContain(got, path) {
		t.Fatalf("workspace database found without opting in: %v", got)
	}

	// Either the project root or the .freebuff directory itself is accepted.
	for _, entry := range []string{project, filepath.Join(project, ".freebuff")} {
		t.Setenv(HistoryDirsEnv, entry)
		if !slicesContain(DesktopRoots(), filepath.Join(project, ".freebuff")) {
			t.Fatalf("%s did not add the workspace root: %v", entry, DesktopRoots())
		}
		got, err := DesktopDatabases(DesktopRoots())
		if err != nil {
			t.Fatal(err)
		}
		if !slicesContain(got, path) {
			t.Fatalf("%s: database %s missing from %v", entry, path, got)
		}
	}
}

// A relative entry would follow whatever working directory a login item happens to have.
func TestWorkspaceRootsIgnoreRelativeEntries(t *testing.T) {
	t.Setenv(HistoryDirsEnv, "relative/project"+string(os.PathListSeparator)+t.TempDir())
	for _, root := range DesktopRoots() {
		if !filepath.IsAbs(root) {
			t.Fatalf("relative root %q in %v", root, DesktopRoots())
		}
	}
}

func slicesContain(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// The search has to survive the app directory existing but holding nothing usable.
func TestDesktopDatabasesToleratesEmptyAndMissingRoots(t *testing.T) {
	empty := t.TempDir()
	missing := filepath.Join(t.TempDir(), "Freebuff")
	got, err := DesktopDatabases([]string{empty, missing})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v err %v, want nothing", got, err)
	}
}

// End to end over the Linux layout: a database two levels below the app directory still counts.
func TestScannerReadsLinuxAppLayout(t *testing.T) {
	root := t.TempDir()
	ts := testNow.Add(-2 * time.Minute).UnixMilli()
	path := filepath.Join(root, "projects", "spyro", "desktop-v2.db")
	writeDBAt(t, path, fmt.Sprintf(`%s
INSERT INTO threads VALUES ('t1', 'deepseek/deepseek-v4-flash', 'codebuff', NULL);
INSERT INTO messages VALUES ('t1', 1, NULL, %d, '{"usage":{"inputTokens":100,"outputTokens":50}}', 'assistant')`, usageSchema, ts))

	s := &DesktopScanner{Roots: []string{root}}
	f, err := s.Scan(context.Background(), testNow, testPricing(t), testCatalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Days) != 1 || f.Days[0].Tokens != 150 {
		t.Fatalf("days = %+v, want one day with 150 tokens", f.Days)
	}
}

// Every platform's app directory is searched, so the same code finds Freebuff wherever Electron
// put it. XDG_DATA_HOME stands in for the Linux data directory on any host.
func TestDesktopRootsCoverPlatformDirs(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)

	roots := DesktopRoots()
	want := map[string]bool{
		filepath.Join(data, "Freebuff"):         false,
		filepath.Join(data, "freebuff-desktop"): false,
	}
	if config, err := os.UserConfigDir(); err == nil {
		want[filepath.Join(config, "Freebuff")] = false
	}
	for _, root := range roots {
		if _, ok := want[root]; ok {
			want[root] = true
		}
	}
	for root, found := range want {
		if !found {
			t.Fatalf("%s missing from %v", root, roots)
		}
	}
	seen := map[string]bool{}
	for _, root := range roots {
		if seen[root] {
			t.Fatalf("%s listed twice in %v", root, roots)
		}
		seen[root] = true
	}
}

// $XDG_CONFIG_HOME moves the whole config tree, so the app directory lands there and must be
// searched before the ~/.config fallback rather than after it.
func TestDesktopRootsPreferXDGConfigHome(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	roots := DesktopRoots()

	xdg := filepath.Join(config, "Freebuff")
	at := -1
	for i, root := range roots {
		if root == xdg {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("%s missing from %v", xdg, roots)
	}
	if home, err := os.UserHomeDir(); err == nil {
		fallback := filepath.Join(home, ".config", "Freebuff")
		for i, root := range roots {
			if root == fallback && i < at {
				t.Fatalf("fallback %s searched before $XDG_CONFIG_HOME in %v", fallback, roots)
			}
		}
	}
}

// Known Freebuff databases that cannot be queried are not mistaken for unrelated Electron state.
// Other databases must continue contributing measured history in the same scan.
func TestCorruptKnownDesktopDatabaseDoesNotHideValidProjects(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "projects", "broken", "desktop-v2.db")
	if err := os.MkdirAll(filepath.Dir(bad), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("not a SQLite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	ts := testNow.Add(-2 * time.Minute).UnixMilli()
	good := filepath.Join(root, "projects", "good", "desktop-v2.db")
	writeDBAt(t, good, fmt.Sprintf(`%s
INSERT INTO threads VALUES ('t1', 'deepseek/deepseek-v4-flash', 'codebuff', NULL);
INSERT INTO messages VALUES ('t1', 1, NULL, %d, '{"usage":{"inputTokens":100,"outputTokens":50}}', 'assistant')`, usageSchema, ts))
	paths, err := DesktopDatabases([]string{root})
	if err != nil || !slicesContain(paths, bad) || !slicesContain(paths, good) {
		t.Fatalf("discovery lost known database: %v, err: %v", paths, err)
	}
	s := &DesktopScanner{Roots: []string{root}}
	for pass := 0; pass < 2; pass++ {
		fold, err := s.Scan(context.Background(), testNow, testPricing(t), testCatalog(), nil)
		if err != nil {
			t.Fatalf("scan %d: %v", pass, err)
		}
		if len(fold.Days) != 1 || fold.Days[0].Tokens != 150 {
			t.Fatalf("scan %d: days = %+v", pass, fold.Days)
		}
		if got := s.UnreadableDatabases(); got != 1 {
			t.Fatalf("scan %d: unreadable databases = %d, want 1", pass, got)
		}
	}
}

func TestCorruptPreviouslyCachedDatabaseReleasesCacheBudget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "projects", "was-valid", "desktop-v2.db")
	ts := testNow.Add(-time.Minute).UnixMilli()
	writeDBAt(t, path, fmt.Sprintf(`%s
INSERT INTO threads VALUES ('t1', 'deepseek/deepseek-v4-flash', 'codebuff', NULL);
INSERT INTO messages VALUES ('t1', 1, NULL, %d, '{"usage":{"inputTokens":100,"outputTokens":50}}', 'assistant')`, usageSchema, ts))
	s := &DesktopScanner{Roots: []string{root}}
	if _, err := s.Scan(context.Background(), testNow, testPricing(t), testCatalog(), nil); err != nil {
		t.Fatal(err)
	}
	if s.retained == 0 {
		t.Fatal("fixture did not populate the cache")
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Scan(context.Background(), testNow, testPricing(t), testCatalog(), nil); err != nil {
		t.Fatal(err)
	}
	if s.UnreadableDatabases() != 1 || s.retained != 0 || len(s.dbs) != 0 {
		t.Fatalf("cache not released: unreadable=%d retained=%d cached=%d", s.UnreadableDatabases(), s.retained, len(s.dbs))
	}
}
