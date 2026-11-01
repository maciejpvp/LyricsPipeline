// Package lambdabuild compiles Go programs into reproducible Linux Lambda archives.
package lambdabuild

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Args specifies a Go program and the independent destination for its Linux artifact.
type Args struct {
	ModuleDir    string
	Package      string
	OutputDir    string
	Architecture string // Go architecture: arm64 or amd64.
}

type Artifact struct {
	Path         string
	Architecture string
}

// Build compiles the entry package with CGO disabled, then atomically publishes
// a ZIP containing an executable bootstrap. It never caches application artifacts.
func Build(ctx context.Context, args Args) (Artifact, error) {
	if args.Architecture != "arm64" && args.Architecture != "amd64" {
		return Artifact{}, fmt.Errorf("unsupported Lambda architecture %q", args.Architecture)
	}
	if args.ModuleDir == "" || args.Package == "" || args.OutputDir == "" {
		return Artifact{}, errors.New("module directory, entry package and output directory are required")
	}
	output, err := filepath.Abs(args.OutputDir)
	if err != nil {
		return Artifact{}, err
	}
	if err = os.MkdirAll(output, 0755); err != nil {
		return Artifact{}, err
	}
	temporary, err := os.MkdirTemp(output, ".build-")
	if err != nil {
		return Artifact{}, err
	}
	defer os.RemoveAll(temporary)
	bootstrap := filepath.Join(temporary, "bootstrap")
	command := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-trimpath", "-tags=lambda.norpc", "-ldflags=-s -w -buildid=", "-o", bootstrap, args.Package)
	command.Dir = args.ModuleDir
	command.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+args.Architecture, "CGO_ENABLED=0")
	if message, err := command.CombinedOutput(); err != nil {
		return Artifact{}, fmt.Errorf("build Go Lambda: %w: %s", err, message)
	}
	archive := filepath.Join(temporary, "lambda.zip")
	if err = zipBootstrap(archive, bootstrap); err != nil {
		return Artifact{}, err
	}
	target := filepath.Join(output, "lambda.zip")
	if err = os.Rename(archive, target); err != nil {
		return Artifact{}, err
	}
	return Artifact{Path: target, Architecture: args.Architecture}, nil
}
func zipBootstrap(destination, bootstrap string) (err error) {
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()
	archive := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "bootstrap", Method: zip.Deflate}
	header.SetMode(0755)
	header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	binary, err := os.Open(bootstrap)
	if err != nil {
		return err
	}
	defer binary.Close()
	if _, err = io.Copy(writer, binary); err != nil {
		return err
	}
	return archive.Close()
}
