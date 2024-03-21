package git

import (
	"fmt"
	"os"
)

func GitSSHCommand(sshKeyPath, knownHostsFilePath string, verifyKnownHosts bool) (string, error) {
	knownHostsFragment := `-o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no`

	if _, err := os.Stat(sshKeyPath); err != nil {
		return "", fmt.Errorf("can't access SSH key file %s: %w", sshKeyPath, err)
	}

	if verifyKnownHosts {
		if _, err := os.Stat(knownHostsFilePath); err != nil {
			return "", fmt.Errorf("can't access SSH known_hosts file %s: %w", knownHostsFilePath, err)
		}
		knownHostsFragment = fmt.Sprintf("-o StrictHostKeyChecking=yes -o UserKnownHostsFile=%s", knownHostsFilePath)
	}

	return fmt.Sprintf(`ssh -q -F none -o IdentitiesOnly=yes -i %s %s`, sshKeyPath, knownHostsFragment), nil
}
