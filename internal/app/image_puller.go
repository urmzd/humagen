package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urmzd/humagen/pkg/images"
)

// dockerPuller pulls images through the Docker daemon the sandbox starts
// instances on, with the docker CLI, as the sandbox does.
type dockerPuller struct {
	host, tlsDir string
}

// Pull implements platform.ImagePuller. A credential ("user:token") is
// written to a Docker config of this pull's own and removed after, so no
// other pull, and no run, can use it.
func (d dockerPuller) Pull(ctx context.Context, ref, credential string) (string, error) {
	cfgDir, err := os.MkdirTemp("", "humagen-pull-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(cfgDir)
	auths := map[string]any{}
	if credential != "" {
		reg := images.Registry(ref)
		if reg == "docker.io" {
			reg = "https://index.docker.io/v1/"
		}
		auths[reg] = map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(credential))}
	}
	b, _ := json.Marshal(map[string]any{"auths": auths})
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), b, 0o600); err != nil {
		return "", err
	}
	if _, err := d.docker(ctx, cfgDir, "pull", "--quiet", ref); err != nil {
		return "", err
	}
	if strings.Contains(ref, "@sha256:") {
		return ref, nil
	}
	out, err := d.docker(ctx, cfgDir, "image", "inspect", "--format", "{{json .RepoDigests}}", ref)
	if err != nil {
		return "", err
	}
	var digests []string
	if err := json.Unmarshal(bytes.TrimSpace(out), &digests); err != nil {
		return "", fmt.Errorf("reading the digest: %w", err)
	}
	return pinned(ref, digests)
}

// pinned picks the digest of ref's repository from the ones Docker holds
// for it: repo@sha256:...
func pinned(ref string, digests []string) (string, error) {
	repo := ref
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	for _, d := range digests {
		name, _, ok := strings.Cut(d, "@")
		if ok && (name == repo || name == "docker.io/"+repo || name == "docker.io/library/"+repo) {
			return repo + d[len(name):], nil
		}
	}
	return "", errors.New("the registry gave no digest for it")
}

func (d dockerPuller) docker(ctx context.Context, cfgDir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = append(os.Environ(), "DOCKER_CONFIG="+cfgDir)
	if d.host != "" {
		cmd.Env = append(cmd.Env, "DOCKER_HOST="+d.host)
	}
	if d.tlsDir != "" {
		cmd.Env = append(cmd.Env, "DOCKER_TLS_VERIFY=1", "DOCKER_CERT_PATH="+d.tlsDir)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 300 {
			msg = msg[len(msg)-300:]
		}
		return nil, fmt.Errorf("docker %s: %s", args[0], msg)
	}
	return out, nil
}
