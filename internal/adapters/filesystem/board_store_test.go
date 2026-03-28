package filesystem

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/team-mcp/internal/domain"
)

// TestBoardStoreCreateDeskPersistsCreatedAt verifies desk creation timestamp survives reopen and drives TTL scanning.
func TestBoardStoreCreateDeskPersistsCreatedAt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, rootDir := newBoardStoreForTest(t)
	createdAt := time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC)

	deskID, err := store.CreateDesk(ctx, createdAt)
	require.NoError(t, err)
	require.NotEmpty(t, deskID)

	reopenedStore, err := NewBoardStore(rootDir)
	require.NoError(t, err)

	expiredDeskIDs, err := reopenedStore.CollectExpiredDeskIDs(ctx, createdAt.Add(2*time.Hour), time.Hour)
	require.NoError(t, err)
	require.Equal(t, []string{deskID}, expiredDeskIDs)
}

// TestValidateRuntimeMessageDirAllowsExistingBoardState verifies runtime startup accepts a directory that already contains Team MCP state.
func TestValidateRuntimeMessageDirAllowsExistingBoardState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, rootDir := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())
	topicID := mustCreateTopic(t, store, deskID, "Topic")
	_, status, existingMessageID, err := store.CreateMessage(
		ctx,
		topicID,
		"Message",
		normalizeTitleForTest("Message"),
		"# payload",
	)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, status)
	require.Empty(t, existingMessageID)

	err = ValidateRuntimeMessageDir(rootDir)
	require.NoError(t, err)
}

// TestBoardStoreCreateTopicPersistsOrderAndDedupe verifies desk metadata owns topic titles and order on disk.
func TestBoardStoreCreateTopicPersistsOrderAndDedupe(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, rootDir := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())

	firstHeader, firstStatus, firstCreated, err := store.CreateTopic(ctx, deskID, "Alpha")
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, firstStatus)
	require.True(t, firstCreated)

	secondHeader, secondStatus, secondCreated, err := store.CreateTopic(ctx, deskID, "Beta")
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, secondStatus)
	require.True(t, secondCreated)

	duplicateHeader, duplicateStatus, duplicateCreated, err := store.CreateTopic(ctx, deskID, "Alpha")
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, duplicateStatus)
	require.False(t, duplicateCreated)
	require.Equal(t, firstHeader.TopicID, duplicateHeader.TopicID)

	reopenedStore, err := NewBoardStore(rootDir)
	require.NoError(t, err)

	topics, found, err := reopenedStore.ListTopics(ctx, deskID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []domain.TopicHeader{firstHeader, secondHeader}, topics)
}

// TestBoardStoreCreateMessagePersistsOrderAndNormalizedTitleDedupe verifies topic metadata owns message order and dedupe on disk.
func TestBoardStoreCreateMessagePersistsOrderAndNormalizedTitleDedupe(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, rootDir := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())
	topicID := mustCreateTopic(t, store, deskID, "Topic")

	firstMeta, firstStatus, firstExistingMessageID, err := store.CreateMessage(
		ctx,
		topicID,
		"Title One",
		normalizeTitleForTest("Title One"),
		"# first",
	)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, firstStatus)
	require.Empty(t, firstExistingMessageID)

	secondMeta, secondStatus, secondExistingMessageID, err := store.CreateMessage(
		ctx,
		topicID,
		"Title Two",
		normalizeTitleForTest("Title Two"),
		"# second",
	)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, secondStatus)
	require.Empty(t, secondExistingMessageID)

	_, duplicateStatus, existingMessageID, err := store.CreateMessage(
		ctx,
		topicID,
		"TITLE   ONE",
		normalizeTitleForTest("TITLE   ONE"),
		"# duplicate",
	)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusDuplicateTitle, duplicateStatus)
	require.Equal(t, firstMeta.MessageID, existingMessageID)

	reopenedStore, err := NewBoardStore(rootDir)
	require.NoError(t, err)

	messages, found, err := reopenedStore.ListMessages(ctx, topicID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []domain.MessageHeader{
		{MessageID: firstMeta.MessageID, Title: "Title One"},
		{MessageID: secondMeta.MessageID, Title: "Title Two"},
	}, messages)
}

