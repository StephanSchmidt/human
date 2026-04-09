package devcontainer

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"

	"github.com/StephanSchmidt/human/errors"
)

// ImageBuilder handles building or pulling devcontainer images.
type ImageBuilder struct {
	Docker DockerClient
	Logger zerolog.Logger
}

// EnsureImage ensures a devcontainer image exists. If not cached (or rebuild
// requested), it builds/pulls the image per the devcontainer config.
// Returns the image ID.
func (b *ImageBuilder) EnsureImage(ctx context.Context, cfg *DevcontainerConfig, projectDir, configHash string, rebuild bool, out io.Writer) (string, string, error) {
	imageName := ImageName(projectDir, configHash)

	// Check cache unless rebuild forced. Look up by metadata first (the
	// stored imageID is stable across tag changes), then by our tag name.
	if !rebuild {
		if meta, found := FindMetaByProject(projectDir); found && meta.ConfigHash == configHash && meta.ImageID != "" {
			if resp, err := b.Docker.ImageInspect(ctx, meta.ImageID); err == nil {
				_, _ = fmt.Fprintf(out, "Using cached image %s\n", meta.ImageName)
				return resp.ID, meta.ImageName, nil
			}
		}
		if resp, err := b.Docker.ImageInspect(ctx, imageName); err == nil {
			_, _ = fmt.Fprintf(out, "Using cached image %s\n", imageName) // #nosec G705 -- CLI output
			return resp.ID, imageName, nil
		}
	}

	// Determine build mode.
	switch {
	case cfg.Build != nil && cfg.Build.Dockerfile != "":
		return b.buildFromDockerfile(ctx, cfg, projectDir, imageName, out)
	case cfg.DockerFile != "":
		// Legacy dockerFile field; context defaults to the .devcontainer directory.
		build := &BuildConfig{Dockerfile: cfg.DockerFile}
		return b.buildFromDockerfile(ctx, &DevcontainerConfig{Build: build}, projectDir, imageName, out)
	case cfg.Image != "":
		return b.pullImage(ctx, cfg.Image, imageName, out)
	default:
		return "", "", errors.WithDetails("devcontainer.json must specify image or build.dockerfile")
	}
}

// pullImage pulls a base image. The container is created using the original
// image ref directly (no re-tagging needed for image-only configs).
func (b *ImageBuilder) pullImage(ctx context.Context, ref, targetName string, out io.Writer) (string, string, error) {
	_, _ = fmt.Fprintf(out, "Pulling %s...\n", ref) // #nosec G705 -- CLI output
	reader, err := b.Docker.ImagePull(ctx, ref, ImagePullOptions{})
	if err != nil {
		return "", "", errors.WrapWithDetails(err, "pulling image", "ref", ref)
	}
	defer func() { _ = reader.Close() }()

	// Drain pull output, capturing any error messages.
	if pullErr := drainDockerOutput(reader); pullErr != nil {
		return "", "", errors.WrapWithDetails(pullErr, "image pull failed", "ref", ref)
	}

	resp, err := b.Docker.ImageInspect(ctx, ref)
	if err != nil {
		return "", "", errors.WrapWithDetails(err, "inspecting pulled image", "ref", ref)
	}

	_, _ = fmt.Fprintf(out, "Image ready: %s\n", ref) // #nosec G705 -- CLI output
	// Use the original ref as imageName so ContainerCreate can find it.
	return resp.ID, ref, nil
}

