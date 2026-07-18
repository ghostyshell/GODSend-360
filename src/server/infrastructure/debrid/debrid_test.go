package debrid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func quiet(string, ...any) {}

func fastPoll(t *testing.T) {
	t.Helper()
	orig := pollInterval
	pollInterval = time.Millisecond
	t.Cleanup(func() { pollInterval = orig })
}

func TestMagnet(t *testing.T) {
	got := Magnet("abcd", "My Game", []string{"udp://t", ""})
	want := "magnet:?xt=urn:btih:abcd&dn=My+Game&tr=udp%3A%2F%2Ft"
	if got != want {
		t.Fatalf("Magnet() = %q, want %q", got, want)
	}
}

func TestActiveSelection(t *testing.T) {
	if Active("none", "rk", "tk", quiet) != nil {
		t.Fatal("provider none should be nil")
	}
	if Active("realdebrid", "", "tk", quiet) != nil {
		t.Fatal("realdebrid without key should be nil")
	}
	if p := Active("realdebrid", "rk", "", quiet); p == nil || p.Name() != "Real-Debrid" {
		t.Fatalf("expected Real-Debrid, got %v", p)
	}
	if p := Active("torbox", "", "tk", quiet); p == nil || p.Name() != "TorBox" {
		t.Fatalf("expected TorBox, got %v", p)
	}
}

func TestRealDebridCacheTorrentSuccess(t *testing.T) {
	fastPoll(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/torrents/addMagnet", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"T1","uri":"magnet:x"}`))
	})
	mux.HandleFunc("/torrents/info/T1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"T1","status":"downloaded","files":[{"id":1,"path":"/Game.zip","bytes":100,"selected":1}],"links":["https://rd/link1"]}`))
	})
	mux.HandleFunc("/torrents/selectFiles/T1", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})
	mux.HandleFunc("/unrestrict/link", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"download":"https://cdn.rd/direct"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rd := &realDebrid{key: "k", log: quiet, http: srv.Client(), base: srv.URL}
	got, err := rd.CacheTorrent(context.Background(), "magnet:x", "Game.zip", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "https://cdn.rd/direct" {
		t.Fatalf("got %q, want direct link", got)
	}
}

func TestRealDebridCacheTorrentTimeoutFallsBack(t *testing.T) {
	fastPoll(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/torrents/addMagnet", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"T1"}`))
	})
	// Always "downloading" - never cached within the wait window.
	mux.HandleFunc("/torrents/info/T1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"T1","status":"downloading","files":[{"id":1,"path":"/Game.zip"}]}`))
	})
	mux.HandleFunc("/torrents/selectFiles/T1", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	deleted := false
	mux.HandleFunc("/torrents/delete/T1", func(w http.ResponseWriter, r *http.Request) {
		deleted = true
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rd := &realDebrid{key: "k", log: quiet, http: srv.Client(), base: srv.URL}
	got, err := rd.CacheTorrent(context.Background(), "magnet:x", "Game.zip", 30*time.Millisecond)
	if err != nil {
		t.Fatalf("timeout should return nil error, got %v", err)
	}
	if got != "" {
		t.Fatalf("timeout should return empty link, got %q", got)
	}
	if !deleted {
		t.Fatal("expected abandoned torrent to be deleted")
	}
}

func TestRealDebridWebUnsupported(t *testing.T) {
	rd := &realDebrid{key: "k", log: quiet, http: http.DefaultClient}
	if _, err := rd.CacheWebURL(context.Background(), "https://archive.org/x", time.Second); err != ErrUnsupported {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func TestTorBoxCacheTorrentSuccess(t *testing.T) {
	fastPoll(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/torrents/createtorrent", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"torrent_id":5,"hash":"abc"}}`))
	})
	mux.HandleFunc("/torrents/mylist", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"id":5,"download_finished":true,"files":[{"id":2,"name":"Game.zip","size":100}]}}`))
	})
	mux.HandleFunc("/torrents/requestdl", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("file_id") != "2" || r.URL.Query().Get("torrent_id") != "5" {
			t.Errorf("requestdl bad params: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"success":true,"data":"https://cdn.tb/direct"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tb := &torBox{key: "k", log: quiet, http: srv.Client(), base: srv.URL}
	got, err := tb.CacheTorrent(context.Background(), "magnet:x", "Game.zip", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "https://cdn.tb/direct" {
		t.Fatalf("got %q", got)
	}
}

func TestTorBoxCacheWebURLSuccess(t *testing.T) {
	fastPoll(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/webdl/createwebdownload", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"webdownload_id":9,"hash":"h"}}`))
	})
	mux.HandleFunc("/webdl/mylist", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"id":9,"download_present":true,"files":[{"id":1,"name":"file"}]}}`))
	})
	mux.HandleFunc("/webdl/requestdl", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("web_id") != "9" {
			t.Errorf("requestdl bad params: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"success":true,"data":"https://cdn.tb/webdirect"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tb := &torBox{key: "k", log: quiet, http: srv.Client(), base: srv.URL}
	got, err := tb.CacheWebURL(context.Background(), "https://archive.org/x.zip", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "https://cdn.tb/webdirect" {
		t.Fatalf("got %q", got)
	}
}

func TestTorBoxCacheTorrentTimeoutFallsBack(t *testing.T) {
	fastPoll(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/torrents/createtorrent", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"torrent_id":5,"hash":"abc"}}`))
	})
	// Never finished within the wait window.
	mux.HandleFunc("/torrents/mylist", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"id":5,"download_state":"downloading","files":[{"id":2,"name":"Game.zip","size":100}]}}`))
	})
	deleted := false
	mux.HandleFunc("/torrents/deletetorrent", func(w http.ResponseWriter, r *http.Request) {
		deleted = true
		w.Write([]byte(`{"success":true}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tb := &torBox{key: "k", log: quiet, http: srv.Client(), base: srv.URL}
	got, err := tb.CacheTorrent(context.Background(), "magnet:x", "Game.zip", 30*time.Millisecond)
	if err != nil {
		t.Fatalf("timeout should return nil error, got %v", err)
	}
	if got != "" {
		t.Fatalf("timeout should return empty link, got %q", got)
	}
	if !deleted {
		t.Fatal("expected abandoned torrent to be deleted")
	}
}

