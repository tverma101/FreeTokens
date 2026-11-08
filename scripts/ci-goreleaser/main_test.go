package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestCISnapshotRetainsReleaseConfigExceptTargetMatrix(t *testing.T) {
	source, err := os.ReadFile("../../.goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := restrictTargets(source)
	if err != nil {
		t.Fatal(err)
	}
	var original, narrowed yaml.Node
	if err := yaml.Unmarshal(source, &original); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(generated, &narrowed); err != nil {
		t.Fatal(err)
	}
	builds := nodeValue(narrowed.Content[0], "builds")
	for _, build := range builds.Content {
		id := nodeValue(build, "id").Value
		want := smokeTargets[id]
		targets := nodeValue(build, "targets")
		if targets == nil {
			t.Fatalf("missing CI targets for %s", id)
		}
		var got []string
		for _, item := range targets.Content {
			got = append(got, item.Value)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("build %s targets %v, want %v", id, got, want)
		}
		// Remove only our appended targets; all release options must be otherwise
		// identical so archive names and pre-build hooks can't silently diverge.
		build.Content = build.Content[:len(build.Content)-2]
	}
	var restoredOriginal, restoredGenerated any
	if err := original.Decode(&restoredOriginal); err != nil {
		t.Fatal(err)
	}
	if err := narrowed.Decode(&restoredGenerated); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restoredOriginal, restoredGenerated) {
		t.Fatal("CI snapshot changed release configuration outside build targets")
	}
}

func TestCISnapshotFailsClosedWhenReleaseMatrixChanges(t *testing.T) {
	source, err := os.ReadFile("../../.goreleaser.yaml")
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(source), "id: portable", "id: portable-renamed", 1)
	if _, err := restrictTargets([]byte(changed)); err == nil {
		t.Fatal("unknown release build ID was silently omitted")
	}
}