// buildFromDockerfile builds an image from a Dockerfile.
func (b *ImageBuilder) buildFromDockerfile(ctx context.Context, cfg *DevcontainerConfig, projectDir, imageName string, out io.Writer) (string, string, error) {
	dockerfile := cfg.Build.Dockerfile

	// Resolve build context directory.
	contextDir := projectDir
	if cfg.Build.Context != "" {
		contextDir = filepath.Join(filepath.Dir(filepath.Join(projectDir, ".devcontainer", dockerfile)), cfg.Build.Context)
	}

	_, _ = fmt.Fprintf(out, "Building image from %s (context: %s)...\n", dockerfile, contextDir) // #nosec G705 -- CLI output

	// Create tar archive of the build context.
	buildCtx, err := createBuildContext(contextDir, filepath.Join(projectDir, ".devcontainer", dockerfile))
	if err != nil {
		return "", "", errors.WrapWithDetails(err, "creating build context", "dir", contextDir)
	}

	// Convert build args.
	buildArgs := make(map[string]*string)
	for k, v := range cfg.Build.Args {
		v := v
		buildArgs[k] = &v
	}

	reader, err := b.Docker.ImageBuild(ctx, buildCtx, ImageBuildOptions{
		Dockerfile: filepath.Base(dockerfile),
		Tags:       []string{imageName},
		BuildArgs:  buildArgs,
		Target:     cfg.Build.Target,
		CacheFrom:  cfg.Build.CacheFrom,
		Remove:     true,
	})
	if err != nil {
		return "", "", errors.WrapWithDetails(err, "building image")
	}
	defer func() { _ = reader.Close() }()

	// Drain build output, capturing any error messages.
	if buildErr := drainDockerOutput(reader); buildErr != nil {
		return "", "", errors.WrapWithDetails(buildErr, "image build failed")
	}

	resp, err := b.Docker.ImageInspect(ctx, imageName)
	if err != nil {
		return "", "", errors.WrapWithDetails(err, "inspecting built image")
	}

	_, _ = fmt.Fprintf(out, "Image built: %s\n", imageName)
	return resp.ID, imageName, nil
}

// dockerMessage is a line from Docker build/pull JSON output stream.
type dockerMessage struct {
	Error string `json:"error"`
}

// drainDockerOutput reads Docker JSON stream output to completion, returning
// the first error message found. Docker build and pull APIs embed errors in
// the JSON stream rather than the HTTP status.
func drainDockerOutput(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var msg dockerMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Error != "" {
			// Drain the rest to avoid connection reset.
			_, _ = io.Copy(io.Discard, r)
			return errors.WithDetails(msg.Error)
		}
	}
	return nil
}

// createBuildContext creates a tar archive from a directory, suitable for
// Docker image build. If dockerfilePath is outside the context dir, it is
// added to the tar as "Dockerfile".
func createBuildContext(contextDir, dockerfilePath string) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	defer func() { _ = tw.Close() }()

	absContext, err := filepath.Abs(contextDir)
	if err != nil {
		return nil, err
	}

	if err := tarDirectory(tw, absContext); err != nil {
		return nil, err
	}

	if err := addExternalDockerfile(tw, dockerfilePath, absContext); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	return &buf, nil
}

// skippedDirs contains directory names excluded from build contexts.
var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, ".devcontainer": true,
}

// tarDirectory walks a directory and adds all files to the tar writer.
func tarDirectory(tw *tar.Writer, absContext string) error {
	return filepath.Walk(absContext, func(path string, info os.FileInfo, err error) error { // #nosec G703 -- absContext is from filepath.Abs
		if err != nil {
			return err
		}
		if info.IsDir() && skippedDirs[filepath.Base(path)] {
			return filepath.SkipDir
		}
		return addFileToTar(tw, path, absContext, info)
	})
}

// addFileToTar adds a single file or directory entry to the tar writer.
func addFileToTar(tw *tar.Writer, path, absContext string, info os.FileInfo) error {
	relPath, err := filepath.Rel(absContext, path)
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = relPath
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	if info.IsDir() {
		return nil
	}
	f, err := os.Open(path) // #nosec G304 G703 -- path from Walk
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(tw, f)
	return err
}

// addExternalDockerfile adds a Dockerfile to the tar if it lives outside the context.
func addExternalDockerfile(tw *tar.Writer, dockerfilePath, absContext string) error {
	absDockerfile, _ := filepath.Abs(dockerfilePath)
	if strings.HasPrefix(absDockerfile, absContext+string(filepath.Separator)) {
		return nil
	}
	data, err := os.ReadFile(absDockerfile) // #nosec G304 G703 -- validated path from project dir
	if err != nil {
		return err
	}
	header := &tar.Header{Name: "Dockerfile", Size: int64(len(data)), Mode: 0o644}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err = tw.Write(data)
	return err
}
