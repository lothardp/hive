package state

import (
	"context"
	"testing"
)

func setupRepoRepo(t *testing.T) *RepoRepository {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRepoRepository(db)
}

func TestRepoCreateAndGetByName(t *testing.T) {
	rr := setupRepoRepo(t)
	ctx := context.Background()

	repo := &Repo{
		Name:          "myapp",
		Path:          "/home/user/projects/myapp",
		RemoteURL:     "git@github.com:user/myapp.git",
		DefaultBranch: "main",
		Config:        `{"expose_port":3000}`,
	}

	if err := rr.Create(ctx, repo); err != nil {
		t.Fatalf("creating repo: %v", err)
	}
	if repo.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	got, err := rr.GetByName(ctx, "myapp")
	if err != nil {
		t.Fatalf("getting repo: %v", err)
	}
	if got == nil {
		t.Fatal("expected repo, got nil")
	}
	if got.Path != "/home/user/projects/myapp" {
		t.Errorf("expected path /home/user/projects/myapp, got %s", got.Path)
	}
	if got.Config != `{"expose_port":3000}` {
		t.Errorf("unexpected config: %s", got.Config)
	}
}

func TestRepoGetByPath(t *testing.T) {
	rr := setupRepoRepo(t)
	ctx := context.Background()

	repo := &Repo{Name: "app", Path: "/projects/app", DefaultBranch: "main", Config: "{}"}
	if err := rr.Create(ctx, repo); err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	got, err := rr.GetByPath(ctx, "/projects/app")
	if err != nil {
		t.Fatalf("getting by path: %v", err)
	}
	if got == nil {
		t.Fatal("expected repo, got nil")
	}
	if got.Name != "app" {
		t.Errorf("expected name app, got %s", got.Name)
	}
}

func TestRepoGetByNameNotFound(t *testing.T) {
	rr := setupRepoRepo(t)
	ctx := context.Background()

	got, err := rr.GetByName(ctx, "ghost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestRepoCreateDuplicate(t *testing.T) {
	rr := setupRepoRepo(t)
	ctx := context.Background()

	repo := &Repo{Name: "dup", Path: "/a", DefaultBranch: "main", Config: "{}"}
	if err := rr.Create(ctx, repo); err != nil {
		t.Fatalf("first create: %v", err)
	}

	repo2 := &Repo{Name: "dup", Path: "/b", DefaultBranch: "main", Config: "{}"}
	if err := rr.Create(ctx, repo2); err == nil {
		t.Fatal("expected error on duplicate name")
	}
}

func TestRepoList(t *testing.T) {
	rr := setupRepoRepo(t)
	ctx := context.Background()

	for _, name := range []string{"alpha", "beta", "gamma"} {
		repo := &Repo{Name: name, Path: "/projects/" + name, DefaultBranch: "main", Config: "{}"}
		if err := rr.Create(ctx, repo); err != nil {
			t.Fatalf("creating repo %s: %v", name, err)
		}
	}

	repos, err := rr.List(ctx)
	if err != nil {
		t.Fatalf("listing repos: %v", err)
	}
	if len(repos) != 3 {
		t.Fatalf("expected 3 repos, got %d", len(repos))
	}
	// Ordered by name ASC
	if repos[0].Name != "alpha" {
		t.Errorf("expected first repo alpha, got %s", repos[0].Name)
	}
}

func TestRepoUpdate(t *testing.T) {
	rr := setupRepoRepo(t)
	ctx := context.Background()

	repo := &Repo{Name: "app", Path: "/projects/app", DefaultBranch: "main", Config: "{}"}
	if err := rr.Create(ctx, repo); err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	repo.Config = `{"expose_port":8080}`
	repo.DefaultBranch = "develop"
	if err := rr.Update(ctx, repo); err != nil {
		t.Fatalf("updating repo: %v", err)
	}

	got, _ := rr.GetByName(ctx, "app")
	if got.Config != `{"expose_port":8080}` {
		t.Errorf("expected updated config, got %s", got.Config)
	}
	if got.DefaultBranch != "develop" {
		t.Errorf("expected develop, got %s", got.DefaultBranch)
	}
}

func TestRepoDelete(t *testing.T) {
	rr := setupRepoRepo(t)
	ctx := context.Background()

	repo := &Repo{Name: "del", Path: "/projects/del", DefaultBranch: "main", Config: "{}"}
	if err := rr.Create(ctx, repo); err != nil {
		t.Fatalf("creating repo: %v", err)
	}

	if err := rr.Delete(ctx, "del"); err != nil {
		t.Fatalf("deleting repo: %v", err)
	}

	got, _ := rr.GetByName(ctx, "del")
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestRepoDeleteNotFound(t *testing.T) {
	rr := setupRepoRepo(t)
	ctx := context.Background()

	if err := rr.Delete(ctx, "ghost"); err == nil {
		t.Fatal("expected error deleting nonexistent repo")
	}
}
