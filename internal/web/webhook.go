package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Yatsuiii/spendlint/internal/ledger"
)

// mrEvent is the subset of a GitLab MR webhook payload we need.
type mrEvent struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		IID    int    `json:"iid"`
		Title  string `json:"title"`
		Action string `json:"action"`
		State  string `json:"state"`
	} `json:"object_attributes"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	// Optional token check.
	if s.cfg.WebhookSecret != "" {
		if r.Header.Get("X-Gitlab-Token") != s.cfg.WebhookSecret {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	var ev mrEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}

	// Only handle MR open/reopen/update events.
	if ev.ObjectKind != "merge_request" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	action := ev.ObjectAttributes.Action
	if action != "open" && action != "reopen" && action != "update" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	proj := ev.Project.PathWithNamespace
	iid := ev.ObjectAttributes.IID
	title := ev.ObjectAttributes.Title

	// Respond immediately; run the review in the background.
	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, "queued review for %s!%d\n", proj, iid)

	go func() {
		ctx := context.Background()
		ag, err := s.newAgent(ctx)
		if err != nil {
			log.Printf("agent init error for %s!%d: %v", proj, iid, err)
			return
		}
		comment, err := ag.ReviewMR(ctx, proj, iid)
		if err != nil {
			log.Printf("review error for %s!%d: %v", proj, iid, err)
			return
		}

		verdict, delta := extractVerdictDelta(comment)
		rv := &ledger.Review{
			Timestamp:   time.Now(),
			Project:     proj,
			MRIID:       iid,
			MRTitle:     title,
			Verdict:     verdict,
			DeltaDay:    delta,
			CommentBody: comment,
		}
		if err := s.cfg.Ledger.RecordReview(rv); err != nil {
			log.Printf("record review: %v", err)
		}
		log.Printf("review done %s!%d verdict=%s delta=%.4f", proj, iid, verdict, delta)
	}()
}

// extractVerdictDelta parses "PASS", "WARN", or "BLOCK" from the comment text.
func extractVerdictDelta(comment string) (string, float64) {
	upper := strings.ToUpper(comment)
	for _, verdict := range []string{"BLOCK", "WARN", "PASS", "INFO"} {
		if strings.Contains(upper, verdict) {
			return verdict, 0
		}
	}
	return "UNKNOWN", 0
}
