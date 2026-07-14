package messagefile

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/n-r-w/team-mcp/internal/domain"
)

// serviceSuite exercises message export behavior against real temporary filesystems.
type serviceSuite struct {
	suite.Suite
	homeDir    string
	outsideDir string
	service    *Service
}

// TestServiceSuite runs message file writer behavior tests.
func TestServiceSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(serviceSuite))
}

// SetupTest creates isolated home and external directories for each test.
func (s *serviceSuite) SetupTest() {
	s.homeDir = s.T().TempDir()
	s.outsideDir = s.T().TempDir()
	s.service = New(s.homeDir)
}

// TestCreateWritesExactContentAndCreatesParents verifies exclusive creation preserves payload bytes and creates parents.
func (s *serviceSuite) TestCreateWritesExactContentAndCreatesParents() {
	content := "  first line\nlast line  "
	filePath := filepath.Join(s.homeDir, "nested", "message.md")

	err := s.service.WriteMessage(s.T().Context(), filePath, domain.MessageFileModeCreate, content)
	s.Require().NoError(err)

	payload, err := os.ReadFile(filePath)
	s.Require().NoError(err)
	s.Equal(content, string(payload))
}

// TestCreatePreservesExistingFile verifies create mode returns an existence error without changing content.
func (s *serviceSuite) TestCreatePreservesExistingFile() {
	filePath := filepath.Join(s.homeDir, "message.txt")
	s.Require().NoError(os.WriteFile(filePath, []byte("existing"), 0o600))

	err := s.service.WriteMessage(s.T().Context(), filePath, domain.MessageFileModeCreate, "replacement")
	s.Require().Error(err)
	s.Require().ErrorIs(err, fs.ErrExist)

	payload, readErr := os.ReadFile(filePath)
	s.Require().NoError(readErr)
	s.Equal("existing", string(payload))
}

// TestOverwriteTruncatesExistingFile verifies overwrite replaces longer existing content.
func (s *serviceSuite) TestOverwriteTruncatesExistingFile() {
	filePath := filepath.Join(s.homeDir, "message.md")
	s.Require().NoError(os.WriteFile(filePath, []byte("long existing content"), 0o600))

	err := s.service.WriteMessage(s.T().Context(), filePath, domain.MessageFileModeOverwrite, "new")
	s.Require().NoError(err)

	payload, readErr := os.ReadFile(filePath)
	s.Require().NoError(readErr)
	s.Equal("new", string(payload))
}

// TestOverwriteCreatesMissingFile verifies overwrite creates a destination that does not exist.
func (s *serviceSuite) TestOverwriteCreatesMissingFile() {
	filePath := filepath.Join(s.homeDir, "new", "message.txt")

	err := s.service.WriteMessage(s.T().Context(), filePath, domain.MessageFileModeOverwrite, "payload")
	s.Require().NoError(err)

	payload, readErr := os.ReadFile(filePath)
	s.Require().NoError(readErr)
	s.Equal("payload", string(payload))
}

// TestRejectsPathsOutsideHome verifies relative, sibling, and parent-escape paths are rejected without writes.
func (s *serviceSuite) TestRejectsPathsOutsideHome() {
	separator := string(filepath.Separator)
	parentEscape := s.homeDir + separator + ".." + separator + filepath.Base(s.outsideDir) + separator + "escape.md"
	testCases := map[string]string{
		"relative":      filepath.Join("nested", "message.md"),
		"outside":       filepath.Join(s.outsideDir, "message.md"),
		"parent escape": parentEscape,
	}

	for name, filePath := range testCases {
		s.Run(name, func() {
			err := s.service.WriteMessage(s.T().Context(), filePath, domain.MessageFileModeCreate, "payload")
			s.Require().Error(err)
		})
	}

	_, err := os.Stat(filepath.Join(s.outsideDir, "message.md"))
	s.Require().ErrorIs(err, fs.ErrNotExist)
	_, err = os.Stat(filepath.Join(s.outsideDir, "escape.md"))
	s.Require().ErrorIs(err, fs.ErrNotExist)
}

// TestRejectsUnsupportedExtensions verifies only exact lowercase .txt and .md suffixes are accepted.
func (s *serviceSuite) TestRejectsUnsupportedExtensions() {
	for _, name := range []string{"message", "message.MD", "message.txt.bak", "message.md "} {
		s.Run(name, func() {
			err := s.service.WriteMessage(
				s.T().Context(),
				filepath.Join(s.homeDir, name),
				domain.MessageFileModeCreate,
				"payload",
			)
			s.Require().Error(err)
		})
	}
}

// TestRejectsExternalSymbolicLink verifies rooted traversal cannot write through a link outside home.
func (s *serviceSuite) TestRejectsExternalSymbolicLink() {
	linkPath := filepath.Join(s.homeDir, "external")
	s.Require().NoError(os.Symlink(s.outsideDir, linkPath))

	err := s.service.WriteMessage(
		s.T().Context(),
		filepath.Join(linkPath, "message.md"),
		domain.MessageFileModeCreate,
		"payload",
	)
	s.Require().Error(err)

	_, statErr := os.Stat(filepath.Join(s.outsideDir, "message.md"))
	s.Require().ErrorIs(statErr, fs.ErrNotExist)
}

