package httpapi

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/fault"
	"tradar/internal/storage"
)

func (s *Server) projectSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	page, size, err := pagination(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, total, err := catalog.ListSources(r.Context(), s.db, r.PathValue("id"), catalog.SourceFilter{Page: page, PageSize: size})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: items, Pagination: &pageInfo{page, size, total, (total + size - 1) / size}})
}

type reassociateSourceInput struct {
	ProjectID string `json:"projectId"`
	Reason    string `json:"reason"`
}

func (s *Server) sourceRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		methodError(w, r)
		return
	}
	expected, err := revision(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input reassociateSourceInput
	if err = decode(r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	var item catalog.SourceRecord
	err = storage.WithTx(r.Context(), s.db, func(tx *sql.Tx) error {
		var err error
		item, err = catalog.ReassociateSource(r.Context(), tx, r.PathValue("id"), expected, input.ProjectID, input.Reason, s.now())
		return err
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%d"`, item.Revision))
	writeJSON(w, http.StatusOK, response{Data: item})
}

func (s *Server) projectObservations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	page, size, err := pagination(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	to := s.now().UTC()
	if raw := r.URL.Query().Get("to"); raw != "" {
		to, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(w, r, fault.Field("to", "需要 RFC3339 时间"))
			return
		}
	}
	from := to.Add(-30 * 24 * time.Hour)
	if raw := r.URL.Query().Get("from"); raw != "" {
		from, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(w, r, fault.Field("from", "需要 RFC3339 时间"))
			return
		}
	}
	filter := catalog.ObservationFilter{
		SourceRecordID: r.URL.Query().Get("sourceRecordId"),
		Metric:         r.URL.Query().Get("metric"),
		Basis:          r.URL.Query().Get("basis"),
		From:           from,
		To:             to,
		Page:           page,
		PageSize:       size,
	}
	items, total, err := catalog.ListObservations(r.Context(), s.db, r.PathValue("id"), filter)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: items, Pagination: &pageInfo{page, size, total, (total + size - 1) / size}})
}
