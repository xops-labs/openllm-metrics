// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package server exposes the notifier's HTTP surface:
//
//   - /v1/notification/channels       — CRUD on notification_channels
//   - /v1/notification/rules          — CRUD on notification_rules
//   - /v1/notification/deliveries     — read-only delivery history
//   - /metrics                        — Prometheus exposition
//   - /healthz                        — liveness
//
// Every CRUD endpoint expects the caller's tenant in the `X-Tenant-Id`
// header. The OSS distribution does not implement authn/authz at this layer;
// operators should front the notifier with a service-mesh or reverse-proxy
// policy that enforces tenancy.
//
// Every successful mutation emits an audit.event.v1 event for F031.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/store"
)

// TenantHeader names the request header carrying the caller's tenant UUID.
const TenantHeader = "X-Tenant-Id"

// ActorHeader is optional; when present it is attached to the audit event.
const ActorHeader = "X-Actor"

// Server bundles the dependencies the HTTP handlers need.
type Server struct {
	store   store.Store
	emitter *busproducer.AuditEmitter
	mreg    *metrics.Registry
	logger  *slog.Logger
}

// New constructs a Server.
func New(s store.Store, e *busproducer.AuditEmitter, m *metrics.Registry, logger *slog.Logger) *Server {
	return &Server{store: s, emitter: e, mreg: m, logger: logger}
}

