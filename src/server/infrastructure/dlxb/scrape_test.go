package dlxb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"godsend/app"
)

// fakeSite emulates the WordPress REST endpoint for both categories.
func fakeSite(t *testing.T) *httptest.Server {
	t.Helper()
	posts := map[string][][]string{
		// category id -> pages -> post JSON
		"127": {
			{
				`{"link":"https://dlxbgame.com/just-dance-2019/","title":{"rendered":"Just Dance 2019 [Region Free][ISO]"},"content":{"rendered":"<p>Password : DLXBGAME.COM</p><a href=\"https://1fichier.com/?abc\">1File</a><a href=\"https://filecrypt.cc/Container/AAA.html\">1File (Multi)</a><a href=\"https://gamepciso.com\">Here</a>"}}`,
				`{"link":"https://dlxbgame.com/fifa-19/","title":{"rendered":"FIFA 19 [PAL][ISO]"},"content":{"rendered":"———<br/>FIFA 19 (Europe) (En,De) : <a href=\"https://downloadgameps3.net/archives/1\">Direct</a><br/>FIFA 19 (USA) : <a href=\"https://downloadgameps3.net/archives/2\">Direct</a><a href=\"https://dlxbgame.com/dmca/\">DMCA</a>"}}`,
			},
			{
				`{"link":"https://dlxbgame.com/jtag-only-in-iso-cat/","title":{"rendered":"Some Game [ISO]"},"content":{"rendered":"<a href=\"https://www.mediafire.com/folder/abc/Game\">Mediafire</a>"}}`,
			},
		},
		"113": {
			{
				`{"link":"https://dlxbgame.com/fifa-21-jtag/","title":{"rendered":"FIFA 21 Mod [Jtag/RGH]"},"content":{"rendered":"<p>Password : DLXBGAME.COM</p><a href=\"https://www.mediafire.com/folder/rb1/Game\">Mediafire</a><a href=\"https://1fichier.com/?xyz\">1File</a>"}}`,
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cat := r.URL.Query().Get("categories")
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pages := posts[cat]
		if page < 1 || page > len(pages) {
			w.Header().Set("X-WP-TotalPages", fmt.Sprint(len(pages)))
			w.WriteHeader(400)
			w.Write([]byte(`{"code":"rest_post_invalid_page_number"}`))
			return
		}
		w.Header().Set("X-WP-TotalPages", fmt.Sprint(len(pages)))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[" + join(pages[page-1]) + "]"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

func TestScrapeBuildsCache(t *testing.T) {
	srv := fakeSite(t)
	cache, err := Scrape(context.Background(), Options{
		APIURL:      srv.URL + "/posts",
		PerPage:     100,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := len(cache.Games["iso"]); got != 3 {
		t.Fatalf("iso titles = %d, want 3 (%v)", got, cache.Games["iso"])
	}
	if got := len(cache.Games["rgh"]); got != 1 {
		t.Fatalf("rgh titles = %d, want 1", got)
	}

	iso, ok := cache.Entries[EntryKey("iso", "Just Dance 2019 [Region Free][ISO]")]
	if !ok {
		t.Fatal("missing Just Dance entry")
	}
	if iso.Password != "DLXBGAME.COM" {
		t.Fatalf("password = %q", iso.Password)
	}
	if iso.Region != "REGION FREE" {
		t.Fatalf("region = %q", iso.Region)
	}
	hosts := map[string]bool{}
	for _, l := range iso.Links {
		hosts[l.Host] = true
	}
	if !hosts["1fichier"] || !hosts["filecrypt"] {
		t.Fatalf("links = %+v, want 1fichier + filecrypt", iso.Links)
	}
	if hosts["web"] {
		t.Fatalf("gamepciso/dlxbgame links leaked into entry: %+v", iso.Links)
	}

	fifa, ok := cache.Entries[EntryKey("iso", "FIFA 19 [PAL][ISO]")]
	if !ok {
		t.Fatal("missing FIFA entry")
	}
	if len(fifa.Links) != 2 || fifa.Links[0].Host != "adgate" {
		t.Fatalf("FIFA links = %+v, want two adgate links", fifa.Links)
	}
	if fifa.Links[0].Label != "FIFA 19 (Europe) (En,De)" || fifa.Links[1].Label != "FIFA 19 (USA)" {
		t.Fatalf("edition labels = %q / %q", fifa.Links[0].Label, fifa.Links[1].Label)
	}

	rgh, ok := cache.Entries[EntryKey("rgh", "FIFA 21 Mod [Jtag/RGH]")]
	if !ok {
		t.Fatal("missing RGH entry")
	}
	if rgh.Category != "rgh" || rgh.PageURL == "" {
		t.Fatalf("bad rgh entry: %+v", rgh)
	}
}

func TestClassifyHost(t *testing.T) {
	cases := map[string]string{
		"https://1fichier.com/?x":                     "1fichier",
		"https://www.filecrypt.cc/Container/ABC.html": "filecrypt",
		"https://www.mediafire.com/folder/a/b":        "mediafire",
		"https://drive.google.com/file/d/abc/view":    "gdrive",
		"https://mega.nz/file/abc":                    "mega",
		"https://gofile.io/d/abc":                     "gofile",
		"https://anotepad.com/notes/abc":              "anotepad",
		"https://bit.ly/abc":                          "shortener",
		"https://downloadgameps3.net/archives/1":      "adgate",
		"https://cdn.example.com/game.iso":            "direct",
		"https://gamepciso.com":                       "",
		"https://dlxbgame.com/x":                      "",
		"https://pixeldrain.com/u/abc":                "pixeldrain",
		"https://buzzheavier.com/abc":                 "buzzheavier",
	}
	for in, want := range cases {
		if got := ClassifyHost(in); got != want {
			t.Errorf("ClassifyHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScrapeSchemaUsesAppConstant(t *testing.T) {
	srv := fakeSite(t)
	cache, err := Scrape(context.Background(), Options{APIURL: srv.URL + "/posts", Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	if cache.Schema != app.DlxbCacheSchema {
		t.Fatalf("schema = %d, want %d", cache.Schema, app.DlxbCacheSchema)
	}
}
