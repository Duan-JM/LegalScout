package workspace

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testWorkspace(t *testing.T) Workspace {
	t.Helper()
	root := filepath.Join(".", ".test-workspace-"+time.Now().Format("20060102150405.000000000"))
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	workspace, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}

	return workspace
}

func TestConcurrentCreateAtomicallyClaimsDistinctDirectories(t *testing.T) {
	root := testWorkspace(t)
	projects := make(chan Project, 2)
	failures := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			project, err := root.Create("并发项目")
			if err != nil {
				failures <- err
				return
			}
			projects <- project
		}()
	}
	group.Wait()
	close(projects)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	created := make([]Project, 0, 2)
	for project := range projects {
		created = append(created, project)
	}
	if len(created) != 2 || created[0].Path == created[1].Path {
		t.Fatalf("concurrent projects collided: %#v", created)
	}
	for _, project := range created {
		if _, err := parseMeta(project.Path); err != nil {
			t.Fatalf("project metadata was overwritten: %v", err)
		}
	}
}

func TestCreateRejectsMetadataControlCharacters(t *testing.T) {
	root := testWorkspace(t)
	if _, err := root.Create("合法名称\nid=伪造"); err == nil {
		t.Fatal("project name with metadata injection was accepted")
	}
}

func TestProjectDirectoriesAreIsolatedAndResolvable(t *testing.T) {
	root := testWorkspace(t)
	first, err := root.Create("恒星科技 IPO")
	if err != nil {
		t.Fatal(err)
	}
	second, err := root.Create("恒星科技 IPO")
	if err != nil {
		t.Fatal(err)
	}
	if first.Slug == second.Slug || first.Path == second.Path {
		t.Fatalf("projects are not isolated: %#v %#v", first, second)
	}
	for _, path := range []string{first.ScreenshotsPath(), first.DiagnosticsPath(), first.DBPath()} {
		if _, err := os.Stat(filepath.Dir(path)); err != nil {
			t.Fatalf("missing isolated directory %s: %v", path, err)
		}
	}
	resolved, err := root.Resolve(first.ID[:4])
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != first.Path {
		t.Fatalf("resolved %s, want %s", resolved.Path, first.Path)
	}
}

func TestAmbiguousPrefixIsRejected(t *testing.T) {
	root := testWorkspace(t)
	first, err := root.Create("Alpha One")
	if err != nil {
		t.Fatal(err)
	}
	_, err = root.Create("Alpha Two")
	if err != nil {
		t.Fatal(err)
	}
	_, err = root.Resolve(first.Slug[:5])
	if err == nil {
		t.Fatal("ambiguous prefix unexpectedly resolved")
	}
}

func TestResolvePrefersExactSlugBeforePrefix(t *testing.T) {
	root := testWorkspace(t)
	alpha, err := root.Create("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = root.Create("alpha-two"); err != nil {
		t.Fatal(err)
	}
	resolved, err := root.Resolve("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != alpha.ID {
		t.Fatalf("exact alpha resolved %q, want %q", resolved.ID, alpha.ID)
	}
}