// Routes returns an http.Handler with all endpoints registered.
func (s *Server) Routes(metricsHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/notification/channels", s.handleChannels)
	mux.HandleFunc("/v1/notification/channels/", s.handleChannelByID)
	mux.HandleFunc("/v1/notification/rules", s.handleRules)
	mux.HandleFunc("/v1/notification/rules/", s.handleRuleByID)
	mux.HandleFunc("/v1/notification/deliveries", s.handleDeliveries)
	mux.Handle("/metrics", metricsHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// ----------------------------------------------------------------------
// Channels
// ----------------------------------------------------------------------

// channelDTO is the wire-format representation of a channel. config is
// passed through verbatim so callers can put webhook or smtp-specific keys
// inside without server-side validation gymnastics. The CHECK constraint on
// the kind column is the source of truth.
type channelDTO struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Config    json.RawMessage `json:"config"`
	CreatedAt time.Time       `json:"created_at,omitempty"`
	UpdatedAt time.Time       `json:"updated_at,omitempty"`
}

func (s *Server) handleChannels(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listChannels(w, r, tenantID)
	case http.MethodPost:
		s.createChannel(w, r, tenantID)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleChannelByID(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/notification/channels/")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getChannel(w, r, tenantID, id)
	case http.MethodPut:
		s.updateChannel(w, r, tenantID, id)
	case http.MethodDelete:
		s.deleteChannel(w, r, tenantID, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request, tenantID string) {
	chans, err := s.store.ListChannels(r.Context(), tenantID)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]channelDTO, 0, len(chans))
	for _, c := range chans {
		out = append(out, channelToDTO(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request, tenantID string) {
	var dto channelDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if dto.Kind != "webhook" && dto.Kind != "smtp" {
		http.Error(w, "kind must be webhook or smtp", http.StatusBadRequest)
		return
	}
	if dto.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	ch := &store.Channel{
		TenantID: tenantID,
		Name:     dto.Name,
		Kind:     dto.Kind,
		Config:   dto.Config,
	}
	if err := s.store.CreateChannel(r.Context(), ch); err != nil {
		s.fail(w, err)
		return
	}
	s.mreg.IncConfigMutation()
	s.audit(r, tenantID, "channel.create", "notification_channel", ch.ID, map[string]any{"name": ch.Name, "kind": ch.Kind}) //nolint:contextcheck // audit uses a detached timeout ctx by design
	writeJSON(w, http.StatusCreated, channelToDTO(*ch))
}

func (s *Server) getChannel(w http.ResponseWriter, r *http.Request, tenantID, id string) {
	ch, err := s.store.GetChannel(r.Context(), tenantID, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, channelToDTO(*ch))
}

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request, tenantID, id string) {
	var dto channelDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if dto.Kind != "webhook" && dto.Kind != "smtp" {
		http.Error(w, "kind must be webhook or smtp", http.StatusBadRequest)
		return
	}
	ch := &store.Channel{
		ID:       id,
		TenantID: tenantID,
		Name:     dto.Name,
		Kind:     dto.Kind,
		Config:   dto.Config,
	}
	if err := s.store.UpdateChannel(r.Context(), ch); err != nil {
		s.fail(w, err)
		return
	}
	s.mreg.IncConfigMutation()
	s.audit(r, tenantID, "channel.update", "notification_channel", ch.ID, map[string]any{"name": ch.Name, "kind": ch.Kind}) //nolint:contextcheck // audit uses a detached timeout ctx by design
	writeJSON(w, http.StatusOK, channelToDTO(*ch))
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request, tenantID, id string) {
	if err := s.store.DeleteChannel(r.Context(), tenantID, id); err != nil {
		s.fail(w, err)
		return
	}
	s.mreg.IncConfigMutation()
	s.audit(r, tenantID, "channel.delete", "notification_channel", id, nil) //nolint:contextcheck // audit uses a detached timeout ctx by design
	w.WriteHeader(http.StatusNoContent)
}

func channelToDTO(c store.Channel) channelDTO {
	return channelDTO{
		ID:        c.ID,
		Name:      c.Name,
		Kind:      c.Kind,
		Config:    c.Config,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

// ----------------------------------------------------------------------
// Rules
// ----------------------------------------------------------------------

type ruleDTO struct {
	ID         string          `json:"id,omitempty"`
	Name       string          `json:"name"`
	Match      json.RawMessage `json:"match"`
	ChannelIDs []string        `json:"channel_ids"`
	CreatedAt  time.Time       `json:"created_at,omitempty"`
	UpdatedAt  time.Time       `json:"updated_at,omitempty"`
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listRules(w, r, tenantID)
	case http.MethodPost:
		s.createRule(w, r, tenantID)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRuleByID(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/notification/rules/")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getRule(w, r, tenantID, id)
	case http.MethodPut:
		s.updateRule(w, r, tenantID, id)
	case http.MethodDelete:
		s.deleteRule(w, r, tenantID, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request, tenantID string) {
	rules, err := s.store.ListRules(r.Context(), tenantID)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]ruleDTO, 0, len(rules))
	for _, x := range rules {
		out = append(out, ruleToDTO(x))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request, tenantID string) {
	var dto ruleDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if dto.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	rule := &store.Rule{
		TenantID:   tenantID,
		Name:       dto.Name,
		Match:      dto.Match,
		ChannelIDs: dto.ChannelIDs,
	}
	if err := s.store.CreateRule(r.Context(), rule); err != nil {
		s.fail(w, err)
		return
	}
	s.mreg.IncConfigMutation()
	s.audit(r, tenantID, "rule.create", "notification_rule", rule.ID, map[string]any{"name": rule.Name, "channel_count": len(rule.ChannelIDs)}) //nolint:contextcheck // audit uses a detached timeout ctx by design
	writeJSON(w, http.StatusCreated, ruleToDTO(*rule))
}

func (s *Server) getRule(w http.ResponseWriter, r *http.Request, tenantID, id string) {
	rule, err := s.store.GetRule(r.Context(), tenantID, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ruleToDTO(*rule))
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request, tenantID, id string) {
	var dto ruleDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}
	rule := &store.Rule{
		ID:         id,
		TenantID:   tenantID,
		Name:       dto.Name,
		Match:      dto.Match,
		ChannelIDs: dto.ChannelIDs,
	}
	if err := s.store.UpdateRule(r.Context(), rule); err != nil {
		s.fail(w, err)
		return
	}
	s.mreg.IncConfigMutation()
	s.audit(r, tenantID, "rule.update", "notification_rule", rule.ID, map[string]any{"name": rule.Name, "channel_count": len(rule.ChannelIDs)}) //nolint:contextcheck // audit uses a detached timeout ctx by design
	writeJSON(w, http.StatusOK, ruleToDTO(*rule))
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request, tenantID, id string) {
	if err := s.store.DeleteRule(r.Context(), tenantID, id); err != nil {
		s.fail(w, err)
		return
	}
	s.mreg.IncConfigMutation()
	s.audit(r, tenantID, "rule.delete", "notification_rule", id, nil) //nolint:contextcheck // audit uses a detached timeout ctx by design
	w.WriteHeader(http.StatusNoContent)
}

func ruleToDTO(r store.Rule) ruleDTO {
	return ruleDTO{
		ID:         r.ID,
		Name:       r.Name,
		Match:      r.Match,
		ChannelIDs: r.ChannelIDs,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}

// ----------------------------------------------------------------------
// Deliveries (read-only history)
// ----------------------------------------------------------------------

type deliveryDTO struct {
	ID           int64      `json:"id"`
	RuleID       string     `json:"rule_id"`
	ChannelID    string     `json:"channel_id"`
	AlertEventID string     `json:"alert_event_id"`
	Status       string     `json:"status"`
	Attempts     int        `json:"attempts"`
	LastError    string     `json:"last_error,omitempty"`
	SentAt       *time.Time `json:"sent_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (s *Server) handleDeliveries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tenantID, ok := requireTenant(w, r)
	if !ok {
		return
	}
	ruleID := r.URL.Query().Get("rule_id")
	var fromPtr, toPtr *time.Time
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "from must be RFC3339", http.StatusBadRequest)
			return
		}
		fromPtr = &t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "to must be RFC3339", http.StatusBadRequest)
			return
		}
		toPtr = &t
	}
	deliveries, err := s.store.ListDeliveries(r.Context(), tenantID, ruleID, fromPtr, toPtr)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]deliveryDTO, 0, len(deliveries))
	for _, d := range deliveries {
		out = append(out, deliveryDTO{
			ID:           d.ID,
			RuleID:       d.RuleID,
			ChannelID:    d.ChannelID,
			AlertEventID: d.AlertEventID,
			Status:       d.Status,
			Attempts:     d.Attempts,
			LastError:    d.LastError,
			SentAt:       d.SentAt,
			CreatedAt:    d.CreatedAt,
			UpdatedAt:    d.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// ----------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------

func requireTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	t := r.Header.Get(TenantHeader)
	if t == "" {
		http.Error(w, fmt.Sprintf("missing %s header", TenantHeader), http.StatusUnauthorized)
		return "", false
	}
	return t, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.logger.Warn("notifier api: handler error", "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// audit emits a best-effort audit.event.v1. Failures are logged but never
// surfaced to the API caller — the source-of-truth mutation has already
// committed. F031 (the audit ledger) can be reconciled from the DB if the
// bus drops the event. The detached 2s context is deliberate: the audit
// emission must survive the client disconnecting mid-response.
func (s *Server) audit(r *http.Request, tenantID, action, resource, resourceID string, details map[string]any) {
	if s.emitter == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ev := busproducer.AuditEvent{
		TenantID:   tenantID,
		Actor:      r.Header.Get(ActorHeader),
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    details,
	}
	if err := s.emitter.Emit(ctx, ev); err != nil {
		s.logger.Warn("notifier api: audit emit", "err", err, "action", action)
	}
}
