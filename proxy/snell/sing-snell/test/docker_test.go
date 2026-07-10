package test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sagernet/sing/common/debug"
	F "github.com/sagernet/sing/common/format"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

const baseImage = "debian:bookworm-slim"

type binarySpec struct {
	tag    string
	sha256 string
}

var binaries = map[string]binarySpec{
	"v4": {tag: "v4.1.1", sha256: "8e02974916645aafc0521c49faf0ab9916a0a8a7922ae7261cfd74b96899d25a"},
	"v5": {tag: "v5.0.1", sha256: "5b2e221f2c6e29b1db8e47053e1221be29d5627da807cb932b089f514a3609f0"},
	"v6": {tag: "v6.0.0b4", sha256: "ef2feaaf69d40673c2f3c8dccff28be6eadb75e790cb9e0cabd18bc66675c46f"},
}

func cacheDir() string {
	dir := os.Getenv("SNELL_CACHE_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "sing-snell-test-bin")
	}
	return dir
}

func snellBinary(t *testing.T, version string) string {
	spec, found := binaries[version]
	require.True(t, found, "unknown snell version: %s", version)

	if override := os.Getenv("SNELL_BIN_DIR"); override != "" {
		dir := filepath.Join(override, spec.tag+"-linux-amd64")
		verifyBinary(t, filepath.Join(dir, "snell-server"), spec.sha256)
		return dir
	}

	dir := filepath.Join(cacheDir(), spec.tag)
	binaryPath := filepath.Join(dir, "snell-server")
	if checkBinary(binaryPath, spec.sha256) {
		return dir
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))

	url := "https://dl.nssurge.com/snell/snell-server-" + spec.tag + "-linux-amd64.zip"
	t.Logf("downloading %s", url)
	archive := downloadFile(t, url)
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)
	var extracted bool
	for _, file := range reader.File {
		if filepath.Base(file.Name) != "snell-server" {
			continue
		}
		source, openErr := file.Open()
		require.NoError(t, openErr)
		content, readErr := io.ReadAll(source)
		source.Close()
		require.NoError(t, readErr)
		require.NoError(t, os.WriteFile(binaryPath, content, 0o755))
		extracted = true
		break
	}
	require.True(t, extracted, "snell-server not found in archive")
	verifyBinary(t, binaryPath, spec.sha256)
	return dir
}

func checkBinary(path string, want string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:]) == want
}

func verifyBinary(t *testing.T, path string, want string) {
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	digest := sha256.Sum256(content)
	require.Equal(t, want, hex.EncodeToString(digest[:]), "binary sha256 mismatch: %s", path)
	require.NoError(t, os.Chmod(path, 0o755))
}

func downloadFile(t *testing.T, url string) []byte {
	response, err := http.Get(url)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode, "download %s", url)
	content, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return content
}

func startSnellServer(t *testing.T, version string, configContent string, port uint16) {
	binDir := snellBinary(t, version)

	confDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(confDir, "snell.conf"), []byte(configContent), 0o644))

	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	t.Cleanup(func() { dockerClient.Close() })

	ctx := context.Background()
	ensureImage(t, dockerClient, baseImage)

	containerConfig := &container.Config{
		Image:      baseImage,
		Entrypoint: []string{"/snell/snell-server"},
		Cmd:        []string{"-l", logLevel(), "-c", "/conf/snell.conf"},
	}
	hostConfig := &container.HostConfig{
		NetworkMode: "host",
		Binds: []string{
			binDir + ":/snell:ro",
			confDir + ":/conf:ro",
		},
	}
	platform := &ocispec.Platform{OS: "linux", Architecture: "amd64"}

	created, err := dockerClient.ContainerCreate(ctx, containerConfig, hostConfig, nil, platform, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		dockerClient.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
	})
	require.NoError(t, dockerClient.ContainerStart(ctx, created.ID, container.StartOptions{}))

	if debug.Enabled {
		attach, attachErr := dockerClient.ContainerAttach(ctx, created.ID, container.AttachOptions{
			Stdout: true, Stderr: true, Logs: true, Stream: true,
		})
		require.NoError(t, attachErr)
		go stdcopy.StdCopy(os.Stderr, os.Stderr, attach.Reader)
	}

	waitForListen(t, dockerClient, created.ID, port)
}

func waitForListen(t *testing.T, dockerClient *client.Client, containerID string, port uint16) {
	deadline := time.Now().Add(30 * time.Second)
	address := "127.0.0.1:" + F.ToString(port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			conn.Close()
			return
		}
		info, inspectErr := dockerClient.ContainerInspect(context.Background(), containerID)
		if inspectErr == nil && !info.State.Running {
			logs := containerLogs(dockerClient, containerID)
			t.Fatalf("snell server exited early (code %d):\n%s", info.State.ExitCode, logs)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("snell server did not listen on %s:\n%s", address, containerLogs(dockerClient, containerID))
}

func containerLogs(dockerClient *client.Client, containerID string) string {
	reader, err := dockerClient.ContainerLogs(context.Background(), containerID, container.LogsOptions{
		ShowStdout: true, ShowStderr: true,
	})
	if err != nil {
		return "<no logs: " + err.Error() + ">"
	}
	defer reader.Close()
	var out bytes.Buffer
	stdcopy.StdCopy(&out, &out, reader)
	return out.String()
}

func ensureImage(t *testing.T, dockerClient *client.Client, ref string) {
	_, err := dockerClient.ImageInspect(context.Background(), ref)
	if err == nil {
		return
	}
	reader, pullErr := dockerClient.ImagePull(context.Background(), ref, image.PullOptions{Platform: "linux/amd64"})
	require.NoError(t, pullErr)
	defer reader.Close()
	io.Copy(io.Discard, reader)
}

func logLevel() string {
	if debug.Enabled {
		return "trace"
	}
	return "error"
}
