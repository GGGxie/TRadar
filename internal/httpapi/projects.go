package httpapi

import (
	"database/sql"
	"fmt"
	"net/http"
	"tradar/internal/catalog"
	"tradar/internal/storage"
)

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		page, size, err := pagination(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		items, total, err := catalog.ListProjects(r.Context(), s.db, catalog.ProjectFilter{Page: page, PageSize: size, Query: r.URL.Query().Get("q"), Category: r.URL.Query().Get("category")})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, response{Data: items, Pagination: &pageInfo{page, size, total, (total + size - 1) / size}})
	case "POST":
		var input catalog.CreateProjectInput
		if err := decode(r, &input); err != nil {
			writeError(w, r, err)
			return
		}
		s.mutate(w, r, "projects:create", input, func(tx *sql.Tx) (int, any, error) {
			p, created, err := catalog.CreateProject(r.Context(), tx, input, s.now())
			status := 201
			if !created {
				status = 200
			}
			return status, p, err
		})
	default:
		methodError(w, r)
	}
}
func (s *Server) project(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	switch r.Method {
	case "GET":
		p, err := catalog.GetProject(r.Context(), s.db, id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, p.Revision))
		writeJSON(w, 200, response{Data: p})
	case "PATCH":
		expected, err := revision(r)
		if err != nil {
			writeError(w, r, err)
			return
		}
		var input catalog.ProjectPatch
		if err = decode(r, &input); err != nil {
			writeError(w, r, err)
			return
		}
		var p catalog.Project
		err = storage.WithTx(r.Context(), s.db, func(tx *sql.Tx) error {
			var err error
			p, err = catalog.UpdateProject(r.Context(), tx, id, expected, input, s.now())
			return err
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, p.Revision))
		writeJSON(w, 200, response{Data: p})
	default:
		methodError(w, r)
	}
}
