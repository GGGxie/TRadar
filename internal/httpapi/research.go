package httpapi

import (
	"database/sql"
	"fmt"
	"net/http"

	"tradar/internal/fault"
	"tradar/internal/research"
	"tradar/internal/storage"
)

func (s *Server) researchEntries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	page, size, err := pagination(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	filter := research.EntryFilter{
		Stage:    r.URL.Query().Get("stage"),
		Decision: r.URL.Query().Get("decision"),
		Tag:      r.URL.Query().Get("tag"),
		Query:    r.URL.Query().Get("q"),
		Page:     page,
		PageSize: size,
	}
	if raw := r.URL.Query().Get("isStarred"); raw != "" {
		if raw != "true" && raw != "false" {
			writeError(w, r, fault.Field("isStarred", "需要 true 或 false"))
			return
		}
		value := raw == "true"
		filter.IsStarred = &value
	}
	items, total, err := research.ListEntries(r.Context(), s.db, filter)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: items, Pagination: &pageInfo{page, size, total, (total + size - 1) / size}})
}

func (s *Server) projectResearch(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	switch r.Method {
	case http.MethodGet:
		entry, err := research.GetEntry(r.Context(), s.db, projectID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, entry.Revision))
		writeJSON(w, http.StatusOK, response{Data: entry})
	case http.MethodPost:
		input, err := decodeEntryPatch(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		s.mutate(w, r, "research:create:"+projectID, input, func(tx *sql.Tx) (int, any, error) {
			entry, createErr := research.CreateEntry(r.Context(), tx, projectID, input, s.now())
			return http.StatusCreated, entry, createErr
		})
	case http.MethodPatch:
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		input, err := decodeEntryPatch(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var entry research.Entry
		err = storage.WithTx(r.Context(), s.db, func(tx *sql.Tx) error {
			var updateErr error
			entry, updateErr = research.UpdateEntry(r.Context(), tx, projectID, expected, input, s.now())
			return updateErr
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, entry.Revision))
		writeJSON(w, http.StatusOK, response{Data: entry})
	default:
		methodError(w, r)
	}
}

func decodeEntryPatch(r *http.Request) (research.EntryPatch, error) {
	var input research.EntryPatch
	fields, err := decodeObject(r, &input, map[string]bool{"nextActionAt": true})
	if err != nil {
		return input, err
	}
	_, input.NextActionAtSet = fields["nextActionAt"]
	return input, nil
}
