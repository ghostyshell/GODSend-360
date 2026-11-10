package dlxb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"godsend/models"
)

func TestCheckHTTPAliveDeadUnknown(t *testing.T) {
	file := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="game.rar"`)
		w.Write([]byte("x"))
	}))
	defer file.Close()
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer missing.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>ok</html>"))
	}))
	defer page.Close()

	r := NewResolver()
	r.AllowPrivate = true
	cases := []struct {
		url  string
		want string
	}{
		{file.URL, StatusAlive},
		{missing.URL, StatusDead},
		{broken.URL, StatusUnknown},
		{page.URL, StatusUnknown},
	}
	for _, c := range cases {
		got, why := r.Check(context.Background(), models.DlxbLink{Host: "direct", URL: c.url})
		if got != c.want {
			t.Errorf("Check(%s) = %s (%s), want %s", c.url, got, why, c.want)
		}
	}
}

func TestCheckSolverHostsAreUnknown(t *testing.T) {
	r := NewResolver()
	for _, host := range []string{"mediafire", "filecrypt", "adgate"} {
		got, _ := r.Check(context.Background(), models.DlxbLink{Host: host, URL: "https://example.com/x"})
		if got != StatusUnknown {
			t.Errorf("Check(%s) = %s, want unknown", host, got)
		}
	}
}

func TestCheckAllAnnotatesCache(t *testing.T) {
	file := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="game.rar"`)
	}))
	defer file.Close()
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer missing.Close()

	cache := &models.DlxbCache{Entries: map[string]models.DlxbEntry{
		"iso|game": {Links: []models.DlxbLink{
			{Host: "direct", URL: file.URL},
			{Host: "direct", URL: missing.URL},
		}},
	}}
	r := NewResolver()
	r.AllowPrivate = true
	r.CheckAll(context.Background(), cache, 2, nil)

	links := cache.Entries["iso|game"].Links
	if links[0].Status != StatusAlive || links[1].Status != StatusDead {
		t.Fatalf("statuses = %q, %q", links[0].Status, links[1].Status)
	}
	if links[0].CheckedAt.IsZero() {
		t.Fatal("CheckedAt not set")
	}
}

func TestCheckAllFilteredSkipsOtherHosts(t *testing.T) {
	file := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="game.rar"`)
	}))
	defer file.Close()
	cache := &models.DlxbCache{Entries: map[string]models.DlxbEntry{
		"iso|game": {Links: []models.DlxbLink{{Host: "direct", URL: file.URL}}},
	}}
	r := NewResolver()
	r.AllowPrivate = true
	r.CheckAllFiltered(context.Background(), cache, 2, map[string]bool{"1fichier": true}, nil)
	if got := cache.Entries["iso|game"].Links[0].Status; got != "" {
		t.Fatalf("direct link was checked despite the host filter: %q", got)
	}
}