func TestTorBoxCacheTorrentNoMatchFallsBack(t *testing.T) {
	fastPoll(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/torrents/createtorrent", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"torrent_id":5}}`))
	})
	mux.HandleFunc("/torrents/mylist", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":{"id":5,"download_finished":true,"files":[{"id":2,"name":"Other.iso","size":100}]}}`))
	})
	deleted := false
	mux.HandleFunc("/torrents/deletetorrent", func(w http.ResponseWriter, r *http.Request) {
		deleted = true
		w.Write([]byte(`{"success":true}`))
	})
	mux.HandleFunc("/torrents/requestdl", func(w http.ResponseWriter, r *http.Request) {
		t.Error("requestdl should not be called when no file matches")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tb := &torBox{key: "k", log: quiet, http: srv.Client(), base: srv.URL}
	got, err := tb.CacheTorrent(context.Background(), "magnet:x", "Game.zip", 5*time.Second)
	if err != nil {
		t.Fatalf("no-match should return nil error, got %v", err)
	}
	if got != "" {
		t.Fatalf("no-match should return empty link, got %q", got)
	}
	if !deleted {
		t.Fatal("expected unmatched torrent to be deleted")
	}
}

func TestRealDebridCacheTorrentNoMatchFallsBack(t *testing.T) {
	fastPoll(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/torrents/addMagnet", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"T1"}`))
	})
	mux.HandleFunc("/torrents/info/T1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"T1","status":"downloaded","files":[{"id":1,"path":"/Other.iso","bytes":100}],"links":["https://rd/link1"]}`))
	})
	deleted := false
	mux.HandleFunc("/torrents/delete/T1", func(w http.ResponseWriter, r *http.Request) {
		deleted = true
		w.WriteHeader(204)
	})
	mux.HandleFunc("/torrents/selectFiles/T1", func(w http.ResponseWriter, r *http.Request) {
		t.Error("selectFiles should not be called when no file matches")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rd := &realDebrid{key: "k", log: quiet, http: srv.Client(), base: srv.URL}
	got, err := rd.CacheTorrent(context.Background(), "magnet:x", "Game.zip", 5*time.Second)
	if err != nil {
		t.Fatalf("no-match should return nil error, got %v", err)
	}
	if got != "" {
		t.Fatalf("no-match should return empty link, got %q", got)
	}
	if !deleted {
		t.Fatal("expected unmatched torrent to be deleted")
	}
}
