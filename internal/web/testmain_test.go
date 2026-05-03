package web

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain disables the runtime helpy MCP socket for all tests in this
// package. Production runtime talks to `helpy mcp-serve --unix-socket ...`,
// but tests use httptest-backed MCPs instead.
func TestMain(m *testing.M) {
	_ = os.Setenv("SLOPSHELL_HELPY_SOCKET", "off")
	_ = os.Setenv("SLOPTOOLS_VAULT_CONFIG", filepath.Join(os.TempDir(), "slopshell-test-vaults.toml"))
	os.Exit(m.Run())
}
