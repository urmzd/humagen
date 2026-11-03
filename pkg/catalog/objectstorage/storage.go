// Package objectstorage supplies GCS and S3 collect and catalog capabilities.
// Catalog crawling reads only object metadata. Collection always uses the
// current reader's credential and never falls back to machine credentials.
package objectstorage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/urmzd/orchagent/flow/mcptools"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/knowledge"
)

// Config scopes all operations to one bucket and optional raw object prefix.
// Credential is a reference to a GCS service account JSON key, or an S3 JSON
// object with access_key_id, secret_access_key and optional session_token.
type Config struct{ System, Bucket, Prefix, Region, Credential string }
type Connector struct {
	cfg  Config
	open func(context.Context, string) (bucket, error)
}
type object struct {
	Key      string
	Bytes    int64
	Modified time.Time
}
type bucket interface {
	List(context.Context, string, int) ([]object, error)
	Read(context.Context, string) (io.ReadCloser, error)
	Close() error
}

func New(c Config) (*Connector, error) {
	if (c.System != "gcs" && c.System != "s3") || c.Bucket == "" || strings.ContainsAny(c.Bucket, "/\\?#@") || c.Credential == "" {
		return nil, errors.New("storage connector: system, bucket and credential reference are required")
	}
	out := &Connector{cfg: c}
	out.open = func(ctx context.Context, key string) (bucket, error) { return dial(ctx, c, key) }
	return out, nil
}
func (c *Connector) Crawl(ctx context.Context) ([]catalog.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	key, err := credref.Default.Resolve(ctx, c.cfg.Credential)
	if err != nil {
		return nil, err
	}
	b, err := c.open(ctx, key)
	if err != nil {
		return nil, errors.New("storage connector: authentication failed")
	}
	defer b.Close()
	objs, err := b.List(ctx, c.cfg.Prefix, catalog.MaxEntries)
	if err != nil {
		return nil, err
	}
	entries := make([]catalog.Entry, 0, len(objs))
	for _, o := range objs {
		size, modified := o.Bytes, o.Modified
		entries = append(entries, catalog.Entry{System: c.cfg.System, Kind: "object", Path: c.uri(o.Key), Title: path.Base(o.Key), Stats: catalog.Stats{Bytes: &size, Modified: &modified}})
	}
	return entries, nil
}
func (c *Connector) uri(key string) string {
	scheme := c.cfg.System
	if scheme == "gcs" {
		scheme = "gs"
	}
	return scheme + "://" + c.cfg.Bucket + "/" + key
}

// Provider implements the existing collected source lifecycle. Its origin is
// gs://bucket/prefix or s3://bucket/prefix, supplied by the reader.
type Provider struct{ System string }

func (p Provider) Fetch(ctx context.Context, origin string, secret mcptools.Secret) ([]knowledge.ImportDocument, error) {
	if secret.Empty() {
		return nil, knowledge.ProviderError{Cause: knowledge.AuthExpired}
	}
	u, err := url.Parse(origin)
	scheme := "s3"
	if p.System == "gcs" {
		scheme = "gs"
	}
	if err != nil || u.Scheme != scheme || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, knowledge.ProviderError{Cause: knowledge.Unsupported}
	}
	c, err := New(Config{System: p.System, Bucket: u.Host, Prefix: strings.TrimPrefix(u.Path, "/"), Credential: "person"})
	if err != nil {
		return nil, knowledge.ProviderError{Cause: knowledge.Unsupported}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	b, err := c.open(ctx, secret.Reveal())
	if err != nil {
		return nil, knowledge.ProviderError{Cause: knowledge.AuthExpired}
	}
	defer b.Close()
	return c.collect(ctx, b)
}
func (c *Connector) collect(ctx context.Context, b bucket) ([]knowledge.ImportDocument, error) {
	objs, err := b.List(ctx, c.cfg.Prefix, knowledge.MaxImportDocuments)
	if err != nil {
		return nil, knowledge.ProviderError{Cause: knowledge.Unavailable}
	}
	out := []knowledge.ImportDocument{}
	total := 0
	for _, o := range objs {
		if strings.HasSuffix(o.Key, "/") {
			continue
		}
		switch strings.ToLower(path.Ext(o.Key)) {
		case ".txt", ".md", ".csv", ".json":
		default:
			return nil, knowledge.ProviderError{Cause: knowledge.Unsupported}
		}
		if o.Bytes < 0 || o.Bytes > int64(knowledge.MaxImportBytes-total) {
			return nil, knowledge.ProviderError{Cause: knowledge.Unsupported}
		}
		r, err := b.Read(ctx, o.Key)
		if err != nil {
			return nil, knowledge.ProviderError{Cause: knowledge.Unavailable}
		}
		data, err := io.ReadAll(io.LimitReader(r, int64(knowledge.MaxImportBytes-total+1)))
		closeErr := r.Close()
		if err != nil || closeErr != nil || len(data) > knowledge.MaxImportBytes-total || !utf8.Valid(data) {
			return nil, knowledge.ProviderError{Cause: knowledge.Unsupported}
		}
		total += len(data)
		out = append(out, knowledge.ImportDocument{ID: o.Key, Title: path.Base(o.Key), URI: c.uri(o.Key), Text: string(data)})
	}
	return out, nil
}

