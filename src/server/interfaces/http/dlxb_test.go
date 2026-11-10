package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"godsend/app"
	"godsend/infrastructure/dlxb"
	"godsend/models"
	"godsend/services/cache"
	"godsend/services/local"
)

func dlxbTestDeps(t *testing.T) (*Deps, *httptest.Server) {
	t.Helper()
	a := app.NewApp()
	a.ToolsDir = t.TempDir()
	a.DlxbGames["iso"] = []string{"Just Dance 2019 [Region Free][ISO]"}
	a.DlxbGames["rgh"] = []string{"FIFA 21 Mod [Jtag/RGH]"}
	a.DlxbEntries[dlxb.EntryKey("iso", "Just Dance 2019 [Region Free][ISO]")] = models.DlxbEntry{
		Title:    "Just Dance 2019 [Region Free][ISO]",
		Category: "iso",
		PageURL:  "https://dlxbgame.com/just-dance-2019/",
		Links:    []models.DlxbLink{{Host: "1fichier", URL: "https://1fichier.com/?x"}},
	}
	d := &Deps{
		App:   a,
		Dlxb:  &cache.DlxbService{App: a},
		Local: &local.Service{App: a},
	}
	return d, httptest.NewServer(d.NewRouter())
}

// The bundled scrape must be browsable without touching the network.
func TestBrowseDlxbServesCachedTitles(t *testing.T) {
	_, srv := dlxbTestDeps(t)
	defer srv.Close()

	resp, err := stdhttp.Get(srv.URL + "/browse?platform=dlxb")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if got := b.String(); got != "Just Dance 2019 [Region Free][ISO]" {
		t.Fatalf("body = %q", got)
	}

	resp2, err := stdhttp.Get(srv.URL + "/browse?platform=dlxb_rgh")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	b.Reset()
	for {
		n, err := resp2.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if got := b.String(); got != "FIFA 21 Mod [Jtag/RGH]" {
		t.Fatalf("rgh body = %q", got)
	}
}

func TestTriggerDlxbUnknownTitleReturnsUnavailable(t *testing.T) {
	_, srv := dlxbTestDeps(t)
	defer srv.Close()

	resp, err := stdhttp.Get(srv.URL + "/trigger?game=Not+In+The+List&platform=dlxb")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "dlxb_unavailable") {
		t.Fatalf("body = %q", body)
	}
}

func TestCacheStatusIncludesDlxb(t *testing.T) {
	_, srv := dlxbTestDeps(t)
	defer srv.Close()
	resp, err := stdhttp.Get(srv.URL + "/cache-status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	if !strings.Contains(string(buf[:n]), `"dlxb"`) {
		t.Fatalf("cache-status missing dlxb: %s", string(buf[:n]))
	}
}

func TestBrowseHidesDeadOnlyTitles(t *testing.T) {
	d, srv := dlxbTestDeps(t)
	defer srv.Close()

	d.App.DlxbGames["iso"] = []string{"Live Game [ISO]", "Dead Game [ISO]"}
	d.App.DlxbEntries[dlxb.EntryKey("iso", "Live Game [ISO]")] = models.DlxbEntry{
		Title: "Live Game [ISO]", Category: "iso",
		Links: []models.DlxbLink{{Host: "direct", URL: "https://example.com/live", Status: "alive"}},
	}
	d.App.DlxbEntries[dlxb.EntryKey("iso", "Dead Game [ISO]")] = models.DlxbEntry{
		Title: "Dead Game [ISO]", Category: "iso",
		Links: []models.DlxbLink{{Host: "1fichier", URL: "https://1fichier.com/?x", Status: "dead"}},
	}

	resp, err := stdhttp.Get(srv.URL + "/browse?platform=dlxb")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "Live Game") {
		t.Fatalf("live title missing: %q", body)
	}
	if strings.Contains(body, "Dead Game") {
		t.Fatalf("dead-only title still surfaced: %q", body)
	}
}
