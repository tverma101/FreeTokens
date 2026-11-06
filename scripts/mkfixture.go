//go:build ignore

// mkfixture writes a fake Freebuff Desktop project database so install smoke tests can check
// local usage tracking without a Freebuff account. It contains only usage counters.
//
//	go run scripts/mkfixture.go <home-dir>
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/mkfixture.go <home-dir>")
		os.Exit(2)
	}
	dir := filepath.Join(os.Args[1], ".config", "freebuff-desktop", "projects", "smoke")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		panic(err)
	}
	path := filepath.Join(dir, "desktop-v2.db")
	_ = os.Remove(path)
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		panic(err)
	}
	defer db.Close()
	now := time.Now().Add(-10 * time.Minute).UnixMilli()
	yesterday := time.Now().AddDate(0, 0, -1).UnixMilli()
	stmts := []string{
		`CREATE TABLE threads(id TEXT, model TEXT, harness_id TEXT, world_snapshot TEXT)`,
		`CREATE TABLE messages(thread_id TEXT, seq INTEGER, request_id TEXT, ts INTEGER, metrics_json TEXT, role TEXT)`,
		`INSERT INTO threads VALUES ('t1', 'deepseek/deepseek-v4-flash', 'codebuff', NULL)`,
		fmt.Sprintf(`INSERT INTO messages VALUES ('t1', 1, NULL, %d, '{"usage":{"inputTokens":120000,"outputTokens":30000,"cachedInputTokens":100000}}', 'assistant')`, now),
		fmt.Sprintf(`INSERT INTO messages VALUES ('t1', 2, NULL, %d, '{"usage":{"inputTokens":50000,"outputTokens":10000}}', 'assistant')`, yesterday),
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			panic(err)
		}
	}
	fmt.Println(path)
}
