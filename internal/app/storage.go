package app

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/blob"
)

// Credentials for an S3-compatible store, read from the environment. Unset
// means the AWS default chain (AWS_ACCESS_KEY_ID and friends, shared files,
// an instance role).
const (
	s3AccessEnv = "HUMAGEN_S3_ACCESS_KEY_ID"
	s3SecretEnv = "HUMAGEN_S3_SECRET_ACCESS_KEY"
)

// storageFlags say where published files' bytes are kept.
type storageFlags struct {
	kind, dir                         *string
	bucket, region, endpoint, prefix  *string
	pathStyle                         *bool
	gcsBucket, gcsPrefix, gcsEndpoint *string
}

func addStorageFlags(fs *flag.FlagSet) storageFlags {
	return storageFlags{
		kind:        fs.String("storage", "disk", `where published files are kept: "disk", "memory" (lost on restart), "s3" for any S3-compatible bucket (AWS, MinIO, R2), or "gcs" for Google Cloud Storage`),
		dir:         fs.String("storage-dir", "", "with -storage=disk, the directory files are kept in. Empty means blobs/ in the agents' workspace"),
		bucket:      fs.String("s3-bucket", "", "with -storage=s3, the bucket"),
		region:      fs.String("s3-region", "", `with -storage=s3, the bucket's region ("auto" for R2). Empty means AWS_REGION, then us-east-1`),
		endpoint:    fs.String("s3-endpoint", "", "with -storage=s3, the service URL, such as http://127.0.0.1:9000 for MinIO. Empty means AWS"),
		prefix:      fs.String("s3-prefix", "", "with -storage=s3, a key prefix, so one bucket can serve more than one server"),
		pathStyle:   fs.Bool("s3-path-style", false, "with -storage=s3, put the bucket in the path rather than the host name, as MinIO usually needs"),
		gcsBucket:   fs.String("gcs-bucket", "", "with -storage=gcs, the bucket. Credentials are Application Default Credentials: Workload Identity, GOOGLE_APPLICATION_CREDENTIALS, or gcloud's"),
		gcsPrefix:   fs.String("gcs-prefix", "", "with -storage=gcs, an object name prefix, so one bucket can serve more than one server"),
		gcsEndpoint: fs.String("gcs-endpoint", "", "with -storage=gcs, an emulator's URL, such as http://127.0.0.1:4443, reached without credentials. Empty means Google Cloud Storage"),
	}
}

// openBlobs returns the store the flags name. Nil with no error means the
// platform's default: a disk store in its workspace.
func openBlobs(ctx context.Context, o Options, l loaded, f storageFlags, getenv func(string) string) (blob.Store, error) {
	switch strings.TrimSpace(*f.kind) {
	case "", "disk":
		if *f.dir == "" {
			return nil, nil
		}
		return blob.NewDisk(*f.dir)
	case "s3":
		access, secret := getenv(s3AccessEnv), getenv(s3SecretEnv)
		if (access == "") != (secret == "") {
			return nil, fmt.Errorf("storage: set both %s and %s, or neither", s3AccessEnv, s3SecretEnv)
		}
		return blob.NewS3(ctx, blob.S3Config{
			Bucket: *f.bucket, Region: *f.region, Endpoint: *f.endpoint, Prefix: *f.prefix, PathStyle: *f.pathStyle,
			AccessKey: access, SecretKey: secret,
		})
	case "gcs":
		return blob.NewGCS(ctx, blob.GCSConfig{Bucket: *f.gcsBucket, Prefix: *f.gcsPrefix, Endpoint: *f.gcsEndpoint})
	case "memory":
		return blob.NewMemory(), nil
	default:
		if l.cfg.System.Spec.Blobs.Kind == *f.kind {
			return build[blob.Store](ctx, o, "blobs", l.cfg.System.Spec.Blobs)
		}
		return nil, fmt.Errorf("storage: -storage=%q is not disk, memory, s3 or gcs", *f.kind)
	}
}
