package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/n-r-w/team-mcp/internal/domain"
	"github.com/n-r-w/team-mcp/internal/usecase"
)

var _ usecase.IBoardStore = (*BoardStore)(nil)

// BoardStore persists authoritative board state in the filesystem for future runtime handoff.
type BoardStore struct {
	rootDir string
}

// NewBoardStore constructs the authoritative disk-backed board store.
func NewBoardStore(rootDir string) (*BoardStore, error) {
	cleanRootDir, err := normalizeRootDir(rootDir)
	if err != nil {
		return nil, err
	}

	return &BoardStore{rootDir: cleanRootDir}, nil
}

// ValidateRuntimeMessageDir rejects reused non-empty directories so runtime never
// starts from mixed storage generations.
func ValidateRuntimeMessageDir(rootDir string) error {
	cleanRootDir, err := normalizeRootDir(rootDir)
	if err != nil {
		return err
	}

	rootInfo, err := os.Stat(cleanRootDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return err
	}

	if !rootInfo.IsDir() {
		return errors.New("message directory must point to a directory")
	}

	entries, err := os.ReadDir(cleanRootDir)
	if err != nil {
		return err
	}

	if len(entries) > 0 {
		return errors.New("message directory must be missing or empty before runtime startup")
	}

	return nil
}

// CreateDesk persists a new desk and its creation timestamp.
func (s *BoardStore) CreateDesk(ctx context.Context, createdAt time.Time) (string, error) {
	if err := s.ensureRootDirs(); err != nil {
		return "", err
	}

	deskID := uuid.NewString()
	if err := s.ensureDeskDirs(deskID); err != nil {
		return "", err
	}

	state := boardDeskState{
		Version:      1,
		CreatedAt:    createdAt.UTC().UnixNano(),
		Topics:       []domain.TopicHeader{},
		TopicByTitle: map[string]string{},
	}

	if err := s.writeVersionSnapshot(s.deskVersionsDir(deskID), state.Version, state); err != nil {
		return "", err
	}

	s.refreshDeskMirror(ctx, deskID, state)
	s.cleanupOlderVersionSnapshots(ctx, s.deskVersionsDir(deskID), state.Version)

	return deskID, nil
}

// CreateTopic persists a topic in desk order and dedupes by exact title.
func (s *BoardStore) CreateTopic(
	ctx context.Context,
	deskID string,
	title string,
) (domain.TopicHeader, domain.BusinessStatus, bool, error) {
	if err := validateLocalID("desk ID", deskID); err != nil {
		return emptyTopicHeader(), "", false, err
	}

	if err := s.ensureRootDirs(); err != nil {
		return emptyTopicHeader(), "", false, err
	}

	for range boardMaxVersionRetries {
		deskState, found, err := s.loadDeskState(deskID)
		if err != nil {
			return emptyTopicHeader(), "", false, err
		}

		if !found {
			return emptyTopicHeader(), domain.BusinessStatusNotFound, false, nil
		}

		if existingTopicID, exists := deskState.TopicByTitle[title]; exists {
			return domain.TopicHeader{TopicID: existingTopicID, Title: title}, domain.BusinessStatusOK, false, nil
		}

		topicID := uuid.NewString()
		header := domain.TopicHeader{TopicID: topicID, Title: title}
		initialTopicState := boardTopicState{
			Version:                  1,
			Messages:                 []domain.MessageHeader{},
			MessageByNormalizedTitle: map[string]string{},
		}

		writeErr := s.writeTopicScaffold(ctx, deskID, topicID, initialTopicState)
		if writeErr != nil {
			if errors.Is(writeErr, os.ErrNotExist) {
				return emptyTopicHeader(), domain.BusinessStatusNotFound, false, nil
			}

			return emptyTopicHeader(), "", false, writeErr
		}

		nextDeskState := cloneDeskState(deskState)
		nextDeskState.Version = deskState.Version + 1
		nextDeskState.Topics = append(nextDeskState.Topics, header)
		nextDeskState.TopicByTitle[title] = topicID

		commitErr := s.commitDeskState(ctx, deskID, nextDeskState)
		if errors.Is(commitErr, errBoardVersionConflict) {
			_ = s.cleanupTopicArtifacts(deskID, topicID)

			continue
		}

		if commitErr != nil {
			_ = s.cleanupTopicArtifacts(deskID, topicID)
			if errors.Is(commitErr, os.ErrNotExist) {
				return emptyTopicHeader(), domain.BusinessStatusNotFound, false, nil
			}

			return emptyTopicHeader(), "", false, commitErr
		}

		return header, domain.BusinessStatusOK, true, nil
	}

	return emptyTopicHeader(), "", false,
		fmt.Errorf("create topic exceeded %d version retries", boardMaxVersionRetries)
}

