package embedded

import (
	"archive/tar"
	"bytes"
	"io"
)

// DockerFile holds a file to include in the Docker build context.
type DockerFile struct {
	Name string
	Data []byte
}

// dockerFiles is populated at init time from cmd/klaudio via RegisterDockerFiles.
var dockerFiles []DockerFile

// RegisterDockerFiles stores the embedded Docker build context files.
// Called once at startup from cmd/klaudio where the go:embed directive lives.
func RegisterDockerFiles(files []DockerFile) {
	dockerFiles = files
}

// HasDockerFiles returns true if Docker files were registered.
func HasDockerFiles() bool {
	return len(dockerFiles) > 0
}

// DockerBuildContext creates a tar archive from the registered Docker files,
// suitable for passing to the Docker API's ImageBuild.
func DockerBuildContext() (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	for _, f := range dockerFiles {
		header := &tar.Header{
			Name: f.Name,
			Size: int64(len(f.Data)),
			Mode: 0o755,
		}
		if err := tw.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := tw.Write(f.Data); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}
