package filebrowser

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

func Test_Filebrowser_ProdAndDev_NamesAndFiles(t *testing.T) {
	ctx := context.Background()

	// 0) Require Docker
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		t.Fatalf("Docker not available (client init failed): %v", err)
	}
	if _, err := cli.Ping(ctx); err != nil {
		t.Fatalf("Docker not available (ping failed): %v", err)
	}

	// Table: prod and dev
	cases := []struct {
		name       string
		dev        bool
		expectName string // container name searched in `docker ps` (with leading '/')
	}{
		{"production", false, "/filebrowser-linuxio"},
		{"development", true, "/filebrowser-linuxio-dev"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// 1) Start/replace container in desired mode
			secret := "TEST_SECRET_FOR_INTEGRATION_" + tc.name
			StartServices(secret, true /*debug*/, tc.dev)

			// 2) Find container ID (up to ~30s)
			cid := waitForContainerByName(t, ctx, cli, tc.expectName, 30*time.Second)
			t.Cleanup(func() {
				_ = cli.ContainerRemove(ctx, cid, container.RemoveOptions{Force: true})
			})

			// 3) Validate rendered config.yaml
			cfg := mustReadFromContainerTar(t, ctx, cli, cid, "/home/filebrowser/config.yaml")
			cfgStr := string(cfg)
			if !strings.Contains(cfgStr, secret) {
				t.Fatalf("config.yaml does not contain secret; got:\n%s", cfgStr)
			}
			// debug=true ⇒ API levels include debug
			if !strings.Contains(cfgStr, "info|warning|error|debug") {
				t.Fatalf("config.yaml missing debug API levels; got:\n%s", cfgStr)
			}

			// 4) Validate custom.css bytes
			css := mustReadFromContainerTar(t, ctx, cli, cid, "/home/filebrowser/custom.css")
			if !bytes.Equal(css, EmbeddedCSS) {
				t.Fatalf("custom.css mismatch (len got=%d want=%d)", len(css), len(EmbeddedCSS))
			}
		})
	}
}

func waitForContainerByName(t *testing.T, ctx context.Context, cli *client.Client, wantName string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		list, err := cli.ContainerList(ctx, container.ListOptions{All: true})
		if err != nil {
			t.Fatalf("list containers: %v", err)
		}
		for _, c := range list {
			for _, n := range c.Names {
				if n == wantName {
					return c.ID
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("container %s not found within %v", wantName, timeout)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func mustReadFromContainerTar(t *testing.T, ctx context.Context, cli *client.Client, cid, path string) []byte {
	t.Helper()
	rc, _, err := cli.CopyFromContainer(ctx, cid, path)
	if err != nil {
		t.Fatalf("CopyFromContainer(%s): %v", path, err)
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar.Next for %s: %v", path, err)
	}
	if hdr == nil {
		t.Fatalf("tar empty for %s", path)
	}
	b, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("read tar content for %s: %v", path, err)
	}
	return b
}