// TestBoardStoreGetMessageUsesDirectLookup verifies message_get prerequisites are available through direct lookup files on disk.
func TestBoardStoreGetMessageUsesDirectLookup(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, rootDir := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())
	topicID := mustCreateTopic(t, store, deskID, "Topic")
	messageMeta, status, existingMessageID, err := store.CreateMessage(
		ctx,
		topicID,
		"Lookup Title",
		normalizeTitleForTest("Lookup Title"),
		"# lookup payload",
	)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, status)
	require.Empty(t, existingMessageID)

	reopenedStore, err := NewBoardStore(rootDir)
	require.NoError(t, err)

	meta, content, found, err := reopenedStore.GetMessage(ctx, messageMeta.MessageID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, messageMeta, meta)
	require.Equal(t, "# lookup payload", content)
}

// TestBoardStoreListMessagesFallsBackToCommittedSnapshotWhenTopicMirrorIsCorrupted verifies committed snapshots stay authoritative when the readable topic mirror is broken.
func TestBoardStoreListMessagesFallsBackToCommittedSnapshotWhenTopicMirrorIsCorrupted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, rootDir := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())
	topicID := mustCreateTopic(t, store, deskID, "Topic")
	messageMeta, status, existingMessageID, err := store.CreateMessage(
		ctx,
		topicID,
		"Snapshot Title",
		normalizeTitleForTest("Snapshot Title"),
		"# snapshot payload",
	)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, status)
	require.Empty(t, existingMessageID)

	topicStatePath := store.topicStatePath(deskID, topicID)
	require.NoError(t, os.WriteFile(topicStatePath, []byte("{broken-json"), filePermission))

	reopenedStore, err := NewBoardStore(rootDir)
	require.NoError(t, err)

	messages, found, err := reopenedStore.ListMessages(ctx, topicID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []domain.MessageHeader{{MessageID: messageMeta.MessageID, Title: "Snapshot Title"}}, messages)
}

// TestBoardStoreCreateMessageFailureDoesNotExposeHalfCreatedMessage verifies failed writes do not leak visible list or lookup state.
func TestBoardStoreCreateMessageFailureDoesNotExposeHalfCreatedMessage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, rootDir := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())
	topicID := mustCreateTopic(t, store, deskID, "Topic")

	messageLookupDir := filepath.Join(rootDir, boardMessageLookupDirName)
	require.NoError(t, os.RemoveAll(messageLookupDir))
	require.NoError(t, os.WriteFile(messageLookupDir, []byte("blocked"), filePermission))
	t.Cleanup(func() {
		_ = os.Remove(messageLookupDir)
		_ = os.Mkdir(messageLookupDir, directoryPermission)
	})

	_, _, _, err := store.CreateMessage(
		ctx,
		topicID,
		"Will Fail",
		normalizeTitleForTest("Will Fail"),
		"# payload",
	)
	require.Error(t, err)

	require.NoError(t, os.Remove(messageLookupDir))
	require.NoError(t, os.Mkdir(messageLookupDir, directoryPermission))

	reopenedStore, err := NewBoardStore(rootDir)
	require.NoError(t, err)

	messages, found, err := reopenedStore.ListMessages(ctx, topicID)
	require.NoError(t, err)
	require.True(t, found)
	require.Empty(t, messages)

	lookupEntries, readDirErr := os.ReadDir(filepath.Join(rootDir, boardMessageLookupDirName))
	require.NoError(t, readDirErr)
	require.Empty(t, lookupEntries)

	deskEntries, readDeskDirErr := os.ReadDir(filepath.Join(rootDir, deskID))
	require.NoError(t, readDeskDirErr)
	for _, entry := range deskEntries {
		require.NotEqual(t, markdownExtension, filepath.Ext(entry.Name()))
	}
}

// TestBoardStoreResolveCreateMessageMutationErrorKeepsWriteFailureVisible verifies os.ErrNotExist is not reclassified while the topic is still readable.
func TestBoardStoreResolveCreateMessageMutationErrorKeepsWriteFailureVisible(t *testing.T) {
	t.Parallel()

	store, _ := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())
	topicID := mustCreateTopic(t, store, deskID, "Topic")

	status, err := store.resolveCreateMessageMutationError(topicID, os.ErrNotExist)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Empty(t, status)
}

