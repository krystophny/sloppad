package web

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain disables the stdio helpy MCP subprocess for all tests in this
// package. Production runtime spawns `helpy mcp-stdio`, but tests use
// httptest-backed MCPs and must not fork a real subprocess.
func TestMain(m *testing.M) {
	_ = os.Setenv("SLOPSHELL_HELPY_BIN", "off")
	_ = os.Setenv("SLOPTOOLS_VAULT_CONFIG", filepath.Join(os.TempDir(), "slopshell-test-vaults.toml"))
	os.Exit(m.Run())
}
