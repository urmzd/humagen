package blob_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/warp/pkg/blob"
	"github.com/urmzd/warp/pkg/blob/blobtest"
)

// fakeS3 is the part of the S3 API the store uses, path style, in memory. It
// pages listings two keys at a time so the store's paging is exercised.
type fakeS3 struct {
	t      *testing.T
	bucket string
	mu     sync.Mutex
	objs   map[string]fakeObject
	// unsigned counts requests without a SigV4 signature.
	unsigned int
	// afterHead, when set, runs after a HEAD is answered, with the lock held.
	afterHead func(key string)
}

type fakeObject struct {
	body []byte
	meta map[string]string
	at   time.Time
}

func newFakeS3(t *testing.T) (*fakeS3, *httptest.Server) {
	f := &fakeS3{t: t, bucket: "files", objs: map[string]fakeObject{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		f.mu.Lock()
		f.unsigned++
		f.mu.Unlock()
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/"+f.bucket)
	if !ok {
		f.fail(w, http.StatusNotFound, "NoSuchBucket")
		return
	}
	key := strings.TrimPrefix(rest, "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case key == "" && r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		f.list(w, r)
	case key == "":
		f.fail(w, http.StatusNotImplemented, "NotImplemented")
	case r.Method == http.MethodPut:
		if strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") {
			f.t.Error("the store sent a chunked upload, which some S3-compatible services refuse")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			f.fail(w, http.StatusBadRequest, "IncompleteBody")
			return
		}
		meta := map[string]string{}
		for name, v := range r.Header {
			if m, ok := strings.CutPrefix(strings.ToLower(name), "x-amz-meta-"); ok {
				meta[m] = v[0]
			}
		}
		f.objs[key] = fakeObject{body: body, meta: meta, at: time.Now().UTC().Truncate(time.Second)}
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		o, ok := f.objs[key]
		if !ok {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			f.fail(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		h := w.Header()
		h.Set("Content-Length", strconv.Itoa(len(o.body)))
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Last-Modified", o.at.Format(http.TimeFormat))
		h.Set("ETag", `"etag"`)
		for k, v := range o.meta {
			h.Set("x-amz-meta-"+k, v)
		}
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(o.body)
		} else if f.afterHead != nil {
			f.afterHead(key)
		}
	case r.Method == http.MethodDelete:
		delete(f.objs, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		f.fail(w, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	prefix, after := q.Get("prefix"), q.Get("continuation-token")
	var keys []string
	for k := range f.objs {
		if strings.HasPrefix(k, prefix) && k > after {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	type content struct {
		Key          string `xml:"Key"`
		Size         int    `xml:"Size"`
		LastModified string `xml:"LastModified"`
	}
	type result struct {
		XMLName               xml.Name  `xml:"ListBucketResult"`
		Name                  string    `xml:"Name"`
		Prefix                string    `xml:"Prefix"`
		KeyCount              int       `xml:"KeyCount"`
		IsTruncated           bool      `xml:"IsTruncated"`
		NextContinuationToken string    `xml:"NextContinuationToken,omitempty"`
		Contents              []content `xml:"Contents"`
	}
	out := result{Name: f.bucket, Prefix: prefix}
	if len(keys) > 2 {
		keys, out.IsTruncated = keys[:2], true
		out.NextContinuationToken = keys[1]
	}
	for _, k := range keys {
		out.Contents = append(out.Contents, content{Key: k, Size: len(f.objs[k].body), LastModified: f.objs[k].at.Format(time.RFC3339)})
	}
	out.KeyCount = len(out.Contents)
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(out)
}

func (f *fakeS3) fail(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, "<Error><Code>"+code+"</Code><Message>"+code+"</Message></Error>")
}

func openFake(t *testing.T, srv *httptest.Server, prefix string) *blob.S3 {
	t.Helper()
	s, err := blob.NewS3(context.Background(), blob.S3Config{
		Bucket: "files", Region: "us-east-1", Endpoint: srv.URL, PathStyle: true, Prefix: prefix,
		AccessKey: "test", SecretKey: "test-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestS3(t *testing.T) {
	blobtest.Run(t, func(t *testing.T) blob.Store {
		f, srv := newFakeS3(t)
		t.Cleanup(func() {
			if f.unsigned > 0 {
				t.Errorf("%d requests were not signed", f.unsigned)
			}
		})
		return openFake(t, srv, "warp")
	})
}

func TestS3KeysAreUnderThePrefixAndTenant(t *testing.T) {
	f, srv := newFakeS3(t)
	ctx := context.Background()
	one, two := openFake(t, srv, "/one/"), openFake(t, srv, "two")
	if _, err := one.Put(ctx, "acme", "artifacts/art-1", strings.NewReader("one's"), 100); err != nil {
		t.Fatal(err)
	}
	if _, err := two.Put(ctx, "acme", "artifacts/art-1", strings.NewReader("two's"), 100); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	var keys []string
	for k := range f.objs {
		keys = append(keys, k)
	}
	f.mu.Unlock()
	slices.Sort(keys)
	if strings.Join(keys, " ") != "one/acme/artifacts/art-1 two/acme/artifacts/art-1" {
		t.Fatalf("objects = %v", keys)
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
func TestS3RefusesBytesThatDoNotMatchTheirHash(t *testing.T) {
	f, srv := newFakeS3(t)
	s := openFake(t, srv, "")
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
// store works one out instead of reporting none.
func TestS3HashesAnObjectItDidNotWrite(t *testing.T) {
	f, srv := newFakeS3(t)
	s := openFake(t, srv, "")
	f.mu.Lock()
	f.objs["acme/imported"] = fakeObject{body: []byte("abc"), meta: map[string]string{}, at: time.Now()}
	f.mu.Unlock()
	info, err := s.Stat(context.Background(), "acme", "imported")
	if err != nil || info.SHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" || info.Size != 3 {
		t.Fatalf("stat = %+v, %v", info, err)
	}
}

// An object without a hash that is overwritten between the store's HEAD and
// its GET is described wholly by what the GET read: never one version's size
// with another's hash.
func TestS3StatOfAnOverwrittenObjectIsConsistent(t *testing.T) {
	f, srv := newFakeS3(t)
	s := openFake(t, srv, "")
	f.mu.Lock()
	f.objs["acme/imported"] = fakeObject{body: []byte("abc"), meta: map[string]string{}, at: time.Now()}
	f.afterHead = func(key string) {
		f.objs[key] = fakeObject{body: []byte("abcdef"), meta: map[string]string{}, at: time.Now()}
		f.afterHead = nil
	}
	f.mu.Unlock()
	info, err := s.Stat(context.Background(), "acme", "imported")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 6 || info.SHA256 != "bef57ec7f53a6d40beb640a780a639c83bc29ac8a9816f1fc6c5c6dcd93c4721" {
		t.Fatalf("stat after an overwrite = %+v", info)
	}
}

func TestS3Config(t *testing.T) {
	ctx := context.Background()
	if _, err := blob.NewS3(ctx, blob.S3Config{}); !errors.Is(err, blob.ErrInvalid) {
		t.Fatalf("no bucket = %v", err)
	}
	for _, p := range []string{"a/../b", "a//b", "..", "a\x00"} {
		if _, err := blob.NewS3(ctx, blob.S3Config{Bucket: "b", Prefix: p}); !errors.Is(err, blob.ErrInvalid) {
			t.Errorf("prefix %q = %v", p, err)
		}
	}
}

// TestS3Live runs the conformance suite against a real bucket when one is
// named: WARP_TEST_S3_BUCKET, and optionally WARP_TEST_S3_ENDPOINT,
// WARP_TEST_S3_REGION and WARP_TEST_S3_PATH_STYLE=1. Credentials come from
// the AWS environment variables or the default chain. Each store it opens
// writes under a fresh prefix and removes what it wrote.
//
//	WARP_TEST_S3_BUCKET=warp WARP_TEST_S3_ENDPOINT=http://127.0.0.1:9000 \
//	WARP_TEST_S3_PATH_STYLE=1 AWS_ACCESS_KEY_ID=minioadmin AWS_SECRET_ACCESS_KEY=minioadmin \
//	go test ./pkg/blob -run TestS3Live
func TestS3Live(t *testing.T) {
	bucket := os.Getenv("WARP_TEST_S3_BUCKET")
	if bucket == "" {
		t.Skip("WARP_TEST_S3_BUCKET is not set")
	}
	blobtest.Run(t, func(t *testing.T) blob.Store {
		var id [6]byte
		_, _ = rand.Read(id[:])
		s, err := blob.NewS3(t.Context(), blob.S3Config{
			Bucket: bucket, Region: os.Getenv("WARP_TEST_S3_REGION"), Endpoint: os.Getenv("WARP_TEST_S3_ENDPOINT"),
			PathStyle: os.Getenv("WARP_TEST_S3_PATH_STYLE") == "1", Prefix: "warp-test/" + hex.EncodeToString(id[:]),
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
		})
		return s
	})
}
