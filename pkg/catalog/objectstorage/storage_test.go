package objectstorage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/knowledge"
)

type fakeBucket struct {
	closeErr error
	objects  []object
	reads    int
	body     string
}

func (f *fakeBucket) List(context.Context, string, int) ([]object, error) { return f.objects, nil }
func (f *fakeBucket) Read(context.Context, string) (io.ReadCloser, error) {
	f.reads++
	return io.NopCloser(strings.NewReader(f.body)), nil
}
func (f *fakeBucket) Close() error { return f.closeErr }
func TestCrawlNeverReadsContent(t *testing.T) {
	t.Setenv("STORAGE_CATALOG_TOKEN", "credential")
	c, _ := New(Config{System: "s3", Bucket: "bucket", Prefix: "docs/", Credential: "env:STORAGE_CATALOG_TOKEN"})
	b := &fakeBucket{objects: []object{{Key: "docs/readme.md", Bytes: 42}}}
	c.open = func(context.Context, string) (bucket, error) { return b, nil }
	r, err := c.Crawl(context.Background())
	if err != nil || len(r) != 1 || r[0].Path != "s3://bucket/docs/readme.md" || b.reads != 0 {
		t.Fatalf("catalog=%+v reads=%d err=%v", r, b.reads, err)
	}
}
func TestCollectFailsWholeOnUnsupportedOrOversize(t *testing.T) {
	c, _ := New(Config{System: "gcs", Bucket: "bucket", Credential: "env:unused"})
	for _, objects := range [][]object{{{Key: "a.md", Bytes: 4}, {Key: "b.exe", Bytes: 4}}, {{Key: "large.txt", Bytes: knowledge.MaxImportBytes + 1}}} {
		b := &fakeBucket{objects: objects, body: "text"}
		docs, err := c.collect(context.Background(), b)
		if err == nil || docs != nil {
			t.Fatalf("partial collect=%+v %v", docs, err)
		}
	}
}
func TestCollectText(t *testing.T) {
	c, _ := New(Config{System: "gcs", Bucket: "bucket", Credential: "env:unused"})
	b := &fakeBucket{objects: []object{{Key: "docs/a.md", Bytes: 4}}, body: "text"}
	docs, err := c.collect(context.Background(), b)
	if err != nil || len(docs) != 1 || docs[0].URI != "gs://bucket/docs/a.md" || docs[0].Text != "text" {
		t.Fatalf("docs=%+v err=%v", docs, err)
	}
}

func TestS3ReaderCredentialRegion(t *testing.T) {
	b, err := dial(context.Background(), Config{System: "s3", Bucket: "reports"}, `{"access_key_id":"test","secret_access_key":"test","region":"eu-west-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.(*s3Bucket).c.Options().Region; got != "eu-west-1" {
		t.Fatalf("region=%q", got)
	}
}

func TestCrawlPropagatesCloseFailureWithoutPublishingEntries(t *testing.T) {
	t.Setenv("STORAGE_CATALOG_TOKEN", "credential")
	c, _ := New(Config{System: "s3", Bucket: "bucket", Credential: "env:STORAGE_CATALOG_TOKEN"})
	closeErr := errors.New("close failed")
	b := &fakeBucket{objects: []object{{Key: "a.md", Bytes: 4}}, closeErr: closeErr}
	c.open = func(context.Context, string) (bucket, error) { return b, nil }
	entries, err := c.Crawl(context.Background())
	if !errors.Is(err, closeErr) || entries != nil {
		t.Fatalf("entries=%v error=%v", entries, err)
	}
}
