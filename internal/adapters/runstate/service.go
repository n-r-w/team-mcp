package runstate

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/n-r-w/team-mcp/internal/domain"
	"github.com/n-r-w/team-mcp/internal/usecase"
)

var _ usecase.IRunRegistry = (*Service)(nil)

// Service provides in-memory desk/topic/message metadata and indexes.
type Service struct {
	mu sync.RWMutex

	desks    map[string]*deskRecord
	topics   map[string]*topicRecord
	messages map[string]*messageRecord

	maxActiveDesks int
}

// New constructs runstate service with bounded active desk capacity.
func New(maxActiveDesks int) *Service {
	capacity := max(maxActiveDesks, minimumActiveDeskLimit)

	return &Service{
		mu:             sync.RWMutex{},
		desks:          make(map[string]*deskRecord),
		topics:         make(map[string]*topicRecord),
		messages:       make(map[string]*messageRecord),
		maxActiveDesks: capacity,
	}
}

// CreateDesk creates a new desk and returns generated identifier.
func (s *Service) CreateDesk(_ context.Context, createdAt time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.desks) >= s.maxActiveDesks {
		return "", domain.NewError(domain.ErrorCodeCapacityExceeded)
	}

	deskID := uuid.NewString()
	s.desks[deskID] = &deskRecord{
		id:           deskID,
		createdAt:    createdAt.UTC(),
		topicIDs:     make([]string, 0),
		topicByTitle: make(map[string]string),
	}

	return deskID, nil
}

// DeskExists reports whether desk is present in memory.
func (s *Service) DeskExists(_ context.Context, deskID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.desks[deskID]

	return exists, nil
}

// TopicExists reports whether topic is present in memory.
func (s *Service) TopicExists(_ context.Context, topicID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.topics[topicID]

	return exists, nil
}

// CreateTopic creates topic under desk or returns existing topic identifier for idempotent calls.
func (s *Service) CreateTopic(
	_ context.Context,
	deskID string,
	title string,
) (domain.TopicHeader, domain.BusinessStatus, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	desk := s.desks[deskID]
	if desk == nil {
		return domain.TopicHeader{TopicID: "", Title: ""}, domain.BusinessStatusNotFound, false, nil
	}

	if existingTopicID, exists := desk.topicByTitle[title]; exists {
		return domain.TopicHeader{TopicID: existingTopicID, Title: title}, domain.BusinessStatusOK, false, nil
	}

	topicID := uuid.NewString()
	desk.topicByTitle[title] = topicID
	desk.topicIDs = append(desk.topicIDs, topicID)

	s.topics[topicID] = &topicRecord{
		deskID:                   deskID,
		messageIDs:               make([]string, 0),
		messageByNormalizedTitle: make(map[string]string),
	}

	return domain.TopicHeader{TopicID: topicID, Title: title}, domain.BusinessStatusOK, true, nil
}

// CreateMessage creates message metadata under topic or reports business conflict/not-found status.
func (s *Service) CreateMessage(
	_ context.Context,
	topicID string,
	title string,
	normalizedTitle string,
) (domain.MessageMeta, domain.BusinessStatus, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	topic := s.topics[topicID]
	if topic == nil {
		return emptyMessageMeta(),
			domain.BusinessStatusNotFound,
			"",
			nil
	}

	if existingMessageID, exists := topic.messageByNormalizedTitle[normalizedTitle]; exists {
		return emptyMessageMeta(),
			domain.BusinessStatusDuplicateTitle,
			existingMessageID,
			nil
	}

	messageID := uuid.NewString()
	topic.messageByNormalizedTitle[normalizedTitle] = messageID
	topic.messageIDs = append(topic.messageIDs, messageID)

	meta := &messageRecord{
		id:              messageID,
		deskID:          topic.deskID,
		topicID:         topicID,
		title:           title,
		normalizedTitle: normalizedTitle,
	}
	s.messages[messageID] = meta

	return domain.MessageMeta{
		MessageID: messageID,
		TopicID:   meta.topicID,
		DeskID:    meta.deskID,
		Title:     meta.title,
	}, domain.BusinessStatusOK, "", nil
}

