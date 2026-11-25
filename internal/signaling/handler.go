package signaling

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

const (
	maxOfferSize  = 64 << 10
	answerTimeout = 10 * time.Second
)

type Answerer interface {
	Answer(ctx context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error)
}

func NewHandler(token string, answerer Answerer, static http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorized(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var offer webrtc.SessionDescription
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxOfferSize)).Decode(&offer); err != nil ||
			offer.Type != webrtc.SDPTypeOffer {
			http.Error(w, "expected an SDP offer", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), answerTimeout)
		defer cancel()
		answer, err := answerer.Answer(ctx, offer)
		if err != nil {
			http.Error(w, "could not answer the offer", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(answer)
	})
	if static != nil {
		mux.Handle("/", static)
	}
	return mux
}

func authorized(r *http.Request, token string) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}
