package httpapi

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

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

func (s *Server) projectExperiments(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	switch r.Method {
	case http.MethodGet:
		page, size, err := pagination(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		items, total, err := research.ListExperiments(r.Context(), s.db, projectID, research.ExperimentFilter{
			Status: r.URL.Query().Get("status"), Page: page, PageSize: size,
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, response{Data: items, Pagination: &pageInfo{page, size, total, (total + size - 1) / size}})
	case http.MethodPost:
		input, err := decodeExperimentPatch(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		s.mutate(w, r, "experiment:create:"+projectID, input, func(tx *sql.Tx) (int, any, error) {
			item, createErr := research.CreateExperiment(r.Context(), tx, projectID, input, s.now())
			return http.StatusCreated, item, createErr
		})
	default:
		methodError(w, r)
	}
}

func (s *Server) experiment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	switch r.Method {
	case http.MethodGet:
		item, err := research.GetExperiment(r.Context(), s.db, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, item.Revision))
		writeJSON(w, http.StatusOK, response{Data: item})
	case http.MethodPatch:
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		input, err := decodeExperimentPatch(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var item research.Experiment
		err = storage.WithTx(r.Context(), s.db, func(tx *sql.Tx) error {
			var updateErr error
			item, updateErr = research.UpdateExperiment(r.Context(), tx, id, expected, input, s.now())
			return updateErr
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, item.Revision))
		writeJSON(w, http.StatusOK, response{Data: item})
	default:
		methodError(w, r)
	}
}

func decodeExperimentPatch(r *http.Request) (research.ExperimentPatch, error) {
	var input research.ExperimentPatch
	fields, err := decodeObject(r, &input, map[string]bool{"actualMinutes": true, "outcome": true})
	if err != nil {
		return input, err
	}
	_, input.ActualMinutesSet = fields["actualMinutes"]
	_, input.OutcomeSet = fields["outcome"]
	return input, nil
}

func (s *Server) researchPreferences(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		preferences, err := research.GetPreferences(r.Context(), s.db)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, preferences.Revision))
		writeJSON(w, http.StatusOK, response{Data: preferences})
	case http.MethodPatch:
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var patch research.PreferencesPatch
		if err = decode(r, &patch); err != nil {
			writeError(w, r, err)
			return
		}
		var preferences research.Preferences
		err = storage.WithTx(r.Context(), s.db, func(tx *sql.Tx) error {
			var updateErr error
			preferences, updateErr = research.UpdatePreferences(r.Context(), tx, expected, patch)
			return updateErr
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, preferences.Revision))
		writeJSON(w, http.StatusOK, response{Data: preferences})
	default:
		methodError(w, r)
	}
}

// researchRhythm 返回认知库的研究节奏进度（#48）。timeZone 为 IANA 名称，缺省 Asia/Shanghai。
func (s *Server) researchRhythm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	zone := r.URL.Query().Get("timeZone")
	if zone == "" {
		zone = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(zone)
	if err != nil || zone == "Local" {
		writeError(w, r, fault.Field("timeZone", "需要有效的 IANA 时区"))
		return
	}
	rhythm, err := research.GetRhythm(r.Context(), s.db, s.now(), location)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: rhythm})
}
