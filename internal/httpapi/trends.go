package httpapi

import (
	"net/http"
	"time"

	"tradar/internal/fault"
	"tradar/internal/trends"
)

func (s *Server) radar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	page, size, err := pagination(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	asOf, window, err := s.trendQuery(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, total, err := trends.ListRadar(r.Context(), s.db, trends.RadarFilter{
		Query: r.URL.Query().Get("q"), Category: r.URL.Query().Get("category"), Source: r.URL.Query().Get("source"),
		Window: window, AsOf: asOf, Page: page, PageSize: size,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: items, Pagination: &pageInfo{page, size, total, (total + size - 1) / size}})
}

func (s *Server) projectTrend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodError(w, r)
		return
	}
	asOf, window, err := s.trendQuery(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	result, err := trends.GetProjectTrend(r.Context(), s.db, r.PathValue("id"), asOf, window)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: result})
}

func (s *Server) trendQuery(r *http.Request) (time.Time, trends.Window, error) {
	now := s.now().UTC()
	asOf := now
	if raw := r.URL.Query().Get("asOf"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return time.Time{}, "", fault.Field("asOf", "需要 RFC3339 时间")
		}
		asOf = parsed.UTC()
		if asOf.After(now) {
			return time.Time{}, "", fault.Field("asOf", "不能晚于当前时间")
		}
	}
	window := trends.Window(r.URL.Query().Get("window"))
	if window == "" {
		window = trends.Window24Hours
	}
	if window != trends.Window24Hours && window != trends.Window7Days {
		return time.Time{}, "", fault.Field("window", "仅支持 24h 或 7d")
	}
	return asOf, window, nil
}
