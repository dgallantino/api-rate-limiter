package httplimit

import (
	"net/http"
	"strconv"
)

const denyBody = `{"error":"too_many_requests"}`

func WriteDeny(w http.ResponseWriter, remaining, retryAfterMs int64) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
	if sec := retryAfterSeconds(retryAfterMs); sec > 0 {
		h.Set("Retry-After", strconv.FormatInt(sec, 10))
	}
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(denyBody))
}

func retryAfterSeconds(ms int64) int64 {
	if ms <= 0 {
		return 0
	}
	return (ms + 999) / 1000
}

func writeBadRequest(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte(`{"error":"bad_request"}`))
}
