package cache

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"godsend/app"
)

func TestScrapeMinervaPageDataName(t *testing.T) {
	const html = `<!DOCTYPE html><html><body>
<div class="entry" data-name="halo 3 (usa).zip">
  <a href="/rom?id=1">Halo 3 (USA).zip</a>
</div>
<div class="entry" data-name="assorted (addon).zip">
  <a href="/rom?id=2">Assorted (Addon).zip</a>
</div>
<div class="entry" data-name="skip.txt"><a href="/rom?id=3">skip.txt</a></div>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(srv.Close)

	s := &MinervaService{App: &app.App{}}
	browse := srv.URL + "/browse/Redump/Microsoft%20-%20Xbox%20360/"
	entries, err := s.ScrapeMinervaPage(browse, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	if entries[0].FileName != "Halo 3 (USA).zip" {
		t.Fatalf("fileName=%q (want proper case from rom?id link)", entries[0].FileName)
	}
	if !strings.Contains(entries[0].PathParam, "Redump") || !strings.Contains(entries[0].PathParam, "Halo") {
		t.Fatalf("pathParam=%q", entries[0].PathParam)
	}

	filtered, err := s.ScrapeMinervaPage(browse, []string{"(Addon)"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].FileName != "Assorted (Addon).zip" {
		t.Fatalf("filtered=%+v", filtered)
	}
}

func TestScrapeMinervaPageLegacyHref(t *testing.T) {
	const html = `<a href="/rom?name=.%2FNo-Intro%2FMicrosoft%20-%20Xbox%20360%20(Digital)%2FFoo%20(XBLA).zip">Foo</a>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(srv.Close)

	s := &MinervaService{App: &app.App{}}
	entries, err := s.ScrapeMinervaPage(srv.URL+"/browse/x/", []string{"(XBLA)"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].FileName != "Foo (XBLA).zip" {
		t.Fatalf("got %+v", entries)
	}
}
