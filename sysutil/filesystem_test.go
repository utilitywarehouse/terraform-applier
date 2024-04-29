package sysutil

import (
	"os"
	"path"
	"testing"
)

func TestCopyDirWithReplace(t *testing.T) {
	// Create a temporary directory for testing.
	testTmp, err := os.MkdirTemp("", "tf-applier-copy-dir-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(testTmp)

	srcDir := path.Join(testTmp, "src")
	dstDir := path.Join(testTmp, "dst")

	// Create some test files.
	writeFile := func(name string, content []byte) {
		err = os.WriteFile(name, content, 0644)
		if err != nil {
			t.Fatalf("error creating file: %v", err)
		}
	}

	files := []string{"file1", "file2"}
	dirs := []string{"subDir1", "subDir2"}
	nestedDirs := []string{"nestedDir1", "nestedDir2"}

	// create source file structure
	for _, dir := range dirs {
		for _, nDir := range nestedDirs {
			p := path.Join(srcDir, dir, nDir)
			if err := os.MkdirAll(p, 0700); err != nil {
				t.Fatal(err)
			}

			for _, file := range files {
				name := path.Join(srcDir, dir, nDir, file)
				writeFile(name, []byte("src file contents"))
			}
		}

		for _, file := range files {
			name := path.Join(srcDir, dir, file)
			writeFile(name, []byte("src file contents"))
		}
	}

	// Copy the directory.
	err = CopyDir(srcDir, dstDir, true)
	if err != nil {
		t.Fatalf("error copying directory: %v", err)
	}

	verify := func(name string, want []byte) {
		_, err := os.Stat(name)
		if err != nil {
			t.Fatalf("error verifying %s: %v", name, err)
		}
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("error reading %s: %v", name, err)
		}
		if string(got) != string(want) {
			t.Fatalf("file content mismatch name:%s got:%s want:%s", name, got, want)
		}
	}

	// verify dst file structure
	for _, dir := range dirs {
		for _, nDir := range nestedDirs {
			for _, file := range files {
				name := path.Join(dstDir, dir, nDir, file)
				verify(name, []byte("src file contents"))
			}
		}
		for _, file := range files {
			name := path.Join(dstDir, dir, file)
			verify(name, []byte("src file contents"))
		}
	}
}

func TestCopyDirWithoutReplace(t *testing.T) {
	// Create a temporary directory for testing.
	testTmp, err := os.MkdirTemp("", "tf-applier-copy-dir-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(testTmp)

	srcDir := path.Join(testTmp, "src")
	dstDir := path.Join(testTmp, "dst")

	// Create some test files.
	writeFile := func(name string, content []byte) {
		err = os.WriteFile(name, content, 0644)
		if err != nil {
			t.Fatalf("error creating file: %v", err)
		}
	}

	files := []string{"file1", "file2"}
	dirs := []string{"subDir1", "subDir2"}
	nestedDirs := []string{"nestedDir1", "nestedDir2"}

	// create source file structure
	for _, dir := range dirs {
		for _, nDir := range nestedDirs {
			p := path.Join(srcDir, dir, nDir)
			if err := os.MkdirAll(p, 0700); err != nil {
				t.Fatal(err)
			}

			for _, file := range files {
				name := path.Join(srcDir, dir, nDir, file)
				writeFile(name, []byte("src file contents"))
			}
		}

		for _, file := range files {
			name := path.Join(srcDir, dir, file)
			writeFile(name, []byte("src file contents"))
		}
	}

	// create some dest file structure
	for _, dir := range dirs {
		for _, nDir := range nestedDirs {
			p := path.Join(dstDir, dir, nDir)
			if err := os.MkdirAll(p, 0700); err != nil {
				t.Fatal(err)
			}

			name := path.Join(dstDir, dir, nDir, files[0])
			writeFile(name, []byte("dst file contents"))
		}

		name := path.Join(dstDir, dir, files[0])
		writeFile(name, []byte("dst file contents"))
	}

	// Copy the directory.
	err = CopyDir(srcDir, dstDir, false)
	if err != nil {
		t.Fatalf("error copying directory: %v", err)
	}

	verify := func(name string, want []byte) {
		_, err := os.Stat(name)
		if err != nil {
			t.Fatalf("error verifying %s: %v", name, err)
		}
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("error reading %s: %v", name, err)
		}
		if string(got) != string(want) {
			t.Fatalf("file content mismatch name:%s got:%s want:%s", name, got, want)
		}
	}

	// verify dst file structure
	for _, dir := range dirs {
		for _, nDir := range nestedDirs {
			for i, file := range files {
				name := path.Join(dstDir, dir, nDir, file)
				if i == 0 {
					verify(name, []byte("dst file contents"))
				} else {
					verify(name, []byte("src file contents"))
				}
			}
		}
		for i, file := range files {
			name := path.Join(dstDir, dir, file)
			if i == 0 {
				verify(name, []byte("dst file contents"))
			} else {
				verify(name, []byte("src file contents"))
			}
		}
	}
}
