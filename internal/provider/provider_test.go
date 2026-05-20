package provider

import "testing"

func TestPickFailedJob(t *testing.T) {
	jobs := []Job{
		{ID: 1, Conclusion: "success"},
		{ID: 2, Conclusion: "failure", Steps: []Step{
			{Name: "ok", Conclusion: "success"},
			{Name: "boom", Conclusion: "failure"},
		}},
		{ID: 3, Conclusion: "failure", Steps: []Step{
			{Name: "x", Conclusion: "success"},
		}},
	}
	j, st := PickFailedJob(jobs, 0)
	if j == nil || j.ID != 2 || st == nil || st.Name != "boom" {
		t.Fatalf("unexpected: %+v %+v", j, st)
	}
	// targeted lookup
	j, _ = PickFailedJob(jobs, 3)
	if j == nil || j.ID != 3 {
		t.Fatalf("targeted lookup wrong: %+v", j)
	}
	// not found → falls back to first job when want == 0
	if j, _ := PickFailedJob(jobs, 999); j != nil {
		t.Fatalf("expected nil for unknown id, got %+v", j)
	}
	// nothing failed → fallback to first job
	if j, _ := PickFailedJob([]Job{{ID: 1, Conclusion: "success"}}, 0); j == nil || j.ID != 1 {
		t.Fatalf("expected fallback to first job: %+v", j)
	}
}

func TestRegistryRoundTrip(t *testing.T) {
	called := false
	Register("test_provider", func(token string) Provider {
		called = true
		return nil
	})
	f, err := Get("test_provider")
	if err != nil {
		t.Fatal(err)
	}
	_ = f("")
	if !called {
		t.Fatal("factory not invoked")
	}
	if _, err := Get("missing"); err == nil {
		t.Fatal("expected error for unknown provider")
	}
	names := Names()
	found := false
	for _, n := range names {
		if n == "test_provider" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("registered provider missing from list: %v", names)
	}
}
