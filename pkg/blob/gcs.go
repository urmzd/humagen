package blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// GCSConfig says where a Google Cloud Storage bucket is.
type GCSConfig struct {
	// Bucket is required.
	Bucket string
	// Prefix is put before every object name, so one bucket can hold more
	// than one server's files. A trailing slash is added if it has none.
	Prefix string
	// Endpoint is the base URL of an emulator, such as http://127.0.0.1:4443
	// for fake-gcs-server. Requests to it are not authenticated. Empty means
	// Google Cloud Storage itself, with Application Default Credentials:
	// Workload Identity on GKE, GOOGLE_APPLICATION_CREDENTIALS, or what
	// `gcloud auth application-default login` left.
	Endpoint string
	// HTTPClient replaces the client requests go through, and with it the
	// default credentials: it must authenticate requests itself. Nil means
	// the library's, with Application Default Credentials.
	HTTPClient *http.Client
}

// GCS keeps bytes as objects in a Google Cloud Storage bucket, under
// prefix/tenant/key.
//
// Each object carries the SHA-256 of its bytes in its metadata, computed here
// when it is put, so Stat and List can answer without reading it back. GCS
// replaces an object in one step, so a reader sees the old bytes or the new
// ones, and Get reads the generation it looked at, so the bytes and the hash
// it checks them against belong to the same version.
type GCS struct {
	client *storage.Client
	bucket *storage.BucketHandle
	prefix string
}

// NewGCS returns a store for a bucket. It does not contact the bucket: a
// wrong setting shows on the first call.
func NewGCS(ctx context.Context, c GCSConfig) (*GCS, error) {
	if strings.TrimSpace(c.Bucket) == "" {
		return nil, fmt.Errorf("%w: a GCS store needs a bucket", ErrInvalid)
	}
	prefix := strings.Trim(c.Prefix, "/")
	if prefix != "" {
		if err := check("x", prefix); err != nil {
			return nil, fmt.Errorf("%w: prefix %q", ErrInvalid, c.Prefix)
		}
		prefix += "/"
	}
	// Reads go through the JSON API, the same one writes and listings use,
	// so an emulator need only speak that.
	opts := []option.ClientOption{storage.WithJSONReads()}
	if c.Endpoint != "" {
		u, err := url.Parse(c.Endpoint)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("%w: endpoint %q is not a URL", ErrInvalid, c.Endpoint)
		}
		u.Path = "/storage/v1/"
		opts = append(opts, option.WithEndpoint(u.String()), option.WithoutAuthentication())
	}
	if c.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(c.HTTPClient))
	}
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("blob: gcs: %w", err)
	}
	return &GCS{client: client, bucket: client.Bucket(c.Bucket), prefix: prefix}, nil
}

// Close releases the client.
func (g *GCS) Close() error { return g.client.Close() }

func (g *GCS) object(tenant, key string) *storage.ObjectHandle {
	return g.bucket.Object(g.prefix + tenant + "/" + key)
}

func (g *GCS) fail(key string, err error) error {
	if errors.Is(err, storage.ErrObjectNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return fmt.Errorf("blob: gcs: %w", err)
}

func gcsInfo(key string, a *storage.ObjectAttrs) Info {
	return Info{Key: key, Size: a.Size, SHA256: a.Metadata[metaSHA256], At: a.Updated}
}

// Put implements Store.
func (g *GCS) Put(ctx context.Context, tenant, key string, r io.Reader, max int64) (Info, error) {
	if err := check(tenant, key); err != nil {
		return Info{}, err
	}
	// Read whole and bounded before anything is sent, so a refused put
	// leaves the object as it was.
	b, err := read(r, max)
	if err != nil {
		return Info{}, err
	}
	hash := sum(b)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // cancelling a writer that was not closed abandons the upload
	w := g.object(tenant, key).NewWriter(ctx)
	w.ContentType = "application/octet-stream"
	w.Metadata = map[string]string{metaSHA256: hash}
	if _, err := w.Write(b); err != nil {
		return Info{}, fmt.Errorf("blob: gcs: %w", err)
	}
	if err := w.Close(); err != nil {
		return Info{}, fmt.Errorf("blob: gcs: %w", err)
	}
	// The time is the bucket's, as Stat will report it.
	return Info{Key: key, Size: int64(len(b)), SHA256: hash, At: w.Attrs().Updated}, nil
}

// Get implements Store. The bytes are read whole and checked against the hash
// they were stored with, so what a caller gets is what was put.
func (g *GCS) Get(ctx context.Context, tenant, key string) (io.ReadCloser, Info, error) {
	if err := check(tenant, key); err != nil {
		return nil, Info{}, err
	}
	o := g.object(tenant, key)
	attrs, err := o.Attrs(ctx)
	if err != nil {
		return nil, Info{}, g.fail(key, err)
	}
	r, err := o.Generation(attrs.Generation).NewReader(ctx)
	if err != nil {
		return nil, Info{}, g.fail(key, err)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, Info{}, fmt.Errorf("blob: gcs: %w", err)
	}
	hash := sum(b)
	if want := attrs.Metadata[metaSHA256]; want != "" && want != hash {
		return nil, Info{}, fmt.Errorf("blob: gcs: %s does not match the hash it was stored with", key)
	}
	return io.NopCloser(bytes.NewReader(b)), Info{Key: key, Size: int64(len(b)), SHA256: hash, At: attrs.Updated}, nil
}

// Stat implements Store.
func (g *GCS) Stat(ctx context.Context, tenant, key string) (Info, error) {
	if err := check(tenant, key); err != nil {
		return Info{}, err
	}
	attrs, err := g.object(tenant, key).Attrs(ctx)
	if err != nil {
		return Info{}, g.fail(key, err)
	}
	return g.complete(ctx, tenant, gcsInfo(key, attrs))
}

// complete hashes an object put there by something other than this store,
// which carries no hash. Everything comes from the one read, so an overwrite
// in between cannot pair one version's size with another's hash.
func (g *GCS) complete(ctx context.Context, tenant string, info Info) (Info, error) {
	if info.SHA256 != "" {
		return info, nil
	}
	r, got, err := g.Get(ctx, tenant, info.Key)
	if err != nil {
		return Info{}, err
	}
	_ = r.Close()
	return got, nil
}

// Delete implements Store. GCS refuses to delete a missing object, which is
// what the Store contract asks for.
func (g *GCS) Delete(ctx context.Context, tenant, key string) error {
	if err := check(tenant, key); err != nil {
		return err
	}
	if err := g.object(tenant, key).Delete(ctx); err != nil {
		return g.fail(key, err)
	}
	return nil
}

// List implements Store. A listing carries each object's metadata, so only an
// object this store did not write is read for its hash.
func (g *GCS) List(ctx context.Context, tenant, prefix string) ([]Info, error) {
	if err := check(tenant, "x"); err != nil {
		return nil, err
	}
	root := g.prefix + tenant + "/"
	var found []Info
	it := g.bucket.Objects(ctx, &storage.Query{Prefix: root + prefix})
	for {
		a, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("blob: gcs: %w", err)
		}
		if key, ok := strings.CutPrefix(a.Name, root); ok && check(tenant, key) == nil {
			found = append(found, gcsInfo(key, a))
		}
	}
	slices.SortFunc(found, func(a, b Info) int { return strings.Compare(a.Key, b.Key) })
	out := make([]Info, 0, len(found))
	for _, info := range found {
		info, err := g.complete(ctx, tenant, info)
		if errors.Is(err, ErrNotFound) {
			continue // deleted since it was listed
		}
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}