// ListTopics returns topics in persisted insertion order.
func (s *BoardStore) ListTopics(_ context.Context, deskID string) ([]domain.TopicHeader, bool, error) {
	if err := validateLocalID("desk ID", deskID); err != nil {
		return nil, false, err
	}

	deskState, found, err := s.loadDeskState(deskID)
	if err != nil {
		return nil, false, err
	}

	if !found {
		return nil, false, nil
	}

	return copyTopicHeaders(deskState.Topics), true, nil
}

// CreateMessage persists payload, lookup, ordering, and normalized-title dedupe state.
func (s *BoardStore) CreateMessage(
	ctx context.Context,
	topicID string,
	title string,
	normalizedTitle string,
	payload string,
) (domain.MessageMeta, domain.BusinessStatus, string, error) {
	if err := validateLocalID("topic ID", topicID); err != nil {
		return emptyMessageMeta(), "", "", err
	}

	for range boardMaxVersionRetries {
		deskID, topicState, found, err := s.loadVisibleTopicState(topicID)
		if err != nil {
			return emptyMessageMeta(), "", "", err
		}

		if !found {
			return emptyMessageMeta(), domain.BusinessStatusNotFound, "", nil
		}

		if existingMessageID, exists := topicState.MessageByNormalizedTitle[normalizedTitle]; exists {
			return emptyMessageMeta(), domain.BusinessStatusDuplicateTitle, existingMessageID, nil
		}

		messageID := uuid.NewString()
		writeErr := s.writeMessageArtifacts(deskID, topicID, messageID, payload)
		if writeErr != nil {
			if errors.Is(writeErr, os.ErrNotExist) {
				return emptyMessageMeta(), domain.BusinessStatusNotFound, "", nil
			}

			return emptyMessageMeta(), "", "", writeErr
		}

		nextTopicState := cloneTopicState(topicState)
		nextTopicState.Version = topicState.Version + 1
		nextTopicState.Messages = append(nextTopicState.Messages, domain.MessageHeader{MessageID: messageID, Title: title})
		nextTopicState.MessageByNormalizedTitle[normalizedTitle] = messageID

		commitErr := s.commitTopicState(ctx, deskID, topicID, nextTopicState)
		if errors.Is(commitErr, errBoardVersionConflict) {
			_ = s.cleanupMessageArtifacts(deskID, messageID)

			continue
		}

		if commitErr != nil {
			_ = s.cleanupMessageArtifacts(deskID, messageID)
			if errors.Is(commitErr, os.ErrNotExist) {
				return emptyMessageMeta(), domain.BusinessStatusNotFound, "", nil
			}

			return emptyMessageMeta(), "", "", commitErr
		}

		return domain.MessageMeta{
			MessageID: messageID,
			TopicID:   topicID,
			DeskID:    deskID,
			Title:     title,
		}, domain.BusinessStatusOK, "", nil
	}

	return emptyMessageMeta(), "", "",
		fmt.Errorf("create message exceeded %d version retries", boardMaxVersionRetries)
}

