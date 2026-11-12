package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A spec list that sends fewer specs than its total (a paged answer) leaves
// the counts unknown rather than low.
func TestScheduleListWithSpecsNeedsTheWholeSpecList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/organizations/acme/schedules" {
			_, _ = io.WriteString(w, `{"items": [{"name": "nightly"}], "total": 1}`)
			return
		}
		_, _ = io.WriteString(w, `{"items": [{"code": "a", "schedule_name": "nightly"}], "total": 120}`)
	}))
	defer srv.Close()
	result, err := newTestService(t, srv).ScheduleListWithSpecs(context.Background(), "acme")
	if err != nil || result.SpecCountErr == nil || result.Schedules[0].SpecCount != nil {
		t.Errorf("result %+v, %v; want unknown counts and SpecCountErr", result, err)
	}
}

// A spec list that does not name each spec's schedule leaves the counts
// unknown rather than reading as a 0 for every schedule.
func TestScheduleListWithSpecsNeedsEachSpecsSchedule(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/organizations/acme/schedules" {
			_, _ = io.WriteString(w, `{"items": [{"name": "nightly"}], "total": 1}`)
			return
		}
		_, _ = io.WriteString(w, `{"items": [{"code": "a"}]}`)
	}))
	defer srv.Close()
	result, err := newTestService(t, srv).ScheduleListWithSpecs(context.Background(), "acme")
	if err != nil || result.SpecCountErr == nil || result.Schedules[0].SpecCount != nil {
		t.Errorf("result %+v, %v; want unknown counts and SpecCountErr", result, err)
	}
}

// The schedule list nests no specs, so ScheduleListWithSpecs counts them from
// the org-wide spec list; when that list fails, the counts stay unknown and
// the schedules are still returned.
func TestScheduleListWithSpecsCountsFromTheSpecList(t *testing.T) {
	for _, specsFail := range []bool{false, true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/v1/organizations/acme/schedules":
				_, _ = io.WriteString(w, `{"items": [{"name": "nightly"}, {"name": "weekly"}, {"name": "empty"}], "total": 3}`)
			case "/api/v1/organizations/acme/scheduled-tasks":
				if specsFail {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = io.WriteString(w, `{"error": "boom"}`)
					return
				}
				_, _ = io.WriteString(w, `{"items": [{"code": "a", "schedule_name": "nightly"}, {"code": "b", "schedule_name": "nightly"}, {"code": "c", "schedule_name": "weekly"}]}`)
			default:
				t.Errorf("unexpected %s", r.URL.Path)
			}
		}))
		result, err := newTestService(t, srv).ScheduleListWithSpecs(context.Background(), "acme")
		srv.Close()
		if err != nil || len(result.Schedules) != 3 {
			t.Fatalf("specs fail %v: %+v, %v", specsFail, result, err)
		}
		if (result.SpecCountErr != nil) != specsFail {
			t.Errorf("specs fail %v: SpecCountErr = %v", specsFail, result.SpecCountErr)
		}
		for i, want := range []int{2, 1, 0} {
			got := result.Schedules[i].SpecCount
			switch {
			case specsFail && got != nil:
				t.Errorf("%s: count %d after the spec list failed, want unknown", result.Schedules[i].Name, *got)
			case !specsFail && (got == nil || *got != want):
				t.Errorf("%s: count %v, want %d", result.Schedules[i].Name, got, want)
			}
		}
	}
}
