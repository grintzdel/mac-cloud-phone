package signaling

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

type fakeAnswerer struct {
	offer webrtc.SessionDescription
	err   error
}

func (f *fakeAnswerer) Answer(_ context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	f.offer = offer
	if f.err != nil {
		return webrtc.SessionDescription{}, f.err
	}
	return webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: "v=0 answer"}, nil
}

const offerBody = `{"type":"offer","sdp":"v=0 offer"}`

func postOffer(h http.Handler, auth, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/offer", strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestOfferWithoutTokenIsUnauthorized(t *testing.T) {
	answerer := &fakeAnswerer{}
	rec := postOffer(NewHandler("secret", answerer, nil), "", offerBody)
	if rec.Code != http.StatusUnauthorized || answerer.offer.SDP != "" {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestOfferWithWrongTokenIsUnauthorized(t *testing.T) {
	rec := postOffer(NewHandler("secret", &fakeAnswerer{}, nil), "Bearer nope", offerBody)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestOfferWithValidTokenReturnsAnswer(t *testing.T) {
	answerer := &fakeAnswerer{}
	rec := postOffer(NewHandler("secret", answerer, nil), "Bearer secret", offerBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	if answerer.offer.Type != webrtc.SDPTypeOffer || answerer.offer.SDP != "v=0 offer" {
		t.Fatalf("offer not forwarded: %+v", answerer.offer)
	}
	var answer webrtc.SessionDescription
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil || answer.SDP != "v=0 answer" {
		t.Fatalf("bad answer %s", rec.Body)
	}
}

func TestOfferRejectsNonOfferDescriptions(t *testing.T) {
	rec := postOffer(NewHandler("secret", &fakeAnswerer{}, nil), "Bearer secret", `{"type":"answer","sdp":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestOfferReportsAnswererFailure(t *testing.T) {
	rec := postOffer(NewHandler("secret", &fakeAnswerer{err: errors.New("boom")}, nil), "Bearer secret", offerBody)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestOfferOnlyAcceptsPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/offer", nil)
	rec := httptest.NewRecorder()
	NewHandler("secret", &fakeAnswerer{}, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestPageIsServedWithoutToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	NewHandler("secret", &fakeAnswerer{}, http.FileServer(http.Dir("testdata"))).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "cloudphone") {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}
