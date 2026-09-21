package v2

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sipcapture/gossipper/internal/gateway"
	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/supervisor"
	"github.com/sipcapture/gossipper/internal/uistore"
)

func (s *Server) handleGetGateway(w http.ResponseWriter, r *http.Request) {
	if !s.requireGateway(w) {
		return
	}
	s.writeJSON(w, http.StatusOK, s.cfg.Gateway.Snapshot())
}

func (s *Server) handleListGateways(w http.ResponseWriter, r *http.Request) {
	if !s.requireGateway(w) {
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"gateways": s.cfg.Gateway.List()})
}

// discoverIPs is the Autodiscover backend; tests replace it.
var discoverIPs = gateway.DiscoverIPs

func (s *Server) handleGetGatewayIPs(w http.ResponseWriter, r *http.Request) {
	if !s.requireGateway(w) {
		return
	}
	s.writeJSON(w, http.StatusOK, discoverIPs(r.Context()))
}

func (s *Server) decodeGatewayConfig(r *http.Request) (gateway.Config, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err != nil {
		return gateway.Config{}, err
	}
	return gateway.DecodeConfig(raw)
}

func (s *Server) handlePutGateway(w http.ResponseWriter, r *http.Request) {
	if !s.requireGateway(w) {
		return
	}
	body, err := s.decodeGatewayConfig(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.Gateway.UpdateFirst(body); err != nil {
		s.writeGatewayErr(w, err)
		return
	}
	s.writeAudit(r.Context(), r, "gateway.update", body.AOR(), "")
	s.writeJSON(w, http.StatusOK, s.cfg.Gateway.Snapshot())
}

func (s *Server) handleCreateGateway(w http.ResponseWriter, r *http.Request) {
	if !s.requireGateway(w) {
		return
	}
	body, err := s.decodeGatewayConfig(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	snap, err := s.cfg.Gateway.Create(body)
	if err != nil {
		s.writeGatewayErr(w, err)
		return
	}
	s.writeAudit(r.Context(), r, "gateway.create", snap.Config.ID, "")
	s.writeJSON(w, http.StatusCreated, snap)
}

func (s *Server) handleGetGatewayProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireGateway(w) {
		return
	}
	snap, err := s.cfg.Gateway.Get(pathID(r))
	if err != nil {
		s.writeGatewayErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handlePutGatewayProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireGateway(w) {
		return
	}
	id := pathID(r)
	body, err := s.decodeGatewayConfig(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.Gateway.Update(id, body); err != nil {
		s.writeGatewayErr(w, err)
		return
	}
	s.writeAudit(r.Context(), r, "gateway.update", id, "")
	snap, err := s.cfg.Gateway.Get(id)
	if err != nil {
		s.writeGatewayErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleDeleteGateway(w http.ResponseWriter, r *http.Request) {
	if !s.requireGateway(w) {
		return
	}
	id := pathID(r)
	if err := s.cfg.Gateway.Delete(id); err != nil {
		s.writeGatewayErr(w, err)
		return
	}
	s.writeAudit(r.Context(), r, "gateway.delete", id, "")
	w.WriteHeader(http.StatusNoContent)
}

type armGatewayBody struct {
	ScenarioID string `json:"scenario_id"`
}

func (s *Server) handleArmGateway(w http.ResponseWriter, r *http.Request) {
	s.handleArmGatewayProfile(w, r, "")
}

func (s *Server) handleArmGatewayByID(w http.ResponseWriter, r *http.Request) {
	s.handleArmGatewayProfile(w, r, pathID(r))
}

func (s *Server) handleArmGatewayProfile(w http.ResponseWriter, r *http.Request, profileID string) {
	if !s.requireGateway(w) {
		return
	}
	var body armGatewayBody
	if err := s.decodeJSON(r, &body, 1<<16); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := strings.TrimSpace(body.ScenarioID)
	if id == "" {
		id = "uas"
	}
	sc, err := s.loadNamedOrStore(id)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if profileID == "" {
		err = s.cfg.Gateway.ArmScenario(id, sc)
	} else {
		err = s.cfg.Gateway.ArmProfileScenario(profileID, id, sc)
	}
	if err != nil {
		s.writeGatewayErr(w, err)
		return
	}
	s.writeAudit(r.Context(), r, "gateway.arm", id, profileID)
	if profileID == "" {
		s.writeJSON(w, http.StatusOK, s.cfg.Gateway.Snapshot())
		return
	}
	snap, err := s.cfg.Gateway.Get(profileID)
	if err != nil {
		s.writeGatewayErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, snap)
}

type originateGatewayBody struct {
	ScenarioID string `json:"scenario_id"`
	To         string `json:"to"`
	TotalCalls int    `json:"total_calls,omitempty"`
}

func (s *Server) handleOriginateGateway(w http.ResponseWriter, r *http.Request) {
	s.handleOriginateGatewayProfile(w, r, "")
}

func (s *Server) handleOriginateGatewayByID(w http.ResponseWriter, r *http.Request) {
	s.handleOriginateGatewayProfile(w, r, pathID(r))
}

func (s *Server) handleOriginateGatewayProfile(w http.ResponseWriter, r *http.Request, profileID string) {
	if !s.requireGateway(w) {
		return
	}
	if !s.requireRegistry(w) || !s.requireStore(w) {
		return
	}
	var body originateGatewayBody
	if err := s.decodeJSON(r, &body, 1<<16); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := strings.TrimSpace(body.ScenarioID)
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "scenario_id is required")
		return
	}
	sc, err := s.loadNamedOrStore(id)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := gateway.RequireUACOriginate(id, sc); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var overlay map[string]any
	if profileID == "" {
		overlay, err = s.cfg.Gateway.OriginateOverlay(body.To, body.TotalCalls)
		profileID = s.cfg.Gateway.Config().ID
		if profileID == "" {
			profileID = "gateway"
		}
	} else {
		overlay, err = s.cfg.Gateway.OriginateOverlayProfile(profileID, body.To, body.TotalCalls)
	}
	if err != nil {
		s.writeGatewayErr(w, err)
		return
	}
	_ = s.cfg.Gateway.RememberOriginate(profileID, id, body.To, body.TotalCalls)
	jobID := uuid.NewString()
	artifactsDir, err := s.cfg.Store.Layout().JobArtifactDir(jobID)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	argsJSON, err := supervisor.EncodeArgs(overlay)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	job := supervisor.Job{
		ID:           jobID,
		ProfileID:    profileID,
		ProfileKind:  supervisor.GatewayProfileKind,
		ScenarioID:   id,
		Status:       supervisor.StatusPending,
		ArgsJSON:     argsJSON,
		ArtifactsDir: artifactsDir,
		CreatedAt:    time.Now().UTC(),
	}
	spec := supervisor.Spec{
		JobID:        jobID,
		DataDir:      s.cfg.Store.Layout().Root,
		ProfileID:    profileID,
		ProfileKind:  supervisor.GatewayProfileKind,
		ScenarioID:   id,
		ArtifactsDir: artifactsDir,
		Engine:       overlay,
	}
	out, err := s.cfg.Registry.StartJob(r.Context(), job, spec)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeAudit(r.Context(), r, "gateway.originate", out.ID, id)
	s.writeJSON(w, http.StatusCreated, map[string]any{"job_id": out.ID})
}

func (s *Server) writeGatewayErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gateway.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, gateway.ErrDuplicateID):
		s.writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, gateway.ErrInvalidID), errors.Is(err, gateway.ErrUseOriginate):
		s.writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) loadNamedOrStore(id string) (scenario.Scenario, error) {
	sc, err := scenario.LoadNamed(id)
	if err == nil {
		return sc, nil
	}
	if s.cfg.Store == nil {
		return scenario.Scenario{}, err
	}
	body, gerr := s.cfg.Store.GetScenario(id)
	if gerr != nil {
		if errors.Is(gerr, uistore.ErrNotFound) {
			return scenario.Scenario{}, err
		}
		return scenario.Scenario{}, gerr
	}
	return scenario.ParseString(body.XML)
}