type gcsBucket struct {
	c      *storage.Client
	bucket string
}

func (g *gcsBucket) Close() error { return g.c.Close() }
func (g *gcsBucket) Read(ctx context.Context, key string) (io.ReadCloser, error) {
	return g.c.Bucket(g.bucket).Object(key).NewReader(ctx)
}
func (g *gcsBucket) List(ctx context.Context, prefix string, limit int) ([]object, error) {
	it := g.c.Bucket(g.bucket).Objects(ctx, &storage.Query{Prefix: prefix})
	out := []object{}
	for {
		a, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return nil, errors.New("gcs: listing failed")
		}
		if len(out) == limit {
			return nil, errors.New("gcs: listing exceeds object limit")
		}
		out = append(out, object{Key: a.Name, Bytes: a.Size, Modified: a.Updated})
	}
}

type s3Bucket struct {
	c      *s3.Client
	bucket string
}

func (s *s3Bucket) Close() error { return nil }
func (s *s3Bucket) Read(ctx context.Context, key string) (io.ReadCloser, error) {
	r, err := s.c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	return r.Body, nil
}
func (s *s3Bucket) List(ctx context.Context, prefix string, limit int) ([]object, error) {
	out := []object{}
	var token *string
	for {
		r, err := s.c.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix), ContinuationToken: token, MaxKeys: aws.Int32(1000)})
		if err != nil {
			return nil, errors.New("s3: listing failed")
		}
		for _, v := range r.Contents {
			if len(out) == limit {
				return nil, errors.New("s3: listing exceeds object limit")
			}
			out = append(out, object{Key: aws.ToString(v.Key), Bytes: aws.ToInt64(v.Size), Modified: aws.ToTime(v.LastModified)})
		}
		if !aws.ToBool(r.IsTruncated) {
			return out, nil
		}
		if r.NextContinuationToken == nil || aws.ToString(r.NextContinuationToken) == aws.ToString(token) {
			return nil, errors.New("s3: invalid continuation")
		}
		token = r.NextContinuationToken
	}
}
func dial(ctx context.Context, c Config, key string) (bucket, error) {
	if c.System == "gcs" {
		client, err := storage.NewClient(ctx, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(key)))
		if err != nil {
			return nil, err
		}
		return &gcsBucket{client, c.Bucket}, nil
	}
	var cred struct {
		Access string `json:"access_key_id"`
		Secret string `json:"secret_access_key"`
		Token  string `json:"session_token"`
	}
	if json.Unmarshal([]byte(key), &cred) != nil || cred.Access == "" || cred.Secret == "" {
		return nil, fmt.Errorf("s3: credential requires access_key_id and secret_access_key")
	}
	region := c.Region
	if region == "" {
		region = "us-east-1"
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cred.Access, cred.Secret, cred.Token)))
	if err != nil {
		return nil, err
	}
	return &s3Bucket{s3.NewFromConfig(cfg), c.Bucket}, nil
}
