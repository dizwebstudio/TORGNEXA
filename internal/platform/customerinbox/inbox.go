// Package customerinbox aggregates remote conversations with PII-minimized messages and human-scoped replies.
package customerinbox

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"sync"
	"time"
)

var (
	ErrInvalid     = errors.New("customerinbox: invalid value")
	ErrNotFound    = errors.New("customerinbox: not found")
	ErrAIDraftOnly = errors.New("customerinbox: AI output is draft-only")
)

type CaseState string

const (
	CaseOpen     CaseState = "open"
	CasePending  CaseState = "pending"
	CaseResolved CaseState = "resolved"
)

func (s CaseState) Valid() bool {
	return s == CaseOpen || s == CasePending || s == CaseResolved
}

type Case struct {
	ID, ConversationID, AssigneeID string
	State                          CaseState
	SLADeadline                    time.Time
	Version                        int64
	UpdatedAt                      time.Time
}

type Assignment struct {
	CaseID, AssigneeID string
	At                 time.Time
}

type Conversation struct {
	ID, SourceSystem, AccountID, RemoteThreadID string
	CaseID, AssigneeID                          string
	SLADeadline                                 time.Time
	Version                                     int64
	UpdatedAt                                   time.Time
}
type Message struct {
	ID, ConversationID, RemoteMessageID, Direction, Body string
	At                                                   time.Time
}
type Redactor interface {
	Redact(context.Context, tenancy.Scope, string) (string, error)
}
type Sender interface {
	Reply(context.Context, tenancy.Scope, Conversation, string, string) (string, error)
}
type inboxTenant struct {
	byRemote        map[string]string
	conversations   map[string]Conversation
	byRemoteMessage map[string]Message
	cases           map[string]Case
}
type Service struct {
	mu       sync.Mutex
	tenants  map[string]*inboxTenant
	Redactor Redactor
	Sender   Sender
}

func NewService(r Redactor, s Sender) *Service {
	return &Service{tenants: map[string]*inboxTenant{}, Redactor: r, Sender: s}
}
func inScope(scope tenancy.Scope) string {
	return scope.OrganizationID().String() + "/" + scope.WorkspaceID().String()
}
func (s *Service) state(scope tenancy.Scope) *inboxTenant {
	k := inScope(scope)
	st := s.tenants[k]
	if st == nil {
		st = &inboxTenant{byRemote: map[string]string{}, conversations: map[string]Conversation{}, byRemoteMessage: map[string]Message{}, cases: map[string]Case{}}
		s.tenants[k] = st
	}
	return st
}
func remoteKey(source, account, thread string) string {
	return source + "\x00" + account + "\x00" + thread
}
func (s *Service) UpsertConversation(scope tenancy.Scope, c Conversation) (Conversation, error) {
	if !scope.Valid() || c.ID == "" || c.SourceSystem == "" || c.AccountID == "" || c.RemoteThreadID == "" || c.Version < 1 || c.UpdatedAt.IsZero() {
		return Conversation{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state(scope)
	k := remoteKey(c.SourceSystem, c.AccountID, c.RemoteThreadID)
	if id, ok := st.byRemote[k]; ok {
		return st.conversations[id], nil
	}
	st.byRemote[k] = c.ID
	st.conversations[c.ID] = c
	return c, nil
}
func (s *Service) StoreMessage(ctx context.Context, scope tenancy.Scope, m Message) (Message, error) {
	if !scope.Valid() || m.ID == "" || m.ConversationID == "" || m.RemoteMessageID == "" || m.Body == "" || m.At.IsZero() || s.Redactor == nil {
		return Message{}, ErrInvalid
	}
	body, err := s.Redactor.Redact(ctx, scope, m.Body)
	if err != nil {
		return Message{}, err
	}
	m.Body = body
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state(scope)
	if _, ok := st.conversations[m.ConversationID]; !ok {
		return Message{}, ErrInvalid
	}
	k := m.ConversationID + "\x00" + m.RemoteMessageID
	if old, ok := st.byRemoteMessage[k]; ok {
		return old, nil
	}
	st.byRemoteMessage[k] = m
	return m, nil
}

func (s *Service) OpenCase(scope tenancy.Scope, c Case) (Case, error) {
	if !scope.Valid() || c.ID == "" || c.ConversationID == "" || !c.State.Valid() || c.Version < 1 || c.SLADeadline.IsZero() || c.UpdatedAt.IsZero() || !c.SLADeadline.Equal(c.SLADeadline.UTC()) || !c.UpdatedAt.Equal(c.UpdatedAt.UTC()) {
		return Case{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state(scope)
	conversation, ok := st.conversations[c.ConversationID]
	if !ok {
		return Case{}, ErrNotFound
	}
	if existing, ok := st.cases[c.ID]; ok {
		return existing, nil
	}
	st.cases[c.ID] = c
	conversation.CaseID = c.ID
	conversation.AssigneeID = c.AssigneeID
	conversation.SLADeadline = c.SLADeadline
	conversation.Version++
	conversation.UpdatedAt = c.UpdatedAt
	st.conversations[c.ConversationID] = conversation
	return c, nil
}

func (s *Service) Assign(scope tenancy.Scope, assignment Assignment) (Case, error) {
	if !scope.Valid() || assignment.CaseID == "" || assignment.AssigneeID == "" || assignment.At.IsZero() || !assignment.At.Equal(assignment.At.UTC()) {
		return Case{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.tenants[inScope(scope)]
	if st == nil {
		return Case{}, ErrNotFound
	}
	c, ok := st.cases[assignment.CaseID]
	if !ok {
		return Case{}, ErrNotFound
	}
	c.AssigneeID = assignment.AssigneeID
	c.Version++
	c.UpdatedAt = assignment.At
	st.cases[c.ID] = c
	conversation := st.conversations[c.ConversationID]
	conversation.AssigneeID = assignment.AssigneeID
	conversation.Version++
	conversation.UpdatedAt = assignment.At
	st.conversations[c.ConversationID] = conversation
	return c, nil
}

func (s *Service) GetCase(scope tenancy.Scope, caseID string) (Case, error) {
	if !scope.Valid() || caseID == "" {
		return Case{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.tenants[inScope(scope)]
	if st == nil {
		return Case{}, ErrNotFound
	}
	c, ok := st.cases[caseID]
	if !ok {
		return Case{}, ErrNotFound
	}
	return c, nil
}

func SLABreached(c Case, now time.Time) bool {
	return c.ID != "" && !c.SLADeadline.IsZero() && !now.Before(c.SLADeadline) && c.State != CaseResolved
}

func (s *Service) Reply(ctx context.Context, scope tenancy.Scope, conversationID, body, origin, idempotency string) (string, error) {
	if !scope.Valid() || conversationID == "" || body == "" || idempotency == "" {
		return "", ErrInvalid
	}
	if origin == "ai" {
		return "", ErrAIDraftOnly
	}
	if origin != "human" {
		return "", ErrInvalid
	}
	s.mu.Lock()
	st := s.tenants[inScope(scope)]
	var c Conversation
	var ok bool
	if st != nil {
		c, ok = st.conversations[conversationID]
	}
	s.mu.Unlock()
	if !ok || s.Sender == nil {
		return "", ErrInvalid
	}
	return s.Sender.Reply(ctx, scope, c, body, idempotency)
}