// TestBoardStoreResolveCreateMessageMutationErrorReturnsNotFound verifies os.ErrNotExist maps to not-found only after the topic disappears.
func TestBoardStoreResolveCreateMessageMutationErrorReturnsNotFound(t *testing.T) {
	t.Parallel()

	store, _ := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())
	topicID := mustCreateTopic(t, store, deskID, "Topic")
	require.NoError(t, store.DeleteDesk(t.Context(), deskID))

	status, err := store.resolveCreateMessageMutationError(topicID, os.ErrNotExist)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusNotFound, status)
}

// TestBoardStoreResolveCreateTopicMutationErrorKeepsWriteFailureVisible verifies os.ErrNotExist is not reclassified while the desk is still readable.
func TestBoardStoreResolveCreateTopicMutationErrorKeepsWriteFailureVisible(t *testing.T) {
	t.Parallel()

	store, _ := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())

	status, err := store.resolveCreateTopicMutationError(deskID, os.ErrNotExist)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Empty(t, status)
}

// TestBoardStoreResolveCreateTopicMutationErrorReturnsNotFound verifies os.ErrNotExist maps to not-found only after the desk disappears.
func TestBoardStoreResolveCreateTopicMutationErrorReturnsNotFound(t *testing.T) {
	t.Parallel()

	store, _ := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())
	require.NoError(t, store.DeleteDesk(t.Context(), deskID))

	status, err := store.resolveCreateTopicMutationError(deskID, os.ErrNotExist)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusNotFound, status)
}

// TestBoardStoreCollectExpiredDeskIDsTreatsCorruptedMetadataAsExpired verifies corrupted desk metadata remains collectible.
func TestBoardStoreCollectExpiredDeskIDsTreatsCorruptedMetadataAsExpired(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, _ := newBoardStoreForTest(t)
	expiredDeskID := mustCreateDesk(t, store, time.Now().UTC().Add(-2*time.Hour))
	corruptedDeskID := mustCreateDesk(t, store, time.Now().UTC())
	activeDeskID := mustCreateDesk(t, store, time.Now().UTC())

	corruptedDeskFilePath := filepath.Join(store.rootDir, corruptedDeskID, boardDeskStateFileName)
	require.NoError(t, os.WriteFile(corruptedDeskFilePath, []byte("{broken-json"), filePermission))
	corruptedSnapshotPath := filepath.Join(
		store.deskVersionsDir(corruptedDeskID),
		boardVersionFileName(1),
	)
	require.NoError(t, os.WriteFile(corruptedSnapshotPath, []byte("{broken-json"), filePermission))

	expiredDeskIDs, err := store.CollectExpiredDeskIDs(ctx, time.Now().UTC(), time.Hour)
	require.NoError(t, err)
	require.Contains(t, expiredDeskIDs, expiredDeskID)
	require.Contains(t, expiredDeskIDs, corruptedDeskID)
	require.NotContains(t, expiredDeskIDs, activeDeskID)
}

// TestBoardStoreCollectExpiredDeskIDsSkipsUnknownDirectories verifies TTL cleanup ignores non-desk directories under the shared root.
func TestBoardStoreCollectExpiredDeskIDsSkipsUnknownDirectories(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, rootDir := newBoardStoreForTest(t)
	expiredDeskID := mustCreateDesk(t, store, time.Now().UTC().Add(-2*time.Hour))
	foreignDirName := "user-folder"
	foreignDirPath := filepath.Join(rootDir, foreignDirName)
	require.NoError(t, os.Mkdir(foreignDirPath, directoryPermission))
	require.NoError(t, os.WriteFile(filepath.Join(foreignDirPath, "notes.txt"), []byte("keep"), filePermission))

	expiredDeskIDs, err := store.CollectExpiredDeskIDs(ctx, time.Now().UTC(), time.Hour)
	require.NoError(t, err)
	require.Contains(t, expiredDeskIDs, expiredDeskID)
	require.NotContains(t, expiredDeskIDs, foreignDirName)
}

