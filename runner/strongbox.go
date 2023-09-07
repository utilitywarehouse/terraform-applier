package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const strongboxKeyRingFile string = ".strongbox_keyring"

func ensureDecryption(ctx context.Context, cwd string, sbKeyringData string) error {

	keyRingPath := filepath.Join(cwd, strongboxKeyRingFile)

	// create strongbox keyRing file
	if err := os.WriteFile(keyRingPath, []byte(sbKeyringData), 0600); err != nil {
		return fmt.Errorf("error writing sb keyring file %w", err)
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

	return runStrongboxDecryption(ctx, cwd, runEnv, keyRingPath)
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
func runStrongboxDecryption(ctx context.Context, cwd string, runEnv []string, keyringPath string) error {
	s := exec.CommandContext(ctx, "strongbox", "-keyring", keyringPath, "-decrypt", "-recursive", cwd)
	s.Dir = cwd
	s.Env = runEnv

	stderr, err := s.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error running strongbox -decrypt err:%s ", stderr)
	}

	return nil
}
