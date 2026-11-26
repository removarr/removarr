package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/executor"
	"github.com/removarr/removarr/internal/gate"
	"github.com/removarr/removarr/internal/store"
)

func TestExecutePassesThePlanAndReturnsTheRecords(t *testing.T) {
	h := newHarness(t, nil)
	h.executor.result = executor.Result{PlanID: "abc", DryRun: true, Actions: []domain.Action{{ID: 1, PlanID: "abc", Outcome: domain.OutcomeDryRun}}}
	rec := h.do(http.MethodPost, "/api/plans/abc/execute", "{}")
	if rec.Code != http.StatusOK || h.executor.plan != "abc" {
		t.Fatalf("status = %d %s, plan %q", rec.Code, rec.Body, h.executor.plan)
	}
	if got := decode[executor.Result](t, rec); got.PlanID != "abc" || len(got.Actions) != 1 {
		t.Errorf("response = %+v", got)
	}
}

func TestExecuteRefusals(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{&gate.BusyError{Job: gate.Scan}, http.StatusConflict},
		{store.ErrPlanNotFound, http.StatusNotFound},
		{fmt.Errorf("take: %w", store.ErrPlanExpired), http.StatusGone},
		{errors.New("disk full"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		h := newHarness(t, nil)
		h.executor.err = c.err
		if rec := h.do(http.MethodPost, "/api/plans/abc/execute", "{}"); rec.Code != c.code {
			t.Errorf("%v: status = %d, want %d", c.err, rec.Code, c.code)
		}
	}
}

func TestExecuteGoesThroughTheGuard(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(http.MethodPost, "/api/plans/abc/execute", "{}", func(r *http.Request) { r.Header.Del(RequestHeader) })
	if rec.Code != http.StatusForbidden || h.executor.plan != "" {
		t.Errorf("status = %d, executed %q; want 403 and nothing executed", rec.Code, h.executor.plan)
	}
}

func TestActionsArePagedNewestFirst(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	at := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if _, err := h.store.RecordAction(ctx, domain.Action{PlanID: "p", At: at, Outcome: domain.OutcomeDone, Rows: []domain.ActionRow{}, Steps: []domain.StepResult{}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	type page struct {
		Actions []domain.Action `json:"actions"`
		Next    *int64          `json:"next"`
	}
	first := decode[page](t, h.do(http.MethodGet, "/api/actions?limit=2", ""))
	if len(first.Actions) != 2 || first.Actions[0].ID != 3 || first.Next == nil || *first.Next != 2 {
		t.Fatalf("first page = %+v", first)
	}
	second := decode[page](t, h.do(http.MethodGet, fmt.Sprintf("/api/actions?limit=2&before=%d", *first.Next), ""))
	if len(second.Actions) != 1 || second.Actions[0].ID != 1 || second.Next != nil {
		t.Errorf("second page = %+v", second)
	}
	for _, q := range []string{"limit=0", "limit=201", "limit=x", "before=-1", "before=x"} {
		if rec := h.do(http.MethodGet, "/api/actions?"+q, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", q, rec.Code)
		}
	}
}
