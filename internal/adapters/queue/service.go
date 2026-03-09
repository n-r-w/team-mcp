package queue

import (
	"context"
	"sync"

	"github.com/n-r-w/team-mcp/internal/domain"
	"github.com/n-r-w/team-mcp/internal/usecase"
)

var _ usecase.IHeaderQueue = (*Service)(nil)

// Service stores deterministic topic/message headers in memory.
type Service struct {
	mu sync.RWMutex

	desks  map[string]*deskState
	topics map[string]*topicState

	maxBufferedHeaders int
}

// New constructs ordered header queue with bounded per-topic message capacity.
func New(maxBufferedHeaders int) *Service {
	capacity := max(maxBufferedHeaders, minimumBufferedHeaders)

	return &Service{
		mu:                 sync.RWMutex{},
		desks:              make(map[string]*deskState),
		topics:             make(map[string]*topicState),
		maxBufferedHeaders: capacity,
	}
}

// EnsureTopic adds topic header to desk list if it is not present yet.
func (s *Service) EnsureTopic(_ context.Context, deskID string, header domain.TopicHeader) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	desk := s.desks[deskID]
	if desk == nil {
		desk = &deskState{topics: make([]domain.TopicHeader, 0)}
		s.desks[deskID] = desk
	}

	for _, existing := range desk.topics {
		if existing.TopicID == header.TopicID {
			if _, exists := s.topics[header.TopicID]; !exists {
				s.topics[header.TopicID] = &topicState{messages: make([]domain.MessageHeader, 0)}
			}

			return nil
		}
	}

	desk.topics = append(desk.topics, header)
	if _, exists := s.topics[header.TopicID]; !exists {
		s.topics[header.TopicID] = &topicState{messages: make([]domain.MessageHeader, 0)}
	}

	return nil
}

// ListTopics returns topic headers in first successful EnsureTopic insertion order.
func (s *Service) ListTopics(_ context.Context, deskID string) ([]domain.TopicHeader, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	desk := s.desks[deskID]
	if desk == nil {
		return nil, false, nil
	}

	headers := make([]domain.TopicHeader, 0, len(desk.topics))
	headers = append(headers, desk.topics...)

	return headers, true, nil
}

// AppendMessage appends message header in topic order.
func (s *Service) AppendMessage(_ context.Context, topicID string, header domain.MessageHeader) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	topic := s.topics[topicID]
	if topic == nil {
		return domain.NewError(domain.ErrorCodeStorageInvariant)
	}

	if len(topic.messages) >= s.maxBufferedHeaders {
		return domain.NewError(domain.ErrorCodeCapacityExceeded)
	}

	topic.messages = append(topic.messages, header)

	return nil
}

// RemoveMessage removes message header from topic list.
func (s *Service) RemoveMessage(_ context.Context, topicID, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	topic := s.topics[topicID]
	if topic == nil {
		return nil
	}

	nextHeaders := make([]domain.MessageHeader, 0, len(topic.messages))
	for _, header := range topic.messages {
		if header.MessageID == messageID {
			continue
		}

		nextHeaders = append(nextHeaders, header)
	}
	topic.messages = nextHeaders

	return nil
}

// ListMessages returns ordered message headers for topic.
func (s *Service) ListMessages(_ context.Context, topicID string) ([]domain.MessageHeader, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	topic := s.topics[topicID]
	if topic == nil {
		return nil, false, nil
	}

	headers := make([]domain.MessageHeader, 0, len(topic.messages))
	headers = append(headers, topic.messages...)

	return headers, true, nil
}

// DeleteDesk removes ordered topic/message headers linked to desk.
func (s *Service) DeleteDesk(_ context.Context, deskID string, topicIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.desks, deskID)
	for _, topicID := range topicIDs {
		delete(s.topics, topicID)
	}

	return nil
}
