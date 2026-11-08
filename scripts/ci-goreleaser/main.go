// Command ci-goreleaser generates a CI-only snapshot config from the actual release
// config. A tagged release always uses .goreleaser.yaml unchanged.
package main

import (
	"errors"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

// Exactly the install-smoke archives built by .github/workflows/ci.yml.
// Windows needs both GUI and console executables in the same zip.
var smokeTargets = map[string][]string{
	"darwin":     {"darwin_amd64_v1", "darwin_arm64_v8.0"},
	"portable":   {"linux_amd64_v1", "linux_arm64_v8.0"},
	"windows":    {"windows_amd64_v1", "windows_arm64_v8.0"},
	"windows-gui":{"windows_amd64_v1", "windows_arm64_v8.0"},
}

func nodeValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func restrictTargets(data []byte) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode release config: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, errors.New("release config has no root document")
	}
	builds := nodeValue(doc.Content[0], "builds")
	if builds == nil || builds.Kind != yaml.SequenceNode {
		return nil, errors.New("release config is missing builds list")
	}
	seen := map[string]bool{}
	for _, build := range builds.Content {
		id := nodeValue(build, "id")
		if id == nil || id.Kind != yaml.ScalarNode {
			return nil, errors.New("build without an ID")
		}
		targets, ok := smokeTargets[id.Value]
		if !ok || seen[id.Value] {
			return nil, fmt.Errorf("unknown or duplicated build ID %q; update CI targets explicitly", id.Value)
		}
		if nodeValue(build, "targets") != nil {
			return nil, fmt.Errorf("build %q already has targets; adjust CI generator deliberately", id.Value)
		}
		seen[id.Value] = true
		values := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, target := range targets {
			values.Content = append(values.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: target})
		}
		build.Content = append(build.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "targets"},
			values)
	}
	if len(seen) != len(smokeTargets) {
		return nil, fmt.Errorf("expected %d GoReleaser builds; found %d", len(smokeTargets), len(seen))
	}
	return yaml.Marshal(&doc)
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: ci-goreleaser <release-config> <output-config>")
		os.Exit(2)
	}
	source, err := os.ReadFile(os.Args[1])
	if err == nil {
		var out []byte
		out, err = restrictTargets(source)
		if err == nil {
			err = os.WriteFile(os.Args[2], out, 0o600)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
