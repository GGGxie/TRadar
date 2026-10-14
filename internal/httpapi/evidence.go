package httpapi

import (
	"database/sql"
	"net/http"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/fault"
)

type manualEvidenceInput struct {
	URL            string  `json:"url"`
	Title          string  `json:"title"`
	Excerpt        string  `json:"excerpt"`
	Kind           string  `json:"kind"`
	Publisher      *string `json:"publisher"`
	PublishedAt    *string `json:"publishedAt"`
	IsPublic       *bool   `json:"isPublic"`
	SourceRecordID *string `json:"sourceRecordId"`
	SupersedesID   *string `json:"supersedesId"`
}

func (input manualEvidenceInput) catalogInput() catalog.CreateEvidenceInput {
	return catalog.CreateEvidenceInput{
		URL: input.URL, Title: input.Title, Excerpt: input.Excerpt, Kind: input.Kind,
		Publisher: input.Publisher, PublishedAt: input.PublishedAt, IsPublic: input.IsPublic,
		SourceRecordID: input.SourceRecordID, SupersedesID: input.SupersedesID,
	}
}

func (s *Server) projectEvidence(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	switch r.Method {
	case "GET":
		page, size, err := pagination(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		filter := catalog.EvidenceFilter{Page: page, PageSize: size}
		if raw := r.URL.Query().Get("asOf"); raw != "" {
			asOf, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				writeError(w, r, fault.Field("asOf", "需要 RFC3339 时间"))
				return
			}
			filter.AsOf = &asOf
		}
		items, total, err := catalog.ListEvidence(r.Context(), s.db, projectID, filter)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, response{Data: items, Pagination: &pageInfo{page, size, total, (total + size - 1) / size}})
	case "POST":
		var input manualEvidenceInput
		if err := decode(r, &input); err != nil {
			writeError(w, r, err)
			return
		}
		s.mutate(w, r, "evidence:create:"+projectID, input, func(tx *sql.Tx) (int, any, error) {
			item, err := catalog.CreateManualEvidence(r.Context(), tx, projectID, input.catalogInput(), s.now())
			return http.StatusCreated, item, err
		})
	default:
		methodError(w, r)
	}
}
