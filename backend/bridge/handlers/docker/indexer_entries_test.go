package docker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestFindComposeFileCandidatesChoosesOneCanonicalFilePerStackDir(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "compose.yaml"), "services:\n  web:\n    image: nginx\n")
	writeFile(t, filepath.Join(root, "app", "docker-compose.yml"), "services:\n  app:\n    image: alpine\n")
	writeFile(t, filepath.Join(root, "app", "docker-compose.yaml"), "services:\n  other:\n    image: alpine\n")
	writeFile(t, filepath.Join(root, "app", "compose.yml"), "services:\n  other:\n    image: alpine\n")
	writeFile(t, filepath.Join(root, "app", "data", "nested.yml"), "services:\n  db:\n    image: postgres\n")
	writeFile(t, filepath.Join(root, "app", "settings.yaml"), "theme: dark\n")
	writeFile(t, filepath.Join(root, "worker", "compose.yaml"), "services:\n  worker:\n    image: alpine\n")

	results, err := findComposeFileCandidates(context.Background(), root)
	if err != nil {
		t.Fatalf("findComposeFileCandidates: %v", err)
	}

	paths := make(map[string]bool, len(results))
	for _, result := range results {
		paths[result.Path] = true
	}

	expected := []string{
		filepath.Join(root, "compose.yaml"),
		filepath.Join(root, "app", "docker-compose.yml"),
		filepath.Join(root, "worker", "compose.yaml"),
	}
	if len(paths) != len(expected) {
		t.Fatalf("result count = %d, want %d: %v", len(paths), len(expected), paths)
	}
	for _, path := range expected {
		if !paths[path] {
			t.Fatalf("missing result %s in %v", path, paths)
		}
	}
	for _, path := range []string{
		filepath.Join(root, "app", "docker-compose.yaml"),
		filepath.Join(root, "app", "compose.yml"),
		filepath.Join(root, "app", "settings.yaml"),
	} {
		if paths[path] {
			t.Fatalf("unexpected non-canonical result %s in %v", path, paths)
		}
	}
	if paths[filepath.Join(root, "app", "data", "nested.yml")] {
		t.Fatalf("unexpected nested compose result: %v", paths)
	}
}

func TestAddOfflineComposeProjectSuffixesDuplicateNames(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "first", "homepage", "docker-compose.yml")
	secondPath := filepath.Join(root, "second", "homepage", "docker-compose.yml")
	thirdPath := filepath.Join(root, "third", "homepage", "docker-compose.yml")

	projects := map[string]*apischema.ComposeProject{}
	addOfflineComposeProject(projects, firstPath)
	addOfflineComposeProject(projects, secondPath)
	addOfflineComposeProject(projects, thirdPath)

	expected := map[string]string{
		"homepage":   filepath.Dir(firstPath),
		"homepage-2": filepath.Dir(secondPath),
		"homepage-3": filepath.Dir(thirdPath),
	}
	for name, workingDir := range expected {
		project, ok := projects[name]
		if !ok {
			t.Fatalf("missing project %s in %#v", name, projects)
		}
		if project.Name != name {
			t.Fatalf("project.Name = %q, want %q", project.Name, name)
		}
		if project.WorkingDir != workingDir {
			t.Fatalf("%s working dir = %q, want %q", name, project.WorkingDir, workingDir)
		}
	}
}

func TestFillMissingComposeProjectFileDoesNotAppend(t *testing.T) {
	project := &apischema.ComposeProject{
		Name:        "homepage",
		ConfigFiles: []string{"/stacks/homepage/docker-compose.yml"},
		WorkingDir:  "/stacks/homepage",
	}

	fillMissingComposeProjectFile(project, "/stacks/homepage/compose.yaml")

	if len(project.ConfigFiles) != 1 {
		t.Fatalf("config files = %v, want exactly one file", project.ConfigFiles)
	}
	if project.ConfigFiles[0] != "/stacks/homepage/docker-compose.yml" {
		t.Fatalf("config file = %q", project.ConfigFiles[0])
	}
}

func TestUniqueComposeProjectNameSkipsExistingSuffixes(t *testing.T) {
	projects := map[string]*apischema.ComposeProject{
		"homepage":   {Name: "homepage"},
		"homepage-2": {Name: "homepage-2"},
	}

	if got := uniqueComposeProjectName("homepage", projects); got != "homepage-3" {
		t.Fatalf("uniqueComposeProjectName = %q, want homepage-3", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
