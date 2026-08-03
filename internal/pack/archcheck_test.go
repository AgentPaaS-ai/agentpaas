package pack

import (
	"debug/elf"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestHarnessArchMatches(t *testing.T) {
	tests := []struct {
		name     string
		machine  elf.Machine
		platform string
		want     bool
	}{
		{
			name:     "amd64 harness for linux amd64",
			machine:  elf.EM_X86_64,
			platform: "linux/amd64",
			want:     true,
		},
		{
			name:     "arm64 harness for linux amd64",
			machine:  elf.EM_AARCH64,
			platform: "linux/amd64",
			want:     false,
		},
		{
			name:     "amd64 harness for linux arm64",
			machine:  elf.EM_X86_64,
			platform: "linux/arm64",
			want:     false,
		},
		{
			name:     "darwin host skips cross arch check",
			machine:  elf.EM_AARCH64,
			platform: "darwin/arm64",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "harness")
			if err := os.WriteFile(path, testELF64(tt.machine), 0o755); err != nil {
				t.Fatalf("os.WriteFile() error = %v", err)
			}

			got, err := harnessArchMatches(path, tt.platform)
			if err != nil {
				t.Fatalf("harnessArchMatches() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("harnessArchMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func testELF64(machine elf.Machine) []byte {
	header := make([]byte, 64)
	header[0] = 0x7f
	header[1] = 'E'
	header[2] = 'L'
	header[3] = 'F'
	header[4] = byte(elf.ELFCLASS64)
	header[5] = byte(elf.ELFDATA2LSB)
	header[6] = 1
	binary.LittleEndian.PutUint16(header[16:18], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(header[18:20], uint16(machine))
	binary.LittleEndian.PutUint32(header[20:24], 1)
	binary.LittleEndian.PutUint16(header[52:54], 64)
	binary.LittleEndian.PutUint16(header[58:60], 64)
	return header
}