// TestBoardStoreWriteTopicScaffoldDoesNotRecreateDeletedDesk verifies topic precommit fails instead of recreating a removed desk.
func TestBoardStoreWriteTopicScaffoldDoesNotRecreateDeletedDesk(t *testing.T) {
	t.Parallel()

	store, _ := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())
	topicID := "topic-new"
	topicState := boardTopicState{
		Version:                  1,
		Messages:                 []domain.MessageHeader{},
		MessageByNormalizedTitle: map[string]string{},
	}

	require.NoError(t, os.RemoveAll(store.deskDir(deskID)))

	err := store.writeTopicScaffold(t.Context(), deskID, topicID, topicState)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, statErr := os.Stat(store.deskDir(deskID))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

// TestBoardStoreWriteMessageArtifactsDoesNotRecreateDeletedDesk verifies message precommit fails instead of recreating a removed desk.
func TestBoardStoreWriteMessageArtifactsDoesNotRecreateDeletedDesk(t *testing.T) {
	t.Parallel()

	store, _ := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())
	topicID := mustCreateTopic(t, store, deskID, "Topic")
	messageID := "message-new"

	require.NoError(t, os.RemoveAll(store.deskDir(deskID)))

	err := store.writeMessageArtifacts(deskID, topicID, messageID, "# payload")
	require.ErrorIs(t, err, os.ErrNotExist)
	_, statErr := os.Stat(store.deskDir(deskID))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	_, found, lookupErr := store.loadMessageLookup(messageID)
	require.NoError(t, lookupErr)
	require.False(t, found)
}

// TestBoardStoreCommitTopicStateDoesNotRecreateDeletedDesk verifies topic commit fails instead of recreating a removed desk.
func TestBoardStoreCommitTopicStateDoesNotRecreateDeletedDesk(t *testing.T) {
	t.Parallel()

	store, _ := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())
	topicID := mustCreateTopic(t, store, deskID, "Topic")
	topicState, found, err := store.loadTopicState(deskID, topicID)
	require.NoError(t, err)
	require.True(t, found)

	nextTopicState := cloneTopicState(topicState)
	nextTopicState.Version = topicState.Version + 1
	nextTopicState.Messages = append(nextTopicState.Messages, domain.MessageHeader{MessageID: "message-new", Title: "Title"})
	nextTopicState.MessageByNormalizedTitle[normalizeTitleForTest("Title")] = "message-new"

	require.NoError(t, os.RemoveAll(store.deskDir(deskID)))

	err = store.commitTopicState(t.Context(), deskID, topicID, nextTopicState)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, statErr := os.Stat(store.deskDir(deskID))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

// TestBoardStoreDeleteDeskIsIdempotent verifies cleanup can safely race and removes visible desk state.
func TestBoardStoreDeleteDeskIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store, rootDir := newBoardStoreForTest(t)
	deskID := mustCreateDesk(t, store, time.Now().UTC())
	topicID := mustCreateTopic(t, store, deskID, "Topic")
	messageMeta, status, existingMessageID, err := store.CreateMessage(
		ctx,
		topicID,
		"Delete Me",
		normalizeTitleForTest("Delete Me"),
		"# payload",
	)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, status)
	require.Empty(t, existingMessageID)

	require.NoError(t, store.DeleteDesk(ctx, deskID))
	require.NoError(t, store.DeleteDesk(ctx, deskID))

	reopenedStore, err := NewBoardStore(rootDir)
	require.NoError(t, err)

	topics, deskFound, err := reopenedStore.ListTopics(ctx, deskID)
	require.NoError(t, err)
	require.False(t, deskFound)
	require.Nil(t, topics)

	messages, topicFound, err := reopenedStore.ListMessages(ctx, topicID)
	require.NoError(t, err)
	require.False(t, topicFound)
	require.Nil(t, messages)

	meta, content, messageFound, err := reopenedStore.GetMessage(ctx, messageMeta.MessageID)
	require.NoError(t, err)
	require.False(t, messageFound)
	require.Equal(t, emptyMessageMeta(), meta)
	require.Empty(t, content)
}

