package fluxapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListIPs_StripsPortsAndDedupes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"status":"success",
			"data":[
				{"ip":"80.72.20.158:16147","name":"redis-test"},
				{"ip":"77.132.58.19:16157","name":"redis-test"},
				{"ip":"80.72.20.158:16147","name":"dup"}
			]
		}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL).ListIPs(context.Background(), "redis-test")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"77.132.58.19", "80.72.20.158"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
