package sysutil

import (
	"os"
	"path"
	"path/filepath"
	"testing"
)

func TestCopyDirWithReplace(t *testing.T) {
	// Create a temporary directory for testing.
	testTmp := t.TempDir()

	srcDir := path.Join(testTmp, "src")
	dstDir := path.Join(testTmp, "dst")

	// Create some test files.
	writeFile := func(name string, content []byte) {
		err := os.WriteFile(name, content, 0644)
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
	err := CopyDir(srcDir, dstDir, true)
	if err != nil {
		t.Fatalf("error copying directory: %v", err)
	}
	// verify dst file structure
	for _, dir := range dirs {
		for _, nDir := range nestedDirs {
			for _, file := range files {
				name := path.Join(dstDir, dir, nDir, file)
				assertFileContent(t, name, []byte("src file contents"))
			}
		}
		for _, file := range files {
			name := path.Join(dstDir, dir, file)
			assertFileContent(t, name, []byte("src file contents"))
		}
	}
}

func TestCopyDirWithoutReplace(t *testing.T) {
	// Create a temporary directory for testing.
	testTmp := t.TempDir()

	srcDir := path.Join(testTmp, "src")
	dstDir := path.Join(testTmp, "dst")

	// Create some test files.
	writeFile := func(name string, content []byte) {
		err := os.WriteFile(name, content, 0644)
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
	err := CopyDir(srcDir, dstDir, false)
	if err != nil {
		t.Fatalf("error copying directory: %v", err)
	}

	// verify dst file structure
	for _, dir := range dirs {
		for _, nDir := range nestedDirs {
			for i, file := range files {
				name := path.Join(dstDir, dir, nDir, file)
				if i == 0 {
					assertFileContent(t, name, []byte("dst file contents"))
				} else {
					assertFileContent(t, name, []byte("src file contents"))
				}
			}
		}
		for i, file := range files {
			name := path.Join(dstDir, dir, file)
			if i == 0 {
				assertFileContent(t, name, []byte("dst file contents"))
			} else {
				assertFileContent(t, name, []byte("src file contents"))
			}
		}
	}
}

func TestRemoveDirContentsRecursiveIf(t *testing.T) {
	// Create a temporary directory for testing.
	testTmp := t.TempDir()

	target := path.Join(testTmp, "src")

	// Create some test files.
	writeFile := func(name string, content []byte) {
		err := os.WriteFile(name, content, 0644)
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
			p := path.Join(target, dir, nDir)
			if err := os.MkdirAll(p, 0700); err != nil {
				t.Fatal(err)
			}

			for _, file := range files {
				name := path.Join(target, dir, nDir, file)
				writeFile(name, []byte("src file contents"))
			}
		}

		for _, file := range files {
			name := path.Join(target, dir, file)
			writeFile(name, []byte("src file contents"))
		}
	}

	// delete all file2
	err := RemoveDirContentsRecursiveIf(target,
		func(path string, fi os.FileInfo) (bool, error) { return fi.Name() == "file2", nil })
	if err != nil {
		t.Fatalf("error removing contents: %v", err)
	}

	// verify file1 exits and file2 deleted
	for _, dir := range dirs {
		for _, nDir := range nestedDirs {
			assertFileContent(t, path.Join(target, dir, nDir, "file1"), []byte("src file contents"))
			assertMissing(t, path.Join(target, dir, nDir, "file2"))
		}
		assertFileContent(t, path.Join(target, dir, "file1"), []byte("src file contents"))
		assertMissing(t, path.Join(target, dir, "file2"))
	}

	// delete all
	err = RemoveDirContentsRecursiveIf(target,
		func(path string, fi os.FileInfo) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("error removing contents: %v", err)
	}

	// target dir should be empty
	if empty, err := IsDirEmpty(target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if !empty {
		t.Errorf("expected %q to be deemed not-empty", target)
	}
}

func Test_removeDirContentsIf(t *testing.T) {
	tempRoot := t.TempDir()

	// create target folder with some files
	target := filepath.Join(tempRoot, "target")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatalf("failed to make a temp subdir: %v", err)
	}
	for _, file := range []string{"a", "b", "c"} {
		path := filepath.Join(target, file)
		if err := os.WriteFile(path, []byte{}, 0755); err != nil {
			t.Fatalf("failed to write a file: %v", err)
		}
	}

	// should delete everything form the target dir
	err := RemoveDirContentsIf(target, func(path string, fi os.FileInfo) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// target dir should be empty
	if empty, err := IsDirEmpty(target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if !empty {
		t.Errorf("expected %q to be deemed not-empty", target)
	}

	// add more files and dir
	for _, file := range []string{"a1", "b2", "c2", "d2"} {
		path := filepath.Join(target, file)
		if err := os.WriteFile(path, []byte{}, 0755); err != nil {
			t.Fatalf("failed to write a file: %v", err)
		}
	}
	if err := os.Mkdir(filepath.Join(target, "Dirs"), 0755); err != nil {
		t.Fatalf("failed to make a subdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(target, ".git"), 0755); err != nil {
		t.Fatalf("failed to make a subdir: %v", err)
	}

	// should delete everything except file b2
	if err := RemoveDirContentsIf(
		target,
		func(path string, fi os.FileInfo) (bool, error) { return fi.Name() != "b2", nil },
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "b2")); err != nil {
		t.Fatalf("failed to read %q : %v", filepath.Join(target, "b2"), err)
	}

	// folder test
	dirTarget := filepath.Join(tempRoot, "target2")
	// create folder F1/F1.1
	// create folder F2/F2.1
	if err := os.MkdirAll(path.Join(dirTarget, "F1", "F1.1"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path.Join(dirTarget, "F2", "F2.1"), 0700); err != nil {
		t.Fatal(err)
	}

	// should delete everything except file F2
	if err := RemoveDirContentsIf(
		dirTarget,
		func(path string, fi os.FileInfo) (bool, error) { return fi.Name() != "F2", nil },
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dirTarget, "F2")); err != nil {
		t.Fatalf("failed to read %q : %v", filepath.Join(dirTarget, "F2"), err)
	}

	// should delete everything form the target dir
	err = RemoveDirContentsIf(dirTarget, func(path string, fi os.FileInfo) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// target dir should be empty
	if empty, err := IsDirEmpty(dirTarget); err != nil {
		t.Fatalf("unexpected error: %v", err)
	} else if !empty {
		t.Errorf("expected %q to be deemed not-empty", dirTarget)
	}
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()

	_, err := os.Stat(path)
	if err != nil {
		t.Fatalf("error verifying %s: %v", path, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("error reading %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("file content mismatch name:%s got:%s want:%s", path, got, want)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()

	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("unable to read existing file error: %v", err)
	} else if os.IsNotExist(err) {
		return
	} else {
		t.Errorf("file (%s) exits but it should not", path)
	}
}