// TestBoardStoreCreateTopicRetriesOnVersionConflict verifies concurrent duplicate topic creation converges to one record.
func TestBoardStoreCreateTopicRetriesOnVersionConflict(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		store, rootDir := newBoardStoreForTest(t)
		deskID := mustCreateDesk(t, store, time.Now().UTC())
		secondStore, err := NewBoardStore(rootDir)
		require.NoError(t, err)

		start := make(chan struct{})
		results := make(chan topicCreateOutcome, 2)
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-start
			header, status, created, createErr := store.CreateTopic(ctx, deskID, "Shared")
			results <- topicCreateOutcome{Header: header, Status: status, Created: created, Err: createErr}
		}()

		go func() {
			defer wg.Done()
			<-start
			header, status, created, createErr := secondStore.CreateTopic(ctx, deskID, "Shared")
			results <- topicCreateOutcome{Header: header, Status: status, Created: created, Err: createErr}
		}()

		close(start)
		wg.Wait()
		close(results)

		outcomes := make([]topicCreateOutcome, 0, 2)
		for outcome := range results {
			outcomes = append(outcomes, outcome)
		}

		require.Len(t, outcomes, 2)
		createdCount := 0
		topicIDs := make(map[string]struct{})
		for _, outcome := range outcomes {
			require.NoError(t, outcome.Err)
			require.Equal(t, domain.BusinessStatusOK, outcome.Status)
			topicIDs[outcome.Header.TopicID] = struct{}{}
			if outcome.Created {
				createdCount++
			}
		}
		require.Equal(t, 1, createdCount)
		require.Len(t, topicIDs, 1)

		reopenedStore, reopenErr := NewBoardStore(rootDir)
		require.NoError(t, reopenErr)

		topics, found, listErr := reopenedStore.ListTopics(ctx, deskID)
		require.NoError(t, listErr)
		require.True(t, found)
		require.Equal(t, []domain.TopicHeader{{TopicID: outcomes[0].Header.TopicID, Title: "Shared"}}, topics)
	})
}

// TestBoardStoreCreateMessageRetriesOnVersionConflict verifies concurrent duplicate message creation converges to one record.
func TestBoardStoreCreateMessageRetriesOnVersionConflict(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		store, rootDir := newBoardStoreForTest(t)
		deskID := mustCreateDesk(t, store, time.Now().UTC())
		topicID := mustCreateTopic(t, store, deskID, "Topic")
		secondStore, err := NewBoardStore(rootDir)
		require.NoError(t, err)

		start := make(chan struct{})
		results := make(chan messageCreateOutcome, 2)
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-start
			meta, status, existingMessageID, createErr := store.CreateMessage(
				ctx,
				topicID,
				"Shared Message",
				normalizeTitleForTest("Shared Message"),
				"# payload",
			)
			results <- messageCreateOutcome{Meta: meta, Status: status, ExistingMessageID: existingMessageID, Err: createErr}
		}()

		go func() {
			defer wg.Done()
			<-start
			meta, status, existingMessageID, createErr := secondStore.CreateMessage(
				ctx,
				topicID,
				"SHARED   MESSAGE",
				normalizeTitleForTest("SHARED   MESSAGE"),
				"# payload duplicate",
			)
			results <- messageCreateOutcome{Meta: meta, Status: status, ExistingMessageID: existingMessageID, Err: createErr}
		}()

		close(start)
		wg.Wait()
		close(results)

		outcomes := make([]messageCreateOutcome, 0, 2)
		for outcome := range results {
			outcomes = append(outcomes, outcome)
		}

		require.Len(t, outcomes, 2)
		createdMessageID := ""
		createdTitle := ""
		duplicateCount := 0
		createdCount := 0
		for _, outcome := range outcomes {
			require.NoError(t, outcome.Err)
			switch outcome.Status {
			case domain.BusinessStatusOK:
				createdCount++
				createdMessageID = outcome.Meta.MessageID
				createdTitle = outcome.Meta.Title
			case domain.BusinessStatusDuplicateTitle:
				duplicateCount++
			case domain.BusinessStatusNotFound:
				t.Fatalf("unexpected not_found status")
			default:
				t.Fatalf("unexpected status: %s", outcome.Status)
			}
		}
		require.Equal(t, 1, createdCount)
		require.Equal(t, 1, duplicateCount)
		for _, outcome := range outcomes {
			if outcome.Status == domain.BusinessStatusDuplicateTitle {
				require.Equal(t, createdMessageID, outcome.ExistingMessageID)
			}
		}

		reopenedStore, reopenErr := NewBoardStore(rootDir)
		require.NoError(t, reopenErr)

		messages, found, listErr := reopenedStore.ListMessages(ctx, topicID)
		require.NoError(t, listErr)
		require.True(t, found)
		require.Equal(t, []domain.MessageHeader{{MessageID: createdMessageID, Title: createdTitle}}, messages)
	})
}