// ListMessages returns messages in persisted insertion order.
func (s *BoardStore) ListMessages(_ context.Context, topicID string) ([]domain.MessageHeader, bool, error) {
	if err := validateLocalID("topic ID", topicID); err != nil {
		return nil, false, err
	}

	_, topicState, found, err := s.loadVisibleTopicState(topicID)
	if err != nil {
		return nil, false, err
	}

	if !found {
		return nil, false, nil
	}

	return copyMessageHeaders(topicState.Messages), true, nil
}

// GetMessage resolves persisted metadata and payload by message identifier.
func (s *BoardStore) GetMessage(_ context.Context, messageID string) (
	meta domain.MessageMeta,
	content string,
	found bool,
	err error,
) {
	validateErr := validateLocalID("message ID", messageID)
	if validateErr != nil {
		return emptyMessageMeta(), "", false, validateErr
	}

	lookup, found, err := s.loadMessageLookup(messageID)
	if err != nil {
		return emptyMessageMeta(), "", false, err
	}

	if !found {
		return emptyMessageMeta(), "", false, nil
	}

	deskState, found, err := s.loadDeskState(lookup.DeskID)
	if err != nil {
		return emptyMessageMeta(), "", false, err
	}

	if !found || !deskContainsTopic(deskState, lookup.TopicID) {
		return emptyMessageMeta(), "", false, nil
	}

	topicState, found, err := s.loadTopicState(lookup.DeskID, lookup.TopicID)
	if err != nil {
		return emptyMessageMeta(), "", false, err
	}

	if !found {
		return emptyMessageMeta(), "", false, nil
	}

	header, found := findMessageHeader(topicState.Messages, messageID)
	if !found {
		return emptyMessageMeta(), "", false, nil
	}

	payload, err := readFile(s.messagePayloadPath(lookup.DeskID, messageID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return emptyMessageMeta(), "", false, nil
		}

		return emptyMessageMeta(), "", false, err
	}

	return domain.MessageMeta{
		MessageID: messageID,
		TopicID:   lookup.TopicID,
		DeskID:    lookup.DeskID,
		Title:     header.Title,
	}, string(payload), true, nil
}

// DeleteDesk removes desk data and treats missing desks as already deleted.
func (s *BoardStore) DeleteDesk(_ context.Context, deskID string) error {
	if err := validateLocalID("desk ID", deskID); err != nil {
		return err
	}

	deskState, found, err := s.loadDeskState(deskID)
	if err != nil {
		found = false
	}

	messageIDsByTopicID := make(map[string][]string)
	if found {
		for _, topicHeader := range deskState.Topics {
			topicState, topicFound, topicErr := s.loadTopicState(deskID, topicHeader.TopicID)
			if topicErr != nil || !topicFound {
				continue
			}

			messageIDs := make([]string, 0, len(topicState.Messages))
			for _, messageHeader := range topicState.Messages {
				messageIDs = append(messageIDs, messageHeader.MessageID)
			}
			messageIDsByTopicID[topicHeader.TopicID] = messageIDs
		}
	}

	removeDeskErr := os.RemoveAll(s.deskDir(deskID))
	if removeDeskErr != nil {
		return removeDeskErr
	}

	var cleanupErr error
	for topicID, messageIDs := range messageIDsByTopicID {
		removeTopicLookupErr := removeIfExists(s.topicLookupPath(topicID))
		if removeTopicLookupErr != nil {
			cleanupErr = errors.Join(cleanupErr, removeTopicLookupErr)
		}

		for _, messageID := range messageIDs {
			removeMessageLookupErr := removeIfExists(s.messageLookupPath(messageID))
			if removeMessageLookupErr != nil {
				cleanupErr = errors.Join(cleanupErr, removeMessageLookupErr)
			}
		}
	}

	return cleanupErr
}

