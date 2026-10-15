package httpapi

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"tradar/internal/fault"
	"tradar/internal/ingestion"
	"tradar/internal/storage"
)

func (s *Server) credentials() ingestion.Credentials {
	return ingestion.Credentials{
		GitHub:      s.config.HasGitHubCredentials,
		ProductHunt: s.config.HasProductHuntCredentials,
	}
}

func (s *Server) remoteSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	page, size, err := pagination(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, total, err := ingestion.ListSourceViews(r.Context(), s.db, ingestion.SourceViewFilter{Page: page, PageSize: size}, s.credentials())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: items, Pagination: &pageInfo{page, size, total, (total + size - 1) / size}})
}

func (s *Server) remoteSource(w http.ResponseWriter, r *http.Request) {
	source := ingestion.RemoteSource(r.PathValue("source"))
	switch r.Method {
	case http.MethodGet:
		view, err := ingestion.GetSourceView(r.Context(), s.db, source, s.credentials())
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, view.Revision))
		writeJSON(w, http.StatusOK, response{Data: view})
	case http.MethodPatch:
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var patch ingestion.SourceConfigPatch
		if err = decode(r, &patch); err != nil {
			writeError(w, r, err)
			return
		}
		credentials := s.credentials()
		hasCredentials := source == ingestion.GitHub && credentials.GitHub || source == ingestion.ProductHunt && credentials.ProductHunt
		var view ingestion.SourceView
		err = storage.WithTx(r.Context(), s.db, func(tx *sql.Tx) error {
			if _, updateErr := ingestion.UpdateSourceConfig(r.Context(), tx, source, expected, patch, hasCredentials, s.now()); updateErr != nil {
				return updateErr
			}
			var viewErr error
			view, viewErr = ingestion.GetSourceView(r.Context(), tx, source, credentials)
			return viewErr
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, view.Revision))
		writeJSON(w, http.StatusOK, response{Data: view})
	default:
		methodError(w, r)
	}
}

type createIngestionRunInput struct {
	Source ingestion.RemoteSource `json:"source"`
}

func (s *Server) ingestionRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		page, size, err := pagination(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		items, total, err := ingestion.ListRuns(r.Context(), s.db, ingestion.RunFilter{
			Source: ingestion.RemoteSource(r.URL.Query().Get("source")),
			Status: ingestion.RunStatus(r.URL.Query().Get("status")),
			Page:   page, PageSize: size,
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, response{Data: items, Pagination: &pageInfo{page, size, total, (total + size - 1) / size}})
	case http.MethodPost:
		var input createIngestionRunInput
		if err := decode(r, &input); err != nil {
			writeError(w, r, err)
			return
		}
		credentials := s.credentials()
		hasCredentials := input.Source == ingestion.GitHub && credentials.GitHub || input.Source == ingestion.ProductHunt && credentials.ProductHunt
		var createdRun ingestion.IngestionRun
		created := s.mutateResponse(w, r, "ingestion-runs:create", input, func(tx *sql.Tx) (int, any, error) {
			run, err := ingestion.CreateRun(r.Context(), tx, ingestion.CreateRunInput{
				Source: input.Source, Trigger: ingestion.TriggerManual, HasCredentials: hasCredentials,
			}, s.now())
			createdRun = run
			return http.StatusAccepted, run, err
		}, func(header http.Header, body []byte) {
			var stored struct {
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if json.Unmarshal(body, &stored) == nil && stored.Data.ID != "" {
				header.Set("Location", "/api/v1/ingestion-runs/"+stored.Data.ID)
			}
		})
		if created && s.config.QueueIngestionRun != nil {
			if err := s.config.QueueIngestionRun(createdRun); err != nil {
				slog.Error("queue ingestion run failed", "runId", createdRun.ID, "source", createdRun.Source, "error", err)
			}
		}
	default:
		methodError(w, r)
	}
}

func (s *Server) ingestionRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	if r.PathValue("id") == "" {
		writeError(w, r, fault.Missing("采集任务"))
		return
	}
	run, err := ingestion.GetRun(r.Context(), s.db, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: run})
}
