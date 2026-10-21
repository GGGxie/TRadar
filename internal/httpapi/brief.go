package httpapi

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"tradar/internal/brief"
	"tradar/internal/storage"
)

func (s *Server) briefs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		page, size, err := pagination(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		items, total, err := brief.ListBriefs(r.Context(), s.db, brief.BriefFilter{
			Date: r.URL.Query().Get("date"), TimeZone: r.URL.Query().Get("timeZone"),
			Page: page, PageSize: size,
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, response{Data: items, Pagination: &pageInfo{page, size, total, (total + size - 1) / size}})
	case http.MethodPost:
		var input brief.CreateInput
		if err := decode(r, &input); err != nil {
			writeError(w, r, err)
			return
		}
		var createdRun brief.Run
		created := s.mutateResponse(w, r, "briefs:create", input, func(tx *sql.Tx) (int, any, error) {
			run, err := brief.AcceptRun(r.Context(), tx, input, s.now())
			createdRun = run
			return http.StatusAccepted, run, err
		}, func(header http.Header, body []byte) {
			var stored struct {
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if json.Unmarshal(body, &stored) == nil && stored.Data.ID != "" {
				header.Set("Location", "/api/v1/brief-runs/"+stored.Data.ID)
			}
		})
		if created && s.config.QueueBriefRun != nil {
			if err := s.config.QueueBriefRun(createdRun); err != nil {
				slog.Error("queue brief run failed", "runId", createdRun.ID, "error", err)
			}
		}
	default:
		methodError(w, r)
	}
}

func (s *Server) briefRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	run, err := brief.GetRun(r.Context(), s.db, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, run.Revision))
	writeJSON(w, http.StatusOK, response{Data: run})
}

func (s *Server) briefByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	item, err := brief.GetBrief(r.Context(), s.db, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: item})
}

func (s *Server) briefMarkdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	item, err := brief.GetBrief(r.Context(), s.db, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="tradar-brief-%s-v%d.md"`, item.Date, item.Version))
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(brief.RenderMarkdown(item)))
}

func (s *Server) briefSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := brief.GetSettings(r.Context(), s.db)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", strconv.Quote(strconv.Itoa(settings.Revision)))
		writeJSON(w, http.StatusOK, response{Data: settings})
	case http.MethodPatch:
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var patch brief.SettingsPatch
		if err = decode(r, &patch); err != nil {
			writeError(w, r, err)
			return
		}
		var settings brief.Settings
		err = storage.WithTx(r.Context(), s.db, func(tx *sql.Tx) error {
			var updateErr error
			settings, updateErr = brief.UpdateSettings(r.Context(), tx, expected, patch)
			return updateErr
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", strconv.Quote(strconv.Itoa(settings.Revision)))
		writeJSON(w, http.StatusOK, response{Data: settings})
	default:
		methodError(w, r)
	}
}