// CollectExpiredDeskIDs resolves desk expiry from persisted metadata only.
func (s *BoardStore) CollectExpiredDeskIDs(_ context.Context, now time.Time, ttl time.Duration) ([]string, error) {
	entries, err := os.ReadDir(s.rootDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}

		return nil, err
	}

	nowUTC := now.UTC()
	expiredDeskIDs := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() || isReservedBoardDir(entry.Name()) {
			continue
		}

		deskState, found, loadErr := s.loadDeskState(entry.Name())
		if loadErr != nil {
			slog.Warn(
				"desk metadata is invalid, marking desk as expired",
				"desk_id",
				entry.Name(),
				"error",
				loadErr,
			)
			expiredDeskIDs = append(expiredDeskIDs, entry.Name())

			continue
		}

		if !found {
			expiredDeskIDs = append(expiredDeskIDs, entry.Name())

			continue
		}

		createdAt := time.Unix(0, deskState.CreatedAt).UTC()
		if createdAt.Add(ttl).After(nowUTC) {
			continue
		}

		expiredDeskIDs = append(expiredDeskIDs, entry.Name())
	}

	return expiredDeskIDs, nil
}

// topicVersionsDir returns the absolute path of the topic version directory for failure-injection tests.
func (s *BoardStore) topicVersionsDir(deskID, topicID string) string {
	return filepath.Join(s.rootDir, deskID, boardTopicsDirName, topicID+boardVersionsSuffix)
}

// deskDir returns the absolute desk directory path under the shared root.
func (s *BoardStore) deskDir(deskID string) string {
	return filepath.Join(s.rootDir, deskID)
}

// deskStatePath returns the mirrored latest desk metadata file path.
func (s *BoardStore) deskStatePath(deskID string) string {
	return filepath.Join(s.deskDir(deskID), boardDeskStateFileName)
}

// deskVersionsDir returns the authoritative desk version snapshot directory.
func (s *BoardStore) deskVersionsDir(deskID string) string {
	return filepath.Join(s.deskDir(deskID), boardVersionsDirName)
}

// topicsDir returns the per-desk directory that stores topic metadata files.
func (s *BoardStore) topicsDir(deskID string) string {
	return filepath.Join(s.deskDir(deskID), boardTopicsDirName)
}

// topicStatePath returns the mirrored latest topic metadata file path.
func (s *BoardStore) topicStatePath(deskID, topicID string) string {
	return filepath.Join(s.topicsDir(deskID), topicID+boardJSONExtension)
}

// topicLookupPath returns the direct topic-to-desk lookup file path.
func (s *BoardStore) topicLookupPath(topicID string) string {
	return filepath.Join(s.rootDir, boardTopicLookupDirName, topicID+boardJSONExtension)
}

// messageLookupPath returns the direct message-to-owner lookup file path.
func (s *BoardStore) messageLookupPath(messageID string) string {
	return filepath.Join(s.rootDir, boardMessageLookupDirName, messageID+boardJSONExtension)
}

// messagePayloadPath returns the immutable markdown payload path for a message.
func (s *BoardStore) messagePayloadPath(deskID, messageID string) string {
	return filepath.Join(s.deskDir(deskID), fileForMessageID(messageID))
}

// ensureRootDirs prepares shared lookup directories used by direct message and topic resolution.
func (s *BoardStore) ensureRootDirs() error {
	return ensureDirectories(
		s.rootDir,
		filepath.Join(s.rootDir, boardTopicLookupDirName),
		filepath.Join(s.rootDir, boardMessageLookupDirName),
	)
}

// ensureDeskDirs prepares per-desk directories that hold authoritative metadata snapshots.
func (s *BoardStore) ensureDeskDirs(deskID string) error {
	if err := validateLocalID("desk ID", deskID); err != nil {
		return err
	}

	return ensureDirectories(s.deskDir(deskID), s.deskVersionsDir(deskID), s.topicsDir(deskID))
}

