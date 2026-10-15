package httpapi

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"tradar/internal/fault"
	"tradar/internal/storage"
)

// mutate stores the response and the side effect in one serialized transaction.
// Asynchronous operations will commit only their intent here, then run separately.
func (s *Server) mutate(w http.ResponseWriter, r *http.Request, scope string, input any, fn func(*sql.Tx) (int, any, error)) {
	s.mutateResponse(w, r, scope, input, fn, nil)
}

func (s *Server) mutateResponse(w http.ResponseWriter, r *http.Request, scope string, input any, fn func(*sql.Tx) (int, any, error), beforeWrite func(http.Header, []byte)) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 1 || len(key) > 128 || strings.IndexFunc(key, func(c rune) bool { return c < 33 || c > 126 }) >= 0 {
		writeError(w, r, fault.Field("Idempotency-Key", "创建请求需要 1–128 字符的稳定重试标识"))
		return
	}
	mutationID := scope + "\x00" + key
	s.mutationMu.Lock()
	if _, active := s.activeMutations[mutationID]; active {
		s.mutationMu.Unlock()
		writeError(w, r, fault.New("IDEMPOTENCY_IN_PROGRESS", "此请求正在处理"))
		return
	}
	s.activeMutations[mutationID] = struct{}{}
	s.mutationMu.Unlock()
	defer func() {
		s.mutationMu.Lock()
		delete(s.activeMutations, mutationID)
		s.mutationMu.Unlock()
	}()
	canonical, err := json.Marshal(input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	sum := sha256.Sum256(canonical)
	hash := hex.EncodeToString(sum[:])
	status := 0
	var body []byte
	err = storage.WithTx(r.Context(), s.db, func(tx *sql.Tx) error {
		claimed, err := tx.ExecContext(r.Context(), "INSERT INTO idempotency(scope,key,request_hash,status,body) VALUES(?,?,?,0,'null') ON CONFLICT(scope,key) DO NOTHING", scope, key, hash)
		if err != nil {
			return err
		}
		count, err := claimed.RowsAffected()
		if err != nil {
			return err
		}
		if count == 0 {
			var storedHash string
			if err := tx.QueryRowContext(r.Context(), "SELECT request_hash,status,body FROM idempotency WHERE scope=? AND key=?", scope, key).Scan(&storedHash, &status, &body); err != nil {
				return err
			}
			if storedHash != hash {
				return fault.New("IDEMPOTENCY_MISMATCH", "同一重试标识不能用于不同内容")
			}
			if status == 0 {
				return fault.New("IDEMPOTENCY_IN_PROGRESS", "此请求正在处理")
			}
			return nil
		}
		var value any
		status, value, err = fn(tx)
		if err != nil {
			return err
		}
		body, err = json.Marshal(response{Data: value})
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(r.Context(), "UPDATE idempotency SET status=?,body=? WHERE scope=? AND key=?", status, body, scope, key)
		return err
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	var versioned struct {
		Data struct {
			Revision *int `json:"revision"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &versioned) == nil && versioned.Data.Revision != nil {
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, *versioned.Data.Revision))
	}
	if beforeWrite != nil {
		beforeWrite(w.Header(), body)
	}
	w.WriteHeader(status)
	w.Write(body)
}