// TestBoardStoreCreateMessagePreservesCommittedOrder verifies distinct concurrent writes are kept exactly once in commit order.
func TestBoardStoreCreateMessagePreservesCommittedOrder(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		store, rootDir := newBoardStoreForTest(t)
		deskID := mustCreateDesk(t, store, time.Now().UTC())
		topicID := mustCreateTopic(t, store, deskID, "Topic")
		secondStore, err := NewBoardStore(rootDir)
		require.NoError(t, err)

		start := make(chan struct{})
		results := make(chan messageCreateOutcome, 2)
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-start
			meta, status, existingMessageID, createErr := store.CreateMessage(
				ctx,
				topicID,
				"First",
				normalizeTitleForTest("First"),
				"# first",
			)
			results <- messageCreateOutcome{Meta: meta, Status: status, ExistingMessageID: existingMessageID, Err: createErr}
		}()

		go func() {
			defer wg.Done()
			<-start
			meta, status, existingMessageID, createErr := secondStore.CreateMessage(
				ctx,
				topicID,
				"Second",
				normalizeTitleForTest("Second"),
				"# second",
			)
			results <- messageCreateOutcome{Meta: meta, Status: status, ExistingMessageID: existingMessageID, Err: createErr}
		}()

		close(start)
		wg.Wait()
		close(results)

		completionOrder := make([]domain.MessageHeader, 0, 2)
		for outcome := range results {
			require.NoError(t, outcome.Err)
			require.Equal(t, domain.BusinessStatusOK, outcome.Status)
			completionOrder = append(completionOrder, domain.MessageHeader{MessageID: outcome.Meta.MessageID, Title: outcome.Meta.Title})
		}
		require.Len(t, completionOrder, 2)

		reopenedStore, reopenErr := NewBoardStore(rootDir)
		require.NoError(t, reopenErr)

		messages, found, listErr := reopenedStore.ListMessages(ctx, topicID)
		require.NoError(t, listErr)
		require.True(t, found)
		require.Equal(t, completionOrder, messages)
	})
}

// topicCreateOutcome keeps concurrent topic-create assertions readable.
type topicCreateOutcome struct {
	Header  domain.TopicHeader
	Status  domain.BusinessStatus
	Created bool
	Err     error
}

// messageCreateOutcome keeps concurrent message-create assertions readable.
type messageCreateOutcome struct {
	Meta              domain.MessageMeta
	Status            domain.BusinessStatus
	ExistingMessageID string
	Err               error
}

// newBoardStoreForTest constructs a board store rooted in a fresh temp directory.
func newBoardStoreForTest(t *testing.T) (*BoardStore, string) {
	t.Helper()

	rootDir := filepath.Join(t.TempDir(), "messages")
	store, err := NewBoardStore(rootDir)
	require.NoError(t, err)

	return store, rootDir
}

// mustCreateDesk creates a desk and fails the test immediately on error.
func mustCreateDesk(t *testing.T, store *BoardStore, createdAt time.Time) string {
	t.Helper()

	deskID, err := store.CreateDesk(t.Context(), createdAt)
	require.NoError(t, err)
	require.NotEmpty(t, deskID)

	return deskID
}

// mustCreateTopic creates a topic and fails the test immediately on error.
func mustCreateTopic(t *testing.T, store *BoardStore, deskID string, title string) string {
	t.Helper()

	header, status, created, err := store.CreateTopic(t.Context(), deskID, title)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, status)
	require.True(t, created)
	require.NotEmpty(t, header.TopicID)

	return header.TopicID
}

// normalizeTitleForTest mirrors usecase duplicate-title normalization so storage tests assert the same invariant.
func normalizeTitleForTest(title string) string {
	lowerTitle := strings.ToLower(title)

	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v' {
			return -1
		}

		return r
	}, lowerTitle)
}