// writeTopicScaffold prepares hidden topic metadata and lookup records before desk metadata makes the topic visible.
func (s *BoardStore) writeTopicScaffold(ctx context.Context, deskID, topicID string, topicState boardTopicState) error {
	if err := ensureDirectories(s.topicVersionsDir(deskID, topicID)); err != nil {
		return err
	}

	if err := s.writeVersionSnapshot(s.topicVersionsDir(deskID, topicID), topicState.Version, topicState); err != nil {
		return err
	}

	s.refreshTopicMirror(ctx, deskID, topicID, topicState)
	if err := writeJSONExclusive(s.topicLookupPath(topicID), boardTopicLookup{DeskID: deskID}); err != nil {
		return err
	}

	s.cleanupOlderVersionSnapshots(ctx, s.topicVersionsDir(deskID, topicID), topicState.Version)

	return nil
}

// writeMessageArtifacts prepares hidden payload and lookup files before topic metadata makes the message visible.
func (s *BoardStore) writeMessageArtifacts(deskID, topicID, messageID, payload string) error {
	if err := writeBytesExclusive(s.messagePayloadPath(deskID, messageID), []byte(payload)); err != nil {
		return err
	}

	lookup := boardMessageLookup{DeskID: deskID, TopicID: topicID}
	if err := writeJSONExclusive(s.messageLookupPath(messageID), lookup); err != nil {
		_ = s.cleanupMessageArtifacts(deskID, messageID)

		return err
	}

	return nil
}

// commitDeskState publishes the next authoritative desk snapshot and refreshes its mutable mirror.
func (s *BoardStore) commitDeskState(ctx context.Context, deskID string, deskState boardDeskState) error {
	if err := s.writeVersionSnapshot(s.deskVersionsDir(deskID), deskState.Version, deskState); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errBoardVersionConflict
		}

		return err
	}

	s.refreshDeskMirror(ctx, deskID, deskState)
	s.cleanupOlderVersionSnapshots(ctx, s.deskVersionsDir(deskID), deskState.Version)

	return nil
}

// commitTopicState publishes the next authoritative topic snapshot and refreshes its mutable mirror.
func (s *BoardStore) commitTopicState(ctx context.Context, deskID, topicID string, topicState boardTopicState) error {
	if err := s.writeVersionSnapshot(s.topicVersionsDir(deskID, topicID), topicState.Version, topicState); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errBoardVersionConflict
		}

		return err
	}

	s.refreshTopicMirror(ctx, deskID, topicID, topicState)
	s.cleanupOlderVersionSnapshots(ctx, s.topicVersionsDir(deskID, topicID), topicState.Version)

	return nil
}

// refreshDeskMirror rewrites the human-readable desk metadata mirror without affecting the authoritative snapshot.
func (s *BoardStore) refreshDeskMirror(ctx context.Context, deskID string, deskState boardDeskState) {
	if err := writeJSONAtomically(s.deskStatePath(deskID), deskState); err != nil {
		slog.WarnContext(
			ctx,
			"failed to refresh desk metadata mirror",
			"desk_id",
			deskID,
			"version",
			deskState.Version,
			"error",
			err,
		)
	}
}

// refreshTopicMirror rewrites the human-readable topic metadata mirror without affecting the authoritative snapshot.
func (s *BoardStore) refreshTopicMirror(ctx context.Context, deskID, topicID string, topicState boardTopicState) {
	if err := writeJSONAtomically(s.topicStatePath(deskID, topicID), topicState); err != nil {
		slog.WarnContext(
			ctx,
			"failed to refresh topic metadata mirror",
			"desk_id",
			deskID,
			"topic_id",
			topicID,
			"version",
			topicState.Version,
			"error",
			err,
		)
	}
}

// cleanupOlderVersionSnapshots keeps only the newest authoritative snapshot so retry checks stay cheap.
func (s *BoardStore) cleanupOlderVersionSnapshots(ctx context.Context, versionsDir string, version int64) {
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.WarnContext(ctx, "failed to list version snapshots", "versions_dir", versionsDir, "error", err)
		}

		return
	}

	keepFileName := boardVersionFileName(version)
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == keepFileName {
			continue
		}

		removeSnapshotErr := removeIfExists(filepath.Join(versionsDir, entry.Name()))
		if removeSnapshotErr != nil {
			slog.WarnContext(
				ctx,
				"failed to prune stale version snapshot",
				"versions_dir",
				versionsDir,
				"snapshot",
				entry.Name(),
				"error",
				removeSnapshotErr,
			)
		}
	}
}

