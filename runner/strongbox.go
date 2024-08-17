package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"filippo.io/age/armor"
)

const strongboxKeyRingFile string = ".strongbox_keyring"
const strongboxIdentityFile string = ".strongbox_identity"

func ensureDecryption(ctx context.Context, cwd string, sbKeyringData string, sbIdentityData string) error {

	if sbKeyringData == "" && sbIdentityData == "" {
		return nil
	}

	// setup env for strongbox command
	runEnv := []string{
		// HOME is also used to setup git config in current dir
		fmt.Sprintf("HOME=%s", cwd),
		//setup SB home for strongbox decryption
		fmt.Sprintf("STRONGBOX_HOME=%s", cwd),
		fmt.Sprintf("PATH=%s", os.Getenv("PATH")),
	}

	// setup git config via `strongbox -git-config`
	if err := setupGitConfigForSB(ctx, cwd, runEnv); err != nil {
		return err
	}

	if sbIdentityData != "" {
		// create strongbox identity file this will also be used by
		// terraform to decrypt remote module secrets using git/SB
		identityPath := filepath.Join(cwd, strongboxIdentityFile)
		if err := os.WriteFile(identityPath, []byte(sbIdentityData), 0600); err != nil {
			return fmt.Errorf("error writing sb identity file %w", err)
		}

		identities, err := age.ParseIdentities(bytes.NewBuffer([]byte(sbIdentityData)))
		if err != nil {
			return fmt.Errorf("error parsing age identity file err:%w", err)
		}

		if err := strongboxAgeRecursiveDecrypt(cwd, identities); err != nil {
			return fmt.Errorf("error decrypting via age err:%w", err)
		}
	}

	if sbKeyringData != "" {
		// create strongbox keyRing file this will also be used by
		// terraform to decrypt remote module secrets using git/SB
		keyRingPath := filepath.Join(cwd, strongboxKeyRingFile)
		if err := os.WriteFile(keyRingPath, []byte(sbKeyringData), 0600); err != nil {
			return fmt.Errorf("error writing sb keyring file %w", err)
		}

		// ensure local siv decryption if any
		if err := runStrongboxDecryption(ctx, cwd, runEnv); err != nil {
			return fmt.Errorf("error decrypting via siv err:%w", err)
		}
	}

	return nil
}

// setupGitConfigForSB will setup git filters to run strongbox
func setupGitConfigForSB(ctx context.Context, cwd string, runEnv []string) error {
	s := exec.CommandContext(ctx, "strongbox", "-git-config")
	s.Dir = cwd
	s.Env = runEnv

	stderr, err := s.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running strongbox -git-config  err:%s ", stderr)
	}

	return nil
}

// runStrongboxDecryption will try to decrypt files in cwd using given keyRing file
// make sure HOME or STRONGBOX_HOME is set in runEnv
func runStrongboxDecryption(ctx context.Context, cwd string, runEnv []string) error {
	s := exec.CommandContext(ctx, "strongbox", "-decrypt", "-recursive", cwd)
	s.Dir = cwd
	s.Env = runEnv

	stderr, err := s.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running strongbox -decrypt err:%s ", stderr)
	}

	return nil
}

func strongboxAgeRecursiveDecrypt(cwd string, identities []age.Identity) error {
	return filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// skip .git directory
			if info.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}

		file, err := os.OpenFile(path, os.O_RDWR, 0644)
		if err != nil {
			return err
		}
		defer file.Close()

		armorReader := armor.NewReader(file)
		ar, err := age.Decrypt(armorReader, identities...)
		if err != nil {
			// Doesn't have age header
			// https://github.com/FiloSottile/age/blob/main/age.go#L208-L215
			if strings.HasPrefix(err.Error(), "failed to read header") {
				return nil
			}
			return err
		}

		if err = file.Truncate(0); err != nil {
			return err
		}

		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}

		_, err = io.Copy(file, ar)
		if err != nil {
			return err
		}

		return nil
	})
}
