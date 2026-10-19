package httpapi

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"tradar/internal/analysis"
	"tradar/internal/catalog"
	"tradar/internal/fault"
)

func (s *Server) projectAnalyses(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	switch r.Method {
	case http.MethodGet:
		page, size, err := pagination(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		filter := analysis.VersionFilter{Level: r.URL.Query().Get("level"), Page: page, PageSize: size}
		if raw := r.URL.Query().Get("cutoffAt"); raw != "" {
			cutoff, parseErr := time.Parse(time.RFC3339Nano, raw)
			if parseErr != nil {
				writeError(w, r, fault.Field("cutoffAt", "需要 RFC3339 时间"))
				return
			}
			value := catalog.Timestamp(cutoff)
			filter.CutoffAt = &value
		}
		items, total, err := analysis.ListVersions(r.Context(), s.db, projectID, filter)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, response{Data: items, Pagination: &pageInfo{page, size, total, (total + size - 1) / size}})
	case http.MethodPost:
		var input analysis.CreateInput
		if err := decode(r, &input); err != nil {
			writeError(w, r, err)
			return
		}
		s.mutate(w, r, "analyses:create:"+projectID, input, func(tx *sql.Tx) (int, any, error) {
			item, err := analysis.CreateManual(r.Context(), tx, projectID, input, s.now())
			return http.StatusCreated, item, err
		})
	default:
		methodError(w, r)
	}
}

func (s *Server) analysisVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	item, err := analysis.GetVersion(r.Context(), s.db, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: item})
}

func (s *Server) projectAnalysisRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodError(w, r)
		return
	}
	projectID := r.PathValue("id")
	var input analysis.CreateRunInput
	if err := decode(r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	var acceptedRun analysis.Run
	created := s.mutateResponse(w, r, "analysis-runs:create:"+projectID, input, func(tx *sql.Tx) (int, any, error) {
		var run analysis.Run
		var err error
		if input.Mode == "MODEL" {
			if s.config.AnalysisModel == nil {
				return http.StatusAccepted, run, fault.New("MODEL_DISABLED", "模型分析尚未启用")
			}
			run, err = s.config.AnalysisModel.Accept(r.Context(), tx, projectID, input, s.now())
		} else {
			run, err = analysis.CreateRulesRun(r.Context(), tx, projectID, input, s.now())
		}
		acceptedRun = run
		return http.StatusAccepted, run, err
	}, func(header http.Header, body []byte) {
		var stored struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &stored) == nil && stored.Data.ID != "" {
			header.Set("Location", "/api/v1/analysis-runs/"+stored.Data.ID)
		}
	})
	if created && acceptedRun.Mode == "MODEL" {
		if err := s.config.AnalysisModel.Enqueue(acceptedRun); err != nil {
			slog.Error("queue model analysis failed", "runId", acceptedRun.ID, "error", err)
		}
	}
}

func (s *Server) analysisRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	run, err := analysis.GetRun(r.Context(), s.db, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, run.Revision))
	writeJSON(w, http.StatusOK, response{Data: run})
}

func (s *Server) analysisCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	capabilities := analysis.CurrentCapabilities()
	if s.config.AnalysisModel != nil {
		capabilities = s.config.AnalysisModel.Capabilities()
	}
	writeJSON(w, http.StatusOK, response{Data: capabilities})
}