// cleanupTopicArtifacts removes precommitted topic files after a failed or conflicted write attempt.
func (s *BoardStore) cleanupTopicArtifacts(deskID, topicID string) error {
	return errors.Join(
		removeIfExists(s.topicLookupPath(topicID)),
		removeIfExists(s.topicStatePath(deskID, topicID)),
		os.RemoveAll(s.topicVersionsDir(deskID, topicID)),
	)
}

// cleanupMessageArtifacts removes precommitted payload and lookup files after a failed or conflicted write attempt.
func (s *BoardStore) cleanupMessageArtifacts(deskID, messageID string) error {
	return errors.Join(
		removeIfExists(s.messageLookupPath(messageID)),
		removeIfExists(s.messagePayloadPath(deskID, messageID)),
	)
}

// loadDeskState resolves the newest persisted desk snapshot.
// A higher committed version wins over the mirrored state file.
func (s *BoardStore) loadDeskState(deskID string) (boardDeskState, bool, error) {
	return loadLatestState[boardDeskState](
		s.deskStatePath(deskID),
		s.deskVersionsDir(deskID),
		func(state boardDeskState) int64 { return state.Version },
	)
}

// loadTopicState resolves the newest persisted topic snapshot.
// A higher committed version wins over the mirrored state file.
func (s *BoardStore) loadTopicState(deskID, topicID string) (boardTopicState, bool, error) {
	return loadLatestState[boardTopicState](
		s.topicStatePath(deskID, topicID),
		s.topicVersionsDir(deskID, topicID),
		func(state boardTopicState) int64 { return state.Version },
	)
}

// loadVisibleTopicState resolves a topic only when both lookup and desk metadata agree that it is committed.
func (s *BoardStore) loadVisibleTopicState(
	topicID string,
) (deskID string, state boardTopicState, found bool, err error) {
	lookup, found, err := s.loadTopicLookup(topicID)
	if err != nil {
		return "", emptyBoardTopicState(), false, err
	}

	if !found {
		return "", emptyBoardTopicState(), false, nil
	}

	deskState, found, err := s.loadDeskState(lookup.DeskID)
	if err != nil {
		return "", emptyBoardTopicState(), false, err
	}

	if !found || !deskContainsTopic(deskState, topicID) {
		return "", emptyBoardTopicState(), false, nil
	}

	topicState, found, err := s.loadTopicState(lookup.DeskID, topicID)
	if err != nil {
		return "", emptyBoardTopicState(), false, err
	}

	if !found {
		return "", emptyBoardTopicState(), false, nil
	}

	return lookup.DeskID, topicState, true, nil
}

// loadTopicLookup reads the direct topic-to-desk lookup file.
func (s *BoardStore) loadTopicLookup(topicID string) (boardTopicLookup, bool, error) {
	return readJSONFile[boardTopicLookup](s.topicLookupPath(topicID))
}

// loadMessageLookup reads the direct message ownership lookup file.
func (s *BoardStore) loadMessageLookup(messageID string) (boardMessageLookup, bool, error) {
	return readJSONFile[boardMessageLookup](s.messageLookupPath(messageID))
}

// writeVersionSnapshot claims the next committed version by creating its snapshot file exactly once.
func (s *BoardStore) writeVersionSnapshot(versionsDir string, version int64, value any) error {
	return writeJSONExclusive(filepath.Join(versionsDir, boardVersionFileName(version)), value)
}

// cloneDeskState protects loaded desk metadata from accidental in-place mutation across retries.
func cloneDeskState(state boardDeskState) boardDeskState {
	return boardDeskState{
		Version:      state.Version,
		CreatedAt:    state.CreatedAt,
		Topics:       copyTopicHeaders(state.Topics),
		TopicByTitle: copyStringMap(state.TopicByTitle),
	}
}