// DeleteMessage removes message metadata and duplicate-title index from topic.
func (s *Service) DeleteMessage(_ context.Context, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	message := s.messages[messageID]
	if message == nil {
		return nil
	}

	delete(s.messages, messageID)

	topic := s.topics[message.topicID]
	if topic == nil {
		return nil
	}

	delete(topic.messageByNormalizedTitle, message.normalizedTitle)

	nextMessageIDs := make([]string, 0, len(topic.messageIDs))
	for _, candidateMessageID := range topic.messageIDs {
		if candidateMessageID == messageID {
			continue
		}

		nextMessageIDs = append(nextMessageIDs, candidateMessageID)
	}
	topic.messageIDs = nextMessageIDs

	return nil
}

// GetMessageMeta resolves message metadata by identifier.
func (s *Service) GetMessageMeta(_ context.Context, messageID string) (domain.MessageMeta, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	message := s.messages[messageID]
	if message == nil {
		return emptyMessageMeta(), false, nil
	}

	return domain.MessageMeta{
		MessageID: message.id,
		TopicID:   message.topicID,
		DeskID:    message.deskID,
		Title:     message.title,
	}, true, nil
}

// GetDeskSnapshot returns desk cascade metadata for synchronous cleanup operations.
func (s *Service) GetDeskSnapshot(_ context.Context, deskID string) (domain.DeskSnapshot, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	desk := s.desks[deskID]
	if desk == nil {
		return domain.DeskSnapshot{
			DeskID:     "",
			CreatedAt:  time.Time{},
			TopicIDs:   nil,
			MessageIDs: nil,
		}, false, nil
	}

	topicIDs := make([]string, 0, len(desk.topicIDs))
	messageIDs := make([]string, 0)
	for _, topicID := range desk.topicIDs {
		topicIDs = append(topicIDs, topicID)

		topic := s.topics[topicID]
		if topic == nil {
			continue
		}

		messageIDs = append(messageIDs, topic.messageIDs...)
	}

	return domain.DeskSnapshot{
		DeskID:     desk.id,
		CreatedAt:  desk.createdAt,
		TopicIDs:   topicIDs,
		MessageIDs: messageIDs,
	}, true, nil
}

// DeleteDesk removes desk metadata and all related topic/message indexes from memory.
func (s *Service) DeleteDesk(_ context.Context, deskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	desk := s.desks[deskID]
	if desk == nil {
		return nil
	}

	for _, topicID := range desk.topicIDs {
		topic := s.topics[topicID]
		if topic == nil {
			continue
		}

		for _, messageID := range topic.messageIDs {
			delete(s.messages, messageID)
		}

		delete(s.topics, topicID)
	}

	delete(s.desks, deskID)

	return nil
}

// ListDeskIDs returns all known active desk identifiers.
func (s *Service) ListDeskIDs(_ context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deskIDs := make([]string, 0, len(s.desks))
	for deskID := range s.desks {
		deskIDs = append(deskIDs, deskID)
	}

	return deskIDs, nil
}

// CollectExpiredDeskIDs returns desk identifiers whose derived expiry time is reached.
func (s *Service) CollectExpiredDeskIDs(_ context.Context, now time.Time, ttl time.Duration) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nowUTC := now.UTC()
	expiredDeskIDs := make([]string, 0)
	for deskID, desk := range s.desks {
		expiresAt := desk.createdAt.Add(ttl)
		if expiresAt.After(nowUTC) {
			continue
		}

		expiredDeskIDs = append(expiredDeskIDs, deskID)
	}

	return expiredDeskIDs, nil
}

func emptyMessageMeta() domain.MessageMeta {
	return domain.MessageMeta{MessageID: "", TopicID: "", DeskID: "", Title: ""}
}