// TestRejectsRelativeExternalSymbolicLink verifies a relative parent link cannot escape through dot-dot components.
func (s *serviceSuite) TestRejectsRelativeExternalSymbolicLink() {
	relativeTarget, err := filepath.Rel(s.homeDir, s.outsideDir)
	s.Require().NoError(err)
	linkPath := filepath.Join(s.homeDir, "relative-external")
	s.Require().NoError(os.Symlink(relativeTarget, linkPath))

	err = s.service.WriteMessage(
		s.T().Context(),
		filepath.Join(linkPath, "message.md"),
		domain.MessageFileModeCreate,
		"payload",
	)
	s.Require().Error(err)

	_, statErr := os.Stat(filepath.Join(s.outsideDir, "message.md"))
	s.Require().ErrorIs(statErr, fs.ErrNotExist)
}

// TestRejectsFinalExternalSymbolicLink verifies overwrite cannot follow a final link outside home.
func (s *serviceSuite) TestRejectsFinalExternalSymbolicLink() {
	outsidePath := filepath.Join(s.outsideDir, "message.md")
	s.Require().NoError(os.WriteFile(outsidePath, []byte("existing"), 0o600))
	linkPath := filepath.Join(s.homeDir, "message.md")
	s.Require().NoError(os.Symlink(outsidePath, linkPath))

	err := s.service.WriteMessage(s.T().Context(), linkPath, domain.MessageFileModeOverwrite, "replacement")
	s.Require().Error(err)

	payload, readErr := os.ReadFile(outsidePath)
	s.Require().NoError(readErr)
	s.Equal("existing", string(payload))
}

// TestAllowsFinalRelativeInternalSymbolicLink verifies overwrite follows a final relative link that stays inside home.
func (s *serviceSuite) TestAllowsFinalRelativeInternalSymbolicLink() {
	targetPath := filepath.Join(s.homeDir, "target.md")
	s.Require().NoError(os.WriteFile(targetPath, []byte("existing"), 0o600))
	linkPath := filepath.Join(s.homeDir, "message.md")
	s.Require().NoError(os.Symlink(filepath.Base(targetPath), linkPath))

	err := s.service.WriteMessage(s.T().Context(), linkPath, domain.MessageFileModeOverwrite, "replacement")
	s.Require().NoError(err)

	payload, readErr := os.ReadFile(targetPath)
	s.Require().NoError(readErr)
	s.Equal("replacement", string(payload))
}

// TestRejectsAbsoluteInternalSymbolicLink verifies os.Root rejects absolute links even when the target remains within home.
func (s *serviceSuite) TestRejectsAbsoluteInternalSymbolicLink() {
	targetDir := filepath.Join(s.homeDir, "target")
	s.Require().NoError(os.Mkdir(targetDir, 0o750))
	linkPath := filepath.Join(s.homeDir, "absolute")
	s.Require().NoError(os.Symlink(targetDir, linkPath))

	err := s.service.WriteMessage(
		s.T().Context(),
		filepath.Join(linkPath, "message.md"),
		domain.MessageFileModeCreate,
		"payload",
	)
	s.Require().Error(err)
}

// TestAllowsRelativeInternalSymbolicLink verifies safe relative links remain usable inside home.
func (s *serviceSuite) TestAllowsRelativeInternalSymbolicLink() {
	targetDir := filepath.Join(s.homeDir, "target")
	s.Require().NoError(os.Mkdir(targetDir, 0o750))
	linkPath := filepath.Join(s.homeDir, "relative")
	s.Require().NoError(os.Symlink("target", linkPath))

	err := s.service.WriteMessage(
		s.T().Context(),
		filepath.Join(linkPath, "message.md"),
		domain.MessageFileModeCreate,
		"payload",
	)
	s.Require().NoError(err)

	payload, readErr := os.ReadFile(filepath.Join(targetDir, "message.md"))
	s.Require().NoError(readErr)
	s.Equal("payload", string(payload))
}

// TestRootOpenFailurePropagates verifies a missing configured home produces operation context.
func (s *serviceSuite) TestRootOpenFailurePropagates() {
	s.Require().NoError(os.RemoveAll(s.homeDir))

	err := s.service.WriteMessage(
		s.T().Context(),
		filepath.Join(s.homeDir, "message.md"),
		domain.MessageFileModeCreate,
		"payload",
	)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "open user home directory")
}

// TestWriteAndClosePreservesErrors verifies write and close failures remain in one returned error chain.
func (s *serviceSuite) TestWriteAndClosePreservesErrors() {
	file, err := os.CreateTemp(s.homeDir, "closed-*")
	s.Require().NoError(err)
	s.Require().NoError(file.Close())

	err = writeAndClose(file, "payload")
	s.Require().Error(err)
	s.Require().ErrorContains(err, "write destination file")
	s.Require().ErrorContains(err, "close destination file")
	s.Require().ErrorIs(err, fs.ErrClosed)
}

// TestRejectsNonDirectoryParent verifies parent creation failures reach the caller.
func (s *serviceSuite) TestRejectsNonDirectoryParent() {
	parentPath := filepath.Join(s.homeDir, "parent")
	s.Require().NoError(os.WriteFile(parentPath, []byte("file"), 0o600))

	err := s.service.WriteMessage(
		s.T().Context(),
		filepath.Join(parentPath, "message.md"),
		domain.MessageFileModeCreate,
		"payload",
	)
	s.Require().Error(err)
}

// TestRejectsUnsupportedMode verifies the adapter enforces the closed write-mode contract.
func (s *serviceSuite) TestRejectsUnsupportedMode() {
	err := s.service.WriteMessage(
		s.T().Context(),
		filepath.Join(s.homeDir, "message.md"),
		domain.MessageFileMode("append"),
		"payload",
	)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "mode must be create or overwrite")
}