// cloneTopicState protects loaded topic metadata from accidental in-place mutation across retries.
func cloneTopicState(state boardTopicState) boardTopicState {
	return boardTopicState{
		Version:                  state.Version,
		Messages:                 copyMessageHeaders(state.Messages),
		MessageByNormalizedTitle: copyStringMap(state.MessageByNormalizedTitle),
	}
}

// copyTopicHeaders returns a defensive copy so callers cannot mutate authoritative ordering state.
func copyTopicHeaders(headers []domain.TopicHeader) []domain.TopicHeader {
	if len(headers) == 0 {
		return []domain.TopicHeader{}
	}

	copiedHeaders := make([]domain.TopicHeader, len(headers))
	copy(copiedHeaders, headers)

	return copiedHeaders
}

// copyMessageHeaders returns a defensive copy so callers cannot mutate authoritative ordering state.
func copyMessageHeaders(headers []domain.MessageHeader) []domain.MessageHeader {
	if len(headers) == 0 {
		return []domain.MessageHeader{}
	}

	copiedHeaders := make([]domain.MessageHeader, len(headers))
	copy(copiedHeaders, headers)

	return copiedHeaders
}

// copyStringMap returns a mutable copy because retry loops must update dedupe indexes in isolation.
func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}

	copiedValues := make(map[string]string, len(values))
	maps.Copy(copiedValues, values)

	return copiedValues
}

// deskContainsTopic checks desk visibility before a topic becomes readable by topic ID alone.
func deskContainsTopic(state boardDeskState, topicID string) bool {
	for _, topicHeader := range state.Topics {
		if topicHeader.TopicID == topicID {
			return true
		}
	}

	return false
}

// findMessageHeader resolves the committed message title from ordered topic metadata.
func findMessageHeader(headers []domain.MessageHeader, messageID string) (domain.MessageHeader, bool) {
	for _, header := range headers {
		if header.MessageID == messageID {
			return header, true
		}
	}

	return emptyMessageHeader(), false
}

// ensureDirectories creates all requested directories so later file operations can stay atomic and simple.
func ensureDirectories(paths ...string) error {
	for _, path := range paths {
		if err := os.MkdirAll(path, directoryPermission); err != nil {
			return err
		}
	}

	return nil
}

// removeIfExists removes one file path while treating missing entries as already cleaned up.
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

// loadLatestState reads the mirrored metadata file and any committed snapshots, then returns the newest valid version.
func loadLatestState[T any](
	currentPath, versionsDir string,
	versionOf func(T) int64,
) (value T, found bool, err error) {
	currentState, currentFound, currentErr := readJSONFile[T](currentPath)
	versionState, versionFound, versionErr := readLatestVersionFile[T](versionsDir)
	if versionErr != nil {
		return value, false, versionErr
	}

	if currentErr != nil {
		if versionFound {
			return versionState, true, nil
		}

		return value, false, currentErr
	}

	if versionFound && (!currentFound || versionOf(versionState) >= versionOf(currentState)) {
		return versionState, true, nil
	}

	return currentState, currentFound, nil
}

// readLatestVersionFile reads the highest-numbered committed snapshot from one version directory.
func readLatestVersionFile[T any](versionsDir string) (value T, found bool, err error) {
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return value, false, nil
		}

		return value, false, err
	}

	latestSnapshotFileName := ""
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != boardJSONExtension {
			continue
		}

		if entry.Name() > latestSnapshotFileName {
			latestSnapshotFileName = entry.Name()
		}
	}

	if latestSnapshotFileName == "" {
		return value, false, nil
	}

	return readJSONFile[T](filepath.Join(versionsDir, latestSnapshotFileName))
}

