package lambdabuild

import (
	"archive/zip"
	"bytes"
	"context"
	"debug/elf"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapArchiveExecutableAndReproducible(t *testing.T) {
	folder := t.TempDir()
	binary := filepath.Join(folder, "bootstrap")
	if err := os.WriteFile(binary, []byte("fixture executable"), 0755); err != nil {
		t.Fatal(err)
	}
	first, second := filepath.Join(folder, "first.zip"), filepath.Join(folder, "second.zip")
	for _, path := range []string{first, second} {
		if err := zipBootstrap(path, binary); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := os.ReadFile(first)
	b, _ := os.ReadFile(second)
	if !bytes.Equal(a, b) {
		t.Fatal("unchanged binary produces a different artifact")
	}
	archive, err := zip.OpenReader(first)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].Name != "bootstrap" || archive.File[0].Mode().Perm() != 0755 {
		t.Fatal("Lambda archive must contain one executable bootstrap at root")
	}
}

// Two unrelated programs and two ELF architectures prove that Build takes its
// entrypoint from the caller rather than caching a repository-specific binary.
func TestBuildIndependentPrograms(t *testing.T) {
	module := t.TempDir()
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.test/functions\n\ngo 1.26\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, program := range []string{"first", "second"} {
		directory := filepath.Join(module, program)
		if err := os.Mkdir(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "main.go"), []byte("package main\nimport \"fmt\"\nfunc main(){fmt.Println(\""+program+"\")}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, architecture := range []string{"arm64", "amd64"} {
		t.Run(architecture, func(t *testing.T) {
			outputs := map[string][]byte{}
			for _, program := range []string{"first", "second"} {
				args := Args{ModuleDir: module, Package: "./" + program, OutputDir: filepath.Join(t.TempDir(), program), Architecture: architecture}
				first, err := Build(t.Context(), args)
				if err != nil {
					t.Fatal(err)
				}
				if first.Architecture != architecture || first.Path != filepath.Join(args.OutputDir, "lambda.zip") {
					t.Fatalf("wrong artifact metadata: %+v", first)
				}
				data, err := os.ReadFile(first.Path)
				if err != nil {
					t.Fatal(err)
				}
				outputs[program] = data
				// Independent output directory must have byte-identical archive bytes.
				args.OutputDir = t.TempDir()
				second, err := Build(t.Context(), args)
				if err != nil {
					t.Fatal(err)
				}
				again, err := os.ReadFile(second.Path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(data, again) {
					t.Fatal("build not reproducible")
				}
				archive, err := zip.OpenReader(first.Path)
				if err != nil {
					t.Fatal(err)
				}
				defer archive.Close()
				if len(archive.File) != 1 || archive.File[0].Name != "bootstrap" || archive.File[0].Mode().Perm() != 0755 {
					t.Fatal("invalid bootstrap archive")
				}
				entry, err := archive.File[0].Open()
				if err != nil {
					t.Fatal(err)
				}
				binary, err := io.ReadAll(entry)
				entry.Close()
				if err != nil {
					t.Fatal(err)
				}
				executable, err := elf.NewFile(bytes.NewReader(binary))
				if err != nil {
					t.Fatal(err)
				}
				defer executable.Close()
				want := elf.EM_AARCH64
				if architecture == "amd64" {
					want = elf.EM_X86_64
				}
				if executable.Machine != want {
					t.Fatalf("machine=%v want=%v", executable.Machine, want)
				}
			}
			if bytes.Equal(outputs["first"], outputs["second"]) {
				t.Fatal("different programs share a stale artifact")
			}
		})
	}
	// Rebuilding an edited entrypoint in the same module/output must pick up edits.
	args := Args{ModuleDir: module, Package: "./first", OutputDir: t.TempDir(), Architecture: "arm64"}
	artifact, err := Build(t.Context(), args)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(artifact.Path)
	if err = os.WriteFile(filepath.Join(module, "first/main.go"), []byte("package main\nfunc main(){}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	artifact, err = Build(t.Context(), args)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(artifact.Path)
	if bytes.Equal(before, after) {
		t.Fatal("source change ignored")
	}
}
func TestBuildRejectsInvalidInputsAndFailures(t *testing.T) {
	for _, args := range []Args{{}, {ModuleDir: ".", Package: ".", OutputDir: t.TempDir(), Architecture: "mips"}} {
		if _, err := Build(t.Context(), args); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
	module := t.TempDir()
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.test/invalid\ngo 1.26\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	args := Args{ModuleDir: module, Package: "./missing", OutputDir: output, Architecture: "arm64"}
	if _, err := Build(t.Context(), args); err == nil {
		t.Fatal("build failure ignored")
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("failed build left temporary artifacts")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Build(ctx, args); err == nil {
		t.Fatal("cancelled build succeeded")
	}
}
