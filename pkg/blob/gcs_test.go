package blob_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/blob/blobtest"

	"github.com/urmzd/humagen/internal/legacy"
)

// fakeGCS is the part of the Cloud Storage JSON API the store uses, in
// memory. It pages listings two objects at a time so the store's paging is
// exercised.
type fakeGCS struct {
	t      *testing.T
	bucket string
	mu     sync.Mutex
	objs   map[string]gcsObject
	gen    int64
}

type gcsObject struct {
	body []byte
	meta map[string]string
	gen  int64
	at   time.Time
}

func newFakeGCS(t *testing.T) (*fakeGCS, *httptest.Server) {
	f := &fakeGCS{t: t, bucket: "files", objs: map[string]gcsObject{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

// put stores an object as the bucket would, with a new generation.
func (f *fakeGCS) put(name string, body []byte, meta map[string]string) gcsObject {
	f.gen++
	o := gcsObject{body: body, meta: meta, gen: f.gen, at: time.Now().UTC().Truncate(time.Millisecond)}
	f.objs[name] = o
	return o
}

func (f *fakeGCS) resource(name string, o gcsObject) map[string]any {
	return map[string]any{
		"kind": "storage#object", "bucket": f.bucket, "name": name,
		"generation": strconv.FormatInt(o.gen, 10), "metageneration": "1",
		"size": strconv.Itoa(len(o.body)), "contentType": "application/octet-stream",
		"updated": o.at.Format(time.RFC3339Nano), "metadata": o.meta,
	}
}

func (f *fakeGCS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.EscapedPath()
	q := r.URL.Query()
	if rest, ok := strings.CutPrefix(path, "/upload/storage/v1/b/"+f.bucket+"/o"); ok && rest == "" && r.Method == http.MethodPost {
		f.upload(w, r)
		return
	}
	rest, ok := strings.CutPrefix(path, "/storage/v1/b/"+f.bucket+"/o")
	if !ok {
		f.fail(w, http.StatusNotFound, "no such bucket")
		return
	}
	if rest == "" && r.Method == http.MethodGet {
		f.list(w, q)
		return
	}
	name, err := url.PathUnescape(strings.TrimPrefix(rest, "/"))
	if err != nil || name == "" {
		f.fail(w, http.StatusBadRequest, "bad object name")
		return
	}
	o, ok := f.objs[name]
	if g := q.Get("generation"); ok && g != "" && g != strconv.FormatInt(o.gen, 10) {
		ok = false
	}
	if !ok {
		f.fail(w, http.StatusNotFound, "No such object: "+f.bucket+"/"+name)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if q.Get("alt") == "media" {
			h := w.Header()
			h.Set("Content-Type", "application/octet-stream")
			h.Set("Content-Length", strconv.Itoa(len(o.body)))
			h.Set("X-Goog-Generation", strconv.FormatInt(o.gen, 10))
			h.Set("X-Goog-Metageneration", "1")
			h.Set("Last-Modified", o.at.Format(http.TimeFormat))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(o.body)
			return
		}
		f.json(w, f.resource(name, o))
	case http.MethodDelete:
		delete(f.objs, name)
		w.WriteHeader(http.StatusNoContent)
	default:
		f.fail(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// upload takes a multipart upload: the object's JSON resource, then its bytes.
func (f *fakeGCS) upload(w http.ResponseWriter, r *http.Request) {
	if got := r.URL.Query().Get("uploadType"); got != "multipart" {
		f.t.Errorf("uploadType = %q, the fake only takes multipart", got)
		f.fail(w, http.StatusNotImplemented, "upload type not supported")
		return
	}
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		f.fail(w, http.StatusBadRequest, "bad content type")
		return
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	var res struct {
		Name     string            `json:"name"`
		Metadata map[string]string `json:"metadata"`
	}
	part, err := mr.NextPart()
	if err != nil || json.NewDecoder(part).Decode(&res) != nil || res.Name == "" {
		f.fail(w, http.StatusBadRequest, "bad object resource")
		return
	}
	part, err = mr.NextPart()
	if err != nil {
		f.fail(w, http.StatusBadRequest, "no media")
		return
	}
	body, err := io.ReadAll(part)
	if err != nil {
		f.fail(w, http.StatusBadRequest, "short media")
		return
	}
	if res.Metadata == nil {
		res.Metadata = map[string]string{}
	}
	f.json(w, f.resource(res.Name, f.put(res.Name, body, res.Metadata)))
}

func (f *fakeGCS) list(w http.ResponseWriter, q url.Values) {
	prefix, after := q.Get("prefix"), q.Get("pageToken")
	var names []string
	for k := range f.objs {
		if strings.HasPrefix(k, prefix) && k > after {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	out := map[string]any{"kind": "storage#objects"}
	if len(names) > 2 {
		names = names[:2]
		out["nextPageToken"] = names[1]
	}
	items := []map[string]any{}
	for _, n := range names {
		items = append(items, f.resource(n, f.objs[n]))
	}
	out["items"] = items
	f.json(w, out)
}

func (f *fakeGCS) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeGCS) fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": status, "message": msg}})
}

func openFakeGCS(t *testing.T, srv *httptest.Server, prefix string) *blob.GCS {
	t.Helper()
	s, err := blob.NewGCS(context.Background(), blob.GCSConfig{Bucket: "files", Endpoint: srv.URL, Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestGCS(t *testing.T) {
	blobtest.Run(t, func(t *testing.T) blob.Store {
		_, srv := newFakeGCS(t)
		return openFakeGCS(t, srv, "humagen")
	})
}

func TestGCSKeysAreUnderThePrefixAndTenant(t *testing.T) {
	f, srv := newFakeGCS(t)
	ctx := context.Background()
	one, two := openFakeGCS(t, srv, "/one/"), openFakeGCS(t, srv, "two")
	if _, err := one.Put(ctx, "acme", "artifacts/art-1", strings.NewReader("one's"), 100); err != nil {
		t.Fatal(err)
	}
	if _, err := two.Put(ctx, "acme", "artifacts/art-1", strings.NewReader("two's"), 100); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	var names []string
	for k := range f.objs {
		names = append(names, k)
	}
	f.mu.Unlock()
	slices.Sort(names)
	if strings.Join(names, " ") != "one/acme/artifacts/art-1 two/acme/artifacts/art-1" {
		t.Fatalf("objects = %v", names)
	}
	if list, _ := one.List(ctx, "acme", ""); len(list) != 1 {
		t.Fatalf("one store lists another's objects: %v", list)
	}
	r, _, err := two.Get(ctx, "acme", "artifacts/art-1")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(r); string(b) != "two's" {
		t.Fatalf("two reads %q", b)
	}
}

// Bytes changed in the bucket behind the store's back are refused, not served.
func TestGCSRefusesBytesThatDoNotMatchTheirHash(t *testing.T) {
	f, srv := newFakeGCS(t)
	s := openFakeGCS(t, srv, "")
	ctx := context.Background()
	if _, err := s.Put(ctx, "acme", "k", strings.NewReader("the real bytes"), 100); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	o := f.objs["acme/k"]
	o.body = []byte("something else")
	f.objs["acme/k"] = o
	f.mu.Unlock()
	if _, _, err := s.Get(ctx, "acme", "k"); err == nil || errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("a changed object = %v", err)
	}
}

// An object put there by something else has no hash in its metadata. The
// store works one out instead of reporting none, in Stat and in List.
func TestGCSHashesAnObjectItDidNotWrite(t *testing.T) {
	f, srv := newFakeGCS(t)
	s := openFakeGCS(t, srv, "")
	f.mu.Lock()
	f.put("acme/imported", []byte("abc"), nil)
	f.mu.Unlock()
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	info, err := s.Stat(context.Background(), "acme", "imported")
	if err != nil || info.SHA256 != want || info.Size != 3 {
		t.Fatalf("stat = %+v, %v", info, err)
	}
	list, err := s.List(context.Background(), "acme", "")
	if err != nil || len(list) != 1 || list[0].SHA256 != want {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestGCSConfig(t *testing.T) {
	ctx := context.Background()
	if _, err := blob.NewGCS(ctx, blob.GCSConfig{}); !errors.Is(err, blob.ErrInvalid) {
		t.Fatalf("no bucket = %v", err)
	}
	for _, p := range []string{"a/../b", "a//b", "..", "a\x00"} {
		if _, err := blob.NewGCS(ctx, blob.GCSConfig{Bucket: "b", Prefix: p, Endpoint: "http://127.0.0.1:1"}); !errors.Is(err, blob.ErrInvalid) {
			t.Errorf("prefix %q = %v", p, err)
		}
	}
	for _, e := range []string{"127.0.0.1:4443", "not a url", "http://"} {
		if _, err := blob.NewGCS(ctx, blob.GCSConfig{Bucket: "b", Endpoint: e}); !errors.Is(err, blob.ErrInvalid) {
			t.Errorf("endpoint %q = %v", e, err)
		}
	}
}

// TestGCSLive runs the conformance suite against a real bucket when one is
// named: HUMAGEN_TEST_GCS_BUCKET, and optionally HUMAGEN_TEST_GCS_ENDPOINT for
// an emulator. Credentials come from Application Default Credentials. Each
// store it opens writes under a fresh prefix and removes what it wrote.
//
//	HUMAGEN_TEST_GCS_BUCKET=humagen-test go test ./pkg/blob -run TestGCSLive
func TestGCSLive(t *testing.T) {
	bucket := legacy.Getenv("HUMAGEN_TEST_GCS_BUCKET")
	if bucket == "" {
		t.Skip("HUMAGEN_TEST_GCS_BUCKET is not set")
	}
	blobtest.Run(t, func(t *testing.T) blob.Store {
		var id [6]byte
		_, _ = rand.Read(id[:])
		s, err := blob.NewGCS(t.Context(), blob.GCSConfig{
			Bucket: bucket, Endpoint: legacy.Getenv("HUMAGEN_TEST_GCS_ENDPOINT"), Prefix: "humagen-test/" + hex.EncodeToString(id[:]),
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			ctx := context.Background()
			for _, tenant := range []string{"acme", "globex"} {
				list, _ := s.List(ctx, tenant, "")
				for _, i := range list {
					_ = s.Delete(ctx, tenant, i.Key)
				}
			}
			_ = s.Close()
		})
		return s
	})
}
