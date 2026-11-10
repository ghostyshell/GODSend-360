package dlxb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"godsend/models"
)

func TestResolveDirectPassthrough(t *testing.T) {
	r := NewResolver()
	files, err := r.Resolve(context.Background(), models.DlxbLink{
		Host: "direct",
		URL:  "https://cdn.example.com/Game%20(USA).rar",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].URL != "https://cdn.example.com/Game%20(USA).rar" {
		t.Fatalf("files = %+v", files)
	}
}

func TestResolveGenericAttachment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="Game.rar"`)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("rar-bytes"))
	}))
	defer srv.Close()

	r := NewResolver()
	r.AllowPrivate = true
	files, err := r.Resolve(context.Background(), models.DlxbLink{Host: "web", URL: srv.URL + "/dl"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v", files)
	}
	if files[0].URL != srv.URL+"/dl" {
		t.Fatalf("url = %q", files[0].URL)
	}
	if files[0].Filename != "Game.rar" {
		t.Fatalf("filename = %q", files[0].Filename)
	}
}

func TestResolveContainerExpands(t *testing.T) {
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="Container.rar"`)
		w.Write([]byte("bytes"))
	}))
	defer fileSrv.Close()

	pageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// A container page holding a file link.
		w.Write([]byte(`<html><body><a href="` + fileSrv.URL + `/file">Download</a></body></html>`))
	}))
	defer pageSrv.Close()

	r := NewResolver()
	r.AllowPrivate = true
	files, err := r.Resolve(context.Background(), models.DlxbLink{Host: "anotepad", URL: pageSrv.URL + "/notes/x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].URL != fileSrv.URL+"/file" {
		t.Fatalf("files = %+v", files)
	}
}

func TestResolveCloudflareViaSolver(t *testing.T) {
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="Solver.rar"`)
		w.Write([]byte("bytes"))
	}))
	defer fileSrv.Close()

	challenge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`<html><title>Just a moment...</title><body>challenge-platform</body></html>`))
	}))
	defer challenge.Close()

	solver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `<html><body><a href="` + fileSrv.URL + `/file">Download</a></body></html>`
		json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"solution": map[string]string{"response": body, "url": challenge.URL + "/"},
		})
	}))
	defer solver.Close()

	r := NewResolver()
	r.AllowPrivate = true
	r.CFSolverURL = solver.URL
	files, err := r.Resolve(context.Background(), models.DlxbLink{Host: "adgate", URL: challenge.URL + "/archives/1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].URL != fileSrv.URL+"/file" {
		t.Fatalf("files = %+v", files)
	}
}

func TestDriveID(t *testing.T) {
	cases := map[string]string{
		"https://drive.google.com/file/d/ABC123/view":        "ABC123",
		"https://drive.google.com/uc?export=download&id=XYZ": "XYZ",
		"https://drive.google.com/open?id=Q":                 "Q",
		"https://example.com/nope":                           "",
	}
	for in, want := range cases {
		if got := driveID(in); got != want {
			t.Errorf("driveID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsCFChallenge(t *testing.T) {
	if !isCFChallenge(403, []byte("<title>Just a moment...</title>")) {
		t.Fatal("challenge page not detected")
	}
	if isCFChallenge(200, []byte("Just a moment...")) {
		t.Fatal("non-403 treated as challenge")
	}
}

func TestFilenameFromDisposition(t *testing.T) {
	resp := &httptest.ResponseRecorder{HeaderMap: http.Header{}}
	resp.Header().Set("Content-Disposition", `attachment; filename="Plain.rar"`)
	if got := filenameFromDisposition(resp.Result()); got != "Plain.rar" {
		t.Fatalf("plain = %q", got)
	}
	resp2 := &httptest.ResponseRecorder{HeaderMap: http.Header{}}
	resp2.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''My%20Game.rar`)
	if got := filenameFromDisposition(resp2.Result()); got != "My Game.rar" {
		t.Fatalf("utf8 = %q", got)
	}
}

func TestResolveDebridFallback(t *testing.T) {
	r := NewResolver()
	r.Debrid = func(context.Context, string) (string, string, error) {
		return "https://debrid.example.com/direct", "Direct.rar", nil
	}
	files, err := r.Resolve(context.Background(), models.DlxbLink{Host: "mega", URL: "https://mega.nz/file/x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !strings.Contains(files[0].URL, "debrid.example.com") {
		t.Fatalf("files = %+v", files)
	}
}

func TestSafeTarget(t *testing.T) {
	r := NewResolver()
	bad := []string{
		"http://127.0.0.1/x",
		"http://localhost/x",
		"http://10.0.0.5/x",
		"http://192.168.1.10/x",
		"http://169.254.1.1/x",
		"ftp://example.com/x",
	}
	for _, u := range bad {
		if err := r.safeTarget(u); err == nil {
			t.Errorf("safeTarget(%q) = nil, want error", u)
		}
	}
	good := []string{"https://1fichier.com/?x", "http://8.8.8.8/x"}
	for _, u := range good {
		if err := r.safeTarget(u); err != nil {
			t.Errorf("safeTarget(%q) = %v", u, err)
		}
	}
}

func TestFilecryptPrefersDebrid(t *testing.T) {
	r := NewResolver()
	called := false
	r.Debrid = func(ctx context.Context, src string) (string, string, error) {
		called = true
		return "https://debrid.example.com/file.rar", "file.rar", nil
	}
	files, err := r.Resolve(context.Background(), models.DlxbLink{
		Host: "filecrypt",
		URL:  "https://filecrypt.cc/Container/ABC.html",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called || len(files) != 1 || files[0].URL != "https://debrid.example.com/file.rar" {
		t.Fatalf("called=%v files=%+v", called, files)
	}
}
