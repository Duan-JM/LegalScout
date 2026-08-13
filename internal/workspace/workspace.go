// Package workspace owns project naming and filesystem isolation.
package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Duan-JM/LegalScout/internal/domain"
)

const EnvWorkspace = "LEGALSCOUT_WORKSPACE"

type Project struct {
	ID        string
	Slug      string
	Name      string
	Path      string
	CreatedAt time.Time
}

type Workspace struct{ Root string }

func Default() (Workspace, error) {
	if root := strings.TrimSpace(os.Getenv(EnvWorkspace)); root != "" {
		return Open(root)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Workspace{}, fmt.Errorf("locate home directory: %w", err)
	}
	return Open(filepath.Join(home, "LegalScout"))
}

func Open(root string) (Workspace, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Workspace{}, err
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return Workspace{}, fmt.Errorf("create workspace: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(absolute, "_legalscout"), 0o700); err != nil {
		return Workspace{}, fmt.Errorf("create workspace metadata: %w", err)
	}
	return Workspace{Root: absolute}, nil
}

func slugify(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			return r
		}
		return '-'
	}, name)
	name = regexp.MustCompile(`-+`).ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		return "project"
	}
	return name
}

func randomID() (string, error) {
	raw := make([]byte, 4)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func (w Workspace) Create(name string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, errors.New("项目名称不能为空")
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return Project{}, errors.New("项目名称不能包含换行或控制字符")
	}
	id, err := randomID()
	if err != nil {
		return Project{}, fmt.Errorf("generate project ID: %w", err)
	}
	base := slugify(name)
	slug := base
	for suffix := 2; ; suffix++ {
		path := filepath.Join(w.Root, slug)
		err := os.Mkdir(path, 0o700)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return Project{}, fmt.Errorf("claim project directory: %w", err)
		}
		slug = fmt.Sprintf("%s-%d", base, suffix)
	}
	path := filepath.Join(w.Root, slug)
	for _, dir := range []string{
		filepath.Join(path, "截图"),
		filepath.Join(path, "_legalscout", "logs"),
		filepath.Join(path, "_legalscout", "diagnostics"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			_ = os.RemoveAll(path)
			return Project{}, fmt.Errorf("create project directory: %w", err)
		}
	}
	project := Project{ID: id, Slug: slug, Name: name, Path: path, CreatedAt: time.Now()}
	if err := writeMeta(project); err != nil {
		_ = os.RemoveAll(path)
		return Project{}, err
	}
	return project, nil
}

func writeMeta(p Project) error {
	content := fmt.Sprintf("id=%s\nname=%s\nslug=%s\ncreated_at=%s\n", p.ID, p.Name, p.Slug, p.CreatedAt.Format(time.RFC3339))
	return os.WriteFile(filepath.Join(p.Path, "_legalscout", "project.meta"), []byte(content), 0o600)
}

func parseMeta(path string) (Project, error) {
	data, err := os.ReadFile(filepath.Join(path, "_legalscout", "project.meta"))
	if err != nil {
		return Project{}, err
	}
	p := Project{Path: path, Slug: filepath.Base(path)}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "id":
			p.ID = parts[1]
		case "name":
			p.Name = parts[1]
		case "slug":
			p.Slug = parts[1]
		case "created_at":
			p.CreatedAt, _ = time.Parse(time.RFC3339, parts[1])
		}
	}
	if p.ID == "" || p.Name == "" {
		return Project{}, fmt.Errorf("invalid project metadata in %s", path)
	}
	return p, nil
}

func (w Workspace) List() ([]Project, error) {
	entries, err := os.ReadDir(w.Root)
	if err != nil {
		return nil, err
	}
	projects := make([]Project, 0)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		p, err := parseMeta(filepath.Join(w.Root, entry.Name()))
		if err == nil {
			projects = append(projects, p)
		}
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].CreatedAt.Before(projects[j].CreatedAt) })
	return projects, nil
}

// Resolve accepts a full slug, short ID, exact name, or an unambiguous prefix.
func (w Workspace) Resolve(reference string) (Project, error) {
	reference = strings.TrimSpace(reference)
	projects, err := w.List()
	if err != nil {
		return Project{}, err
	}
	if reference == "" {
		cwd, err := os.Getwd()
		if err == nil {
			for _, p := range projects {
				if sameOrChild(cwd, p.Path) {
					return p, nil
				}
			}
		}
		return Project{}, errors.New("请指定项目名称、slug 或短 ID（仅在项目目录内可省略）")
	}
	var exact []Project
	for _, p := range projects {
		if reference == p.ID || reference == p.Slug || reference == p.Name {
			exact = append(exact, p)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return Project{}, fmt.Errorf("项目引用 %q 不唯一，请使用完整 slug 或短 ID", reference)
	}
	var matches []Project
	for _, p := range projects {
		if strings.HasPrefix(p.ID, reference) || strings.HasPrefix(p.Slug, reference) ||
			strings.HasPrefix(p.Name, reference) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return Project{}, fmt.Errorf("未找到项目 %q", reference)
	}
	return Project{}, fmt.Errorf("项目引用 %q 不唯一，请使用完整 slug 或短 ID", reference)
}

func sameOrChild(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func (p Project) DBPath() string          { return filepath.Join(p.Path, "_legalscout", "state.db") }
func (p Project) ScreenshotsPath() string { return filepath.Join(p.Path, "截图") }
func (p Project) DiagnosticsPath() string { return filepath.Join(p.Path, "_legalscout", "diagnostics") }
func (p Project) LogsPath() string        { return filepath.Join(p.Path, "_legalscout", "logs") }
func (p Project) ArchivingPath() string   { return filepath.Join(p.Path, "_legalscout", "archiving") }

func (p Project) IsArchiving() bool {
	_, err := os.Stat(p.ArchivingPath())
	return err == nil
}

func (p Project) StatusPath() string { return p.DBPath() }

func (p Project) ArchivePath() string { return filepath.Join(p.Path, "_legalscout", "archived") }

func (p Project) InitialStatus() domain.ProjectStatus { return domain.ProjectDraft }