// readJSONFile loads one JSON file and reports missing paths without turning them into errors.
func readJSONFile[T any](path string) (value T, found bool, err error) {
	payload, err := readFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return value, false, nil
		}

		return value, false, err
	}

	unmarshalErr := json.Unmarshal(payload, &value)
	if unmarshalErr != nil {
		return value, false, unmarshalErr
	}

	return value, true, nil
}

// writeJSONExclusive persists a brand-new file and fails if another writer already created it.
func writeJSONExclusive(path string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return writeBytesExclusive(path, payload)
}

// writeBytesExclusive writes one immutable file used for snapshots, payloads, and direct lookup records.
func writeBytesExclusive(path string, payload []byte) error {
	if err := ensureDirectories(filepath.Dir(path)); err != nil {
		return err
	}

	file, err := openFileExclusive(path, filePermission)
	if err != nil {
		return err
	}

	_, writeErr := file.Write(payload)
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)

		return writeErr
	}

	if closeErr != nil {
		_ = os.Remove(path)

		return closeErr
	}

	return nil
}

// writeJSONAtomically refreshes the mutable mirror file through rename so readers never see torn JSON.
func writeJSONAtomically(path string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return writeBytesAtomically(path, payload)
}

// writeBytesAtomically writes a temporary file and swaps it into place in one rename step.
func writeBytesAtomically(path string, payload []byte) error {
	if err := ensureDirectories(filepath.Dir(path)); err != nil {
		return err
	}

	tempPath := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-"+uuid.NewString())
	if err := os.WriteFile(tempPath, payload, filePermission); err != nil {
		return err
	}

	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)

		return err
	}

	return nil
}

// readFile loads a file via os.Root so lint can see the path stays inside a controlled directory.
func readFile(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	payload, readErr := root.ReadFile(filepath.Base(path))
	closeErr := root.Close()
	if readErr != nil {
		return nil, readErr
	}

	if closeErr != nil {
		return nil, closeErr
	}

	return payload, nil
}

// openFileExclusive creates one new file inside a controlled directory and rejects overwrites.
func openFileExclusive(path string, mode os.FileMode) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	file, openErr := root.OpenFile(filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	closeErr := root.Close()
	if openErr != nil {
		return nil, openErr
	}

	if closeErr != nil {
		_ = file.Close()

		return nil, closeErr
	}

	return file, nil
}

// boardVersionFileName keeps snapshot ordering lexicographically sortable for cheap latest-version resolution.
func boardVersionFileName(version int64) string {
	return fmt.Sprintf("%020d%s", version, boardJSONExtension)
}

// isReservedBoardDir filters lookup directories out of desk TTL scans.
func isReservedBoardDir(name string) bool {
	return name == boardTopicLookupDirName || name == boardMessageLookupDirName
}

// emptyTopicHeader keeps not-found paths explicit and lint-clean.
func emptyTopicHeader() domain.TopicHeader {
	return domain.TopicHeader{TopicID: "", Title: ""}
}

// emptyMessageMeta keeps not-found paths explicit and lint-clean.
func emptyMessageMeta() domain.MessageMeta {
	return domain.MessageMeta{MessageID: "", TopicID: "", DeskID: "", Title: ""}
}

// emptyBoardTopicState keeps unresolved topic paths explicit and lint-clean.
func emptyBoardTopicState() boardTopicState {
	return boardTopicState{Version: 0, Messages: nil, MessageByNormalizedTitle: nil}
}

// emptyMessageHeader keeps unresolved message-header paths explicit and lint-clean.
func emptyMessageHeader() domain.MessageHeader {
	return domain.MessageHeader{MessageID: "", Title: ""}
}

// normalizeRootDir validates and cleans the shared storage root before any filesystem operations begin.
func normalizeRootDir(rootDir string) (string, error) {
	cleanRootDir := filepath.Clean(strings.TrimSpace(rootDir))
	if cleanRootDir == "." {
		return "", errors.New("invalid root directory: must not be empty")
	}

	if !filepath.IsAbs(cleanRootDir) {
		return "", errors.New("invalid root directory: must be absolute path")
	}

	return cleanRootDir, nil
}
