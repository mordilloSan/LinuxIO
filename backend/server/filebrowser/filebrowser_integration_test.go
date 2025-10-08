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

// ONE test to rule them all.
func Test_Filebrowser_EndToEnd_ContainerAndEmbeddedFiles(t *testing.T) {
	ctx := context.Background()

	// 1) Docker availability (FAIL if not available)
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		t.Fatalf("Docker not available (client init failed): %v", err)
	}
	if _, err := cli.Ping(ctx); err != nil {
		t.Fatalf("Docker not available (ping failed): %v", err)
	}

	// 2) Start the service (this will create/replace the container)
	secret := "TEST_SECRET_FOR_INTEGRATION"
	StartServices(secret, true) // debug=true ⇒ API levels include "debug"

	// 3) Find the container ID (wait up to ~30s)
	const containerName = "/filebrowser-linuxio"
	var cid string
	deadline := time.Now().Add(30 * time.Second)
	for {
		list, err := cli.ContainerList(ctx, container.ListOptions{All: true})
		if err != nil {
			t.Fatalf("failed to list containers: %v", err)
		}
		for _, c := range list {
			for _, n := range c.Names {
				if n == containerName {
					cid = c.ID
					break
				}
			}
			if cid != "" {
				break
			}
		}
		if cid != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("container %s not found in time", containerName)
		}
		time.Sleep(300 * time.Millisecond)
	}

	// Ensure cleanup
	t.Cleanup(func() {
		_ = cli.ContainerRemove(ctx, cid, container.RemoveOptions{Force: true})
	})

	// 4) Read config.yaml from the container and assert it has our rendered values
	cfg := mustReadFromContainerTar(t, cid, "/home/filebrowser/config.yaml")
	cfgStr := string(cfg)
	if !strings.Contains(cfgStr, secret) {
		t.Fatalf("config.yaml does not contain secret; got:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, "info|warning|error|debug") {
		t.Fatalf("config.yaml does not contain debug API levels; got:\n%s", cfgStr)
	}

	// 5) Read custom.css and assert it matches the embedded CSS bytes
	css := mustReadFromContainerTar(t, cid, "/home/filebrowser/custom.css")
	if !bytes.Equal(css, EmbeddedCSS) {
		t.Fatalf("custom.css in container does not match embedded CSS (len got=%d want=%d)", len(css), len(EmbeddedCSS))
	}
}

// Helper: CopyFromContainer returns a tar stream; extract the first file’s content.
func mustReadFromContainerTar(t *testing.T, cid, path string) []byte {
	t.Helper()
	rc, _, err := dockerCli.CopyFromContainer(context.Background(), cid, path)
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
