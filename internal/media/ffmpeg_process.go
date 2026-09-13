package media

import (
	"os/exec"
	"time"
)

// managedProcess is platform process-tree cleanup around one external command.
type managedProcess interface {
	Attached(*exec.Cmd) error
	Close(error)
}

func runManaged(cmd *exec.Cmd) (err error) {
	tree, err := newManagedProcess(cmd)
	if err != nil {
		return err
	}
	defer func() { tree.Close(err) }()
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := tree.Attached(cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	return cmd.Wait()
}
