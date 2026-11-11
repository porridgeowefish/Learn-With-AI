package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/inference"
	"github.com/xmz14/lll/backend-go/internal/cloud/routing"
	"github.com/xmz14/lll/backend-go/internal/cloud/store"
	"github.com/xmz14/lll/backend-go/internal/modules/ai"
)

type ManagedConnection struct {
	ID, Model, QuotaGroup, Kind, BaseURL, UpstreamModel, APIKeyEnv string
	Priority, Weight                                               int
	Limits                                                         routing.Limits
	Cost                                                           inference.CostRate
	ToolMode                                                       string `json:",omitempty"`
}

type ModelConfig struct {
	Models      []Model
	Connections []ManagedConnection
	QuotaGroups map[string]routing.Limits
	GuestModels []GuestModel
}

func publishedRouteRevision(version int64) string {
	return "published:" + strconv.FormatInt(version, 10)
}

// SetStartupModelRevision identifies the initial gateway before it serves requests.
func (s *Server) SetStartupModelRevision(config ModelConfig, version int64) error {
	guest, err := guestModelCatalog(config.GuestModels, config.Models)
	if err != nil {
		return err
	}
	s.guestModels = guest
	if version > 0 {
		s.routeRevision = publishedRouteRevision(version)
		return nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	s.routeRevision = fmt.Sprintf("file:%x", sha256.Sum256(raw))
	return nil
}

type connectionTarget struct {
	id, model, protocol, endpoint, upstreamModel, credential string
}

func (c ManagedConnection) target() connectionTarget {
	return connectionTarget{c.ID, c.Model, c.Kind, c.BaseURL, c.UpstreamModel, c.APIKeyEnv}
}

func (c ManagedConnection) requiresCacheUsage(rate Rate) bool {
	return c.Cost.RequiresCacheUsage() || rate.Input != rate.Cached
}

func modelIndex(models []Model) (map[string]Model, error) {
	out := make(map[string]Model, len(models))
	for _, model := range models {
		if !model.valid() || out[model.ID].ID != "" {
			return nil, errors.New("invalid or duplicate managed model")
		}
		out[model.ID] = model
	}
	return out, nil
}

type ModelBuilder func(ModelConfig) (*inference.Gateway, error)

// NewModelBuilder limits configured connections to operator-allowlisted hosts
// and resolves secrets from the service environment instead of config records.
func NewModelBuilder(hosts []string, secret func(string) string) ModelBuilder {
	allowed := make(map[string]bool, len(hosts))
	for _, host := range hosts {
		allowed[strings.ToLower(host)] = true
	}
	return func(cfg ModelConfig) (*inference.Gateway, error) {
		if len(cfg.Models) > 64 || len(cfg.Connections) > 256 || len(cfg.QuotaGroups) > 256 {
			return nil, errors.New("managed catalog exceeds capacity")
		}
		models, err := modelIndex(cfg.Models)
		if err != nil {
			return nil, err
		}
		counts := make(map[string]int)
		upstreams := make([]inference.Upstream, 0, len(cfg.Connections))
		for _, c := range cfg.Connections {
			if c.ToolMode != "" && c.ToolMode != "text" && c.ToolMode != "tools" {
				return nil, fmt.Errorf("connection %q has invalid tool capability", c.ID)
			}
			endpoint, err := url.Parse(c.BaseURL)
			if err != nil || endpoint.Scheme != "https" || !allowed[strings.ToLower(endpoint.Hostname())] ||
				net.ParseIP(endpoint.Hostname()) != nil || (endpoint.Port() != "" && endpoint.Port() != "443") {
				return nil, fmt.Errorf("connection %q uses an unapproved upstream host", c.ID)
			}
			upstreamKey := strings.HasPrefix(c.APIKeyEnv, "LWA_CLOUD_UPSTREAM_") ||
				strings.HasPrefix(c.APIKeyEnv, "LLL_CLOUD_UPSTREAM_")
			if !identifier.MatchString(c.ID) || !identifier.MatchString(c.APIKeyEnv) || !upstreamKey ||
				models[c.Model].ID == "" {
				return nil, fmt.Errorf("invalid model or credential reference for connection %q", c.ID)
			}
			key := secret(c.APIKeyEnv)
			if key == "" {
				return nil, fmt.Errorf("missing API credential for connection %q", c.ID)
			}
			counts[c.Model]++
			upstreams = append(upstreams, inference.Upstream{
				Connection: routing.Connection{ID: c.ID, Model: c.Model, QuotaGroup: c.QuotaGroup, Priority: c.Priority, Weight: c.Weight,
					Limits: c.Limits, TextOnly: c.ToolMode == "text"},
				Provider: ai.ProviderConfig{Kind: c.Kind, BaseURL: c.BaseURL, Model: c.UpstreamModel, APIKey: key}, Cost: c.Cost,
				ModelMaxOutput:    models[c.Model].MaxOutput,
				RequireCacheUsage: c.requiresCacheUsage(models[c.Model].Rate),
			})
		}
		if len(counts) != len(models) {
			return nil, errors.New("every published model needs an upstream connection")
		}
		return inference.New(upstreams, cfg.QuotaGroups)
	}
}

func (s *Server) EnableModelAdmin(builder ModelBuilder) {
	s.enableModelAdmin(builder, func(g *inference.Gateway, ctx context.Context, connection, mode string) (inference.ProbeResult, error) {
		switch mode {
		case "usage":
			return g.ProbeUsage(ctx, connection)
		case "tools":
			return g.ProbeTools(ctx, connection)
		default:
			return g.Probe(ctx, connection)
		}
	})
}

func (s *Server) enableModelAdmin(builder ModelBuilder, probe func(*inference.Gateway, context.Context, string, string) (inference.ProbeResult, error)) {
	const verificationTTL = 15 * time.Minute
	var verificationMu sync.Mutex
	type verification struct {
		expires, usageExpires, toolExpires time.Time
		cacheBreakdown                     bool
	}
	verified := make(map[connectionTarget]verification)
	usageProbeSlot := make(chan struct{}, 1)
	s.mux.HandleFunc("GET /v1/admin/models/capacity", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.adminAuthorize(w, r, false); !ok {
			return
		}
		s.modelMu.RLock()
		gateway, ok := s.gateway.(*inference.Gateway)
		revision := s.routeRevision
		s.modelMu.RUnlock()
		if !ok {
			fail(w, http.StatusServiceUnavailable, "model_capacity_unavailable")
			return
		}
		usage, err := s.db.ConnectionUsage(r.Context(), time.Now().Add(-24*time.Hour))
		if err != nil {
			fail(w, http.StatusInternalServerError, "metrics_unavailable")
			return
		}
		respond(w, http.StatusOK, struct {
			inference.Capacity
			Revision string                  `json:"revision"`
			Usage    []store.ConnectionUsage `json:"usage"`
		}{gateway.Capacity(), revision, usage})
	})
	s.mux.HandleFunc("GET /v1/admin/models/route-usage", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.adminAuthorize(w, r, false); !ok {
			return
		}
		days, err := strconv.Atoi(r.URL.Query().Get("days"))
		if err != nil {
			days = 7
		}
		rows, err := s.db.RouteUsage(r.Context(), time.Now(), days)
		if err != nil {
			fail(w, http.StatusInternalServerError, "metrics_unavailable")
			return
		}
		respond(w, http.StatusOK, rows)
	})
	s.mux.HandleFunc("POST /v1/admin/models/probe", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := s.adminAuthorize(w, r, true)
		if !ok || !s.allowAuth(w, r, "model-probe", admin.ID, 10) {
			return
		}
		var in struct {
			ConnectionID string
			Config       *ModelConfig
			Mode         string
		}
		if !decode(w, r, &in, 1<<20) {
			return
		}
		if !identifier.MatchString(in.ConnectionID) {
			fail(w, http.StatusBadRequest, "invalid_connection_id")
			return
		}
		if in.Mode != "" && in.Mode != "catalog" && in.Mode != "usage" && in.Mode != "tools" {
			fail(w, http.StatusBadRequest, "invalid_probe_mode")
			return
		}
		var gateway *inference.Gateway
		if in.Config != nil {
			var err error
			gateway, err = builder(*in.Config)
			if err != nil {
				fail(w, http.StatusBadRequest, "invalid_model_configuration")
				return
			}
		} else {
			s.modelMu.RLock()
			gateway, _ = s.gateway.(*inference.Gateway)
			s.modelMu.RUnlock()
		}
		if gateway == nil {
			fail(w, http.StatusServiceUnavailable, "model_configuration_unavailable")
			return
		}
		if in.Mode == "usage" || in.Mode == "tools" {
			if !s.allowAuth(w, r, "model-usage-probe", admin.ID, 3) {
				return
			}
			select {
			case usageProbeSlot <- struct{}{}:
				defer func() { <-usageProbeSlot }()
			default:
				fail(w, http.StatusConflict, "model_probe_busy")
				return
			}
		}
		result, err := probe(gateway, r.Context(), in.ConnectionID, in.Mode)
		if err != nil {
			fail(w, http.StatusBadRequest, "unknown_connection")
			return
		}
		if in.Config != nil {
			now := time.Now()
			verificationMu.Lock()
			for target, proof := range verified {
				if !now.Before(proof.expires) && !now.Before(proof.toolExpires) {
					delete(verified, target)
				}
			}
			for _, connection := range in.Config.Connections {
				if connection.ID == in.ConnectionID {
					target := connection.target()
					proof := verified[target]
					if in.Mode == "tools" {
						proof.toolExpires = time.Time{}
						if result.Status == "verified" {
							proof.toolExpires = now.Add(verificationTTL)
						}
					} else if result.Status == "verified" {
						proof.expires = now.Add(verificationTTL)
						if in.Mode == "usage" {
							proof.usageExpires = proof.expires
							proof.cacheBreakdown = result.CacheBreakdown
						}
					} else {
						proof = verification{}
					}
					verified[target] = proof
					break
				}
			}
			verificationMu.Unlock()
		}
		respond(w, http.StatusOK, result)
	})
	s.mux.HandleFunc("GET /v1/admin/models", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.adminAuthorize(w, r, true); !ok {
			return
		}
		active, err := s.db.ActiveModelConfig(r.Context())
		if err != nil {
			fail(w, http.StatusInternalServerError, "model_configuration_unavailable")
			return
		}
		respond(w, http.StatusOK, active)
	})
	s.mux.HandleFunc("POST /v1/admin/models/publish", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := s.adminAuthorize(w, r, true)
		if !ok {
			return
		}
		var in struct {
			BaseVersion int64
			RollbackTo  int64
			Config      *ModelConfig
		}
		if !decode(w, r, &in, 1<<20) {
			return
		}
		if (in.RollbackTo > 0) == (in.Config != nil) {
			fail(w, http.StatusBadRequest, "invalid_model_configuration")
			return
		}
		if in.RollbackTo > 0 {
			old, err := s.db.ModelConfig(r.Context(), in.RollbackTo)
			if err != nil || json.Unmarshal(old.Config, &in.Config) != nil {
				fail(w, http.StatusNotFound, "model_configuration_unavailable")
				return
			}
		}
		gateway, err := builder(*in.Config)
		if err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_model_configuration", "detail": err.Error()})
			return
		}
		models, err := modelIndex(in.Config.Models)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid_model_configuration")
			return
		}
		if in.RollbackTo == 0 {
			active, err := s.db.ActiveModelConfig(r.Context())
			var before ModelConfig
			if err != nil || json.Unmarshal(active.Config, &before) != nil {
				fail(w, http.StatusInternalServerError, "model_configuration_unavailable")
				return
			}
			if active.Version != in.BaseVersion {
				fail(w, http.StatusConflict, "model_configuration_changed")
				return
			}
			if in.Config.GuestModels == nil {
				in.Config.GuestModels = before.GuestModels
			}
			previous := make(map[connectionTarget]ManagedConnection, len(before.Connections))
			for _, connection := range before.Connections {
				previous[connection.target()] = connection
			}
			previousRates := make(map[string]Rate, len(before.Models))
			for _, model := range before.Models {
				previousRates[model.ID] = model.Rate
			}
			var needsProbe, needsTools, needsCache []string
			modelUsage := make(map[string]bool, len(in.Config.Models))
			cacheRequired := make(map[string]bool)
			now := time.Now()
			verificationMu.Lock()
			for _, connection := range in.Config.Connections {
				target := connection.target()
				proof := verified[target]
				old, exists := previous[target]
				if !exists && !now.Before(proof.expires) {
					needsProbe = append(needsProbe, connection.ID)
				}
				if connection.ToolMode == "tools" && old.ToolMode != "tools" && !now.Before(proof.toolExpires) {
					needsTools = append(needsTools, connection.ID)
				}
				usageVerified := now.Before(proof.usageExpires)
				if usageVerified {
					modelUsage[connection.Model] = true
				}
				if connection.requiresCacheUsage(models[connection.Model].Rate) &&
					(!exists || !old.requiresCacheUsage(previousRates[connection.Model])) &&
					(!usageVerified || !proof.cacheBreakdown) {
					needsCache = append(needsCache, connection.ID)
					cacheRequired[connection.Model] = true
				}
			}
			verificationMu.Unlock()
			if len(needsProbe) > 0 {
				respond(w, http.StatusPreconditionFailed, map[string]any{"error": "model_probe_required", "connections": needsProbe})
				return
			}
			var needsUsage []string
			for _, model := range in.Config.Models {
				_, wasPublished := previousRates[model.ID]
				if cacheRequired[model.ID] || (!wasPublished && !modelUsage[model.ID]) {
					needsUsage = append(needsUsage, model.ID)
				}
			}
			if len(needsUsage) > 0 {
				respond(w, http.StatusPreconditionFailed, map[string]any{"error": "model_usage_probe_required", "models": needsUsage, "connections": needsCache})
				return
			}
			if len(needsTools) > 0 {
				respond(w, http.StatusPreconditionFailed, map[string]any{"error": "model_tool_probe_required", "connections": needsTools})
				return
			}
		}
		guest, err := guestModelCatalog(in.Config.GuestModels, in.Config.Models)
		if err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_model_configuration", "detail": err.Error()})
			return
		}
		if in.Config.GuestModels != nil {
			in.Config.GuestModels = guest
		}
		body, err := json.Marshal(in.Config)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid_model_configuration")
			return
		}
		if !s.modelMu.TryLock() {
			fail(w, http.StatusConflict, "model_configuration_busy")
			return
		}
		defer s.modelMu.Unlock()
		version, err := s.db.PublishModelConfig(r.Context(), in.BaseVersion, admin.ID, body, time.Now())
		if errors.Is(err, store.ErrConflict) {
			fail(w, http.StatusConflict, "model_configuration_changed")
			return
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, "model_configuration_unavailable")
			return
		}
		if previous, ok := s.gateway.(*inference.Gateway); ok {
			gateway.CarryCapacity(previous)
		}
		s.gateway, s.models, s.list = gateway, models, append([]Model{}, in.Config.Models...)
		s.guestModels = guest
		s.routeRevision = publishedRouteRevision(version)
		respond(w, http.StatusOK, map[string]int64{"version": version})
	})
}
