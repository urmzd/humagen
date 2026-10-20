package blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// S3Config says where an S3-compatible bucket is: AWS S3 itself, MinIO,
// Cloudflare R2, or anything else that speaks the same API.
type S3Config struct {
	// Bucket is required.
	Bucket string
	// Region is the bucket's region. R2 takes "auto". Empty falls back to the
	// environment, then to us-east-1.
	Region string
	// Endpoint is the service's base URL, such as http://127.0.0.1:9000 for
	// MinIO or https://ACCOUNT.r2.cloudflarestorage.com for R2. Empty means
	// AWS.
	Endpoint string
	// PathStyle addresses the bucket in the path (endpoint/bucket/key)
	// instead of the host name. MinIO usually needs it.
	PathStyle bool
	// Prefix is put before every object key, so one bucket can hold more than
	// one server's files. A trailing slash is added if it has none.
	Prefix string
	// AccessKey and SecretKey are static credentials. Empty means the AWS
	// default chain: the environment, the shared files, then the instance's
	// role. A server reads them from the environment, never from flags.
	AccessKey string
	SecretKey string
	// HTTPClient replaces the client requests go through. Nil means the SDK's.
	HTTPClient *http.Client
}

// S3 keeps bytes as objects in a bucket, under prefix/tenant/key.
//
// Each object carries the SHA-256 of its bytes in its metadata, computed here
// when it is put, so Stat and List can answer without reading it back. S3
// replaces an object in one step, so a reader sees the old bytes or the new
// ones. Delete looks before it deletes, because S3's own delete succeeds on a
// missing key and the Store contract says it must not.
type S3 struct {
	client *s3.Client
	bucket string
	prefix string
}

// metaSHA256 is the user metadata key the hash is kept under. S3 returns it
// lower case whatever case it was sent in.
const metaSHA256 = "sha256"

// NewS3 returns a store for a bucket. It does not contact the bucket: a wrong
// setting shows on the first call.
func NewS3(ctx context.Context, c S3Config) (*S3, error) {
	if strings.TrimSpace(c.Bucket) == "" {
		return nil, fmt.Errorf("%w: an S3 store needs a bucket", ErrInvalid)
	}
	prefix := strings.Trim(c.Prefix, "/")
	if prefix != "" {
		if err := check("x", prefix); err != nil {
			return nil, fmt.Errorf("%w: prefix %q", ErrInvalid, c.Prefix)
		}
		prefix += "/"
	}
	opts := []func(*config.LoadOptions) error{
		// Only what S3 requires. Some S3-compatible services refuse the
		// checksums the SDK adds by default, and the hash is kept separately.
		config.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		config.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	}
	if c.Region != "" {
		opts = append(opts, config.WithRegion(c.Region))
	}
	if c.AccessKey != "" || c.SecretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, "")))
	}
	if c.HTTPClient != nil {
		opts = append(opts, config.WithHTTPClient(c.HTTPClient))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("blob: s3: %w", err)
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = c.PathStyle
		if c.Endpoint != "" {
			o.BaseEndpoint = aws.String(c.Endpoint)
		}
	})
	return &S3{client: client, bucket: c.Bucket, prefix: prefix}, nil
}

func (s *S3) object(tenant, key string) string { return s.prefix + tenant + "/" + key }

// notFound reports whether S3 said there is no such object.
func notFound(err error) bool {
	var nsk *types.NoSuchKey
	var nf *types.NotFound
	if errors.As(err, &nsk) || errors.As(err, &nf) {
		return true
	}
	var api smithy.APIError
	if errors.As(err, &api) && (api.ErrorCode() == "NoSuchKey" || api.ErrorCode() == "NotFound") {
		return true
	}
	var re *smithyhttp.ResponseError
	return errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotFound
}

func (s *S3) fail(key string, err error) error {
	if notFound(err) {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return fmt.Errorf("blob: s3: %w", err)
}

// Put implements Store.
func (s *S3) Put(ctx context.Context, tenant, key string, r io.Reader, max int64) (Info, error) {
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
	if _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.object(tenant, key)),
		Body:          bytes.NewReader(b),
		ContentLength: aws.Int64(int64(len(b))),
		ContentType:   aws.String("application/octet-stream"),
		Metadata:      map[string]string{metaSHA256: hash},
	}); err != nil {
		return Info{}, fmt.Errorf("blob: s3: %w", err)
	}
	// The time is the bucket's, as Stat will report it.
	info, err := s.Stat(ctx, tenant, key)
	if err != nil {
		return Info{}, err
	}
	return Info{Key: key, Size: int64(len(b)), SHA256: hash, At: info.At}, nil
}

// Get implements Store. The bytes are read whole and checked against the hash
// they were stored with, so what a caller gets is what was put.
func (s *S3) Get(ctx context.Context, tenant, key string) (io.ReadCloser, Info, error) {
	if err := check(tenant, key); err != nil {
		return nil, Info{}, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.object(tenant, key))})
	if err != nil {
		return nil, Info{}, s.fail(key, err)
	}
	defer func() { _ = out.Body.Close() }()
	b, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, Info{}, fmt.Errorf("blob: s3: %w", err)
	}
	hash := sum(b)
	if want := out.Metadata[metaSHA256]; want != "" && want != hash {
		return nil, Info{}, fmt.Errorf("blob: s3: %s does not match the hash it was stored with", key)
	}
	return io.NopCloser(bytes.NewReader(b)), Info{Key: key, Size: int64(len(b)), SHA256: hash, At: aws.ToTime(out.LastModified)}, nil
}

// Stat implements Store.
func (s *S3) Stat(ctx context.Context, tenant, key string) (Info, error) {
	if err := check(tenant, key); err != nil {
		return Info{}, err
	}
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.object(tenant, key))})
	if err != nil {
		return Info{}, s.fail(key, err)
	}
	info := Info{Key: key, Size: aws.ToInt64(out.ContentLength), SHA256: out.Metadata[metaSHA256], At: aws.ToTime(out.LastModified)}
	if info.SHA256 == "" {
		// Put there by something other than this store: hash what is there.
		// Everything comes from the one read, so an overwrite between the
		// two requests cannot pair one version's size with another's hash.
		r, got, err := s.Get(ctx, tenant, key)
		if err != nil {
			return Info{}, err
		}
		_ = r.Close()
		return got, nil
	}
	return info, nil
}

// Delete implements Store.
func (s *S3) Delete(ctx context.Context, tenant, key string) error {
	if err := check(tenant, key); err != nil {
		return err
	}
	if _, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.object(tenant, key))}); err != nil {
		return s.fail(key, err)
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(s.object(tenant, key))}); err != nil {
		return s.fail(key, err)
	}
	return nil
}

// List implements Store. A listing carries no metadata, so each object listed
// is looked at once for its hash.
func (s *S3) List(ctx context.Context, tenant, prefix string) ([]Info, error) {
	if err := check(tenant, "x"); err != nil {
		return nil, err
	}
	root := s.prefix + tenant + "/"
	var keys []string
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(root + prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("blob: s3: %w", err)
		}
		for _, o := range page.Contents {
			if key, ok := strings.CutPrefix(aws.ToString(o.Key), root); ok && check(tenant, key) == nil {
				keys = append(keys, key)
			}
		}
	}
	slices.Sort(keys)
	out := make([]Info, 0, len(keys))
	for _, k := range keys {
		info, err := s.Stat(ctx, tenant, k)
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
