package pack

import (
	"debug/elf"
	"fmt"
)

func harnessArchMatches(path string, platform string) (bool, error) {
	want, ok := harnessMachineForPlatform(platform)
	if !ok {
		return true, nil
	}

	machine, err := readHarnessMachine(path)
	if err != nil {
		return false, err
	}
	return machine == want, nil
}

func validateHarnessArchitecture(path string, platform string) error {
	matches, err := harnessArchMatches(path, platform)
	if err != nil {
		return fmt.Errorf("read harness architecture: %w", err)
	}
	if matches {
		return nil
	}

	machine, err := readHarnessMachine(path)
	if err != nil {
		return fmt.Errorf("read harness architecture: %w", err)
	}
	actual := harnessMachineName(machine)
	installBinary, makeTarget := harnessInstallHint(platform)
	return fmt.Errorf("harness binary %s is %s but target is %s — install/build %s (see Makefile %s)",
		path, actual, platform, installBinary, makeTarget)
}

func harnessMachineForPlatform(platform string) (elf.Machine, bool) {
	switch platform {
	case "linux/amd64":
		return elf.EM_X86_64, true
	case "linux/arm64":
		return elf.EM_AARCH64, true
	default:
		return 0, false
	}
}

func readHarnessMachine(path string) (elf.Machine, error) {
	if err := rejectSymlinkPath(path, false); err != nil {
		return 0, err
	}

	f, err := elf.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open %s as ELF: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return f.Machine, nil
}

func harnessMachineName(machine elf.Machine) string {
	switch machine {
	case elf.EM_X86_64:
		return "x86-64"
	case elf.EM_AARCH64:
		return "arm64"
	default:
		return fmt.Sprintf("ELF machine %d", machine)
	}
}

func harnessInstallHint(platform string) (string, string) {
	if platform == "linux/arm64" {
		return "agentpaas-harness-linux", "build-harness-linux"
	}
	return "agentpaas-harness-linux-amd64", "build-harness-linux-amd64"
}
