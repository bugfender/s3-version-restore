package s3

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pkg/errors"
)

type Client struct {
	c *s3.Client
}

func New(ctx context.Context) (*Client, error) {
	cfg, err := makeAwsConfigWithEnvExtensions(ctx)
	if err != nil {
		return nil, err
	}
	s3Client := s3.NewFromConfig(cfg)
	return &Client{c: s3Client}, nil
}

func makeAwsConfigWithEnvExtensions(ctx context.Context) (aws.Config, error) {
	// note: AWS_ENDPOINT_URL_S3 is specified by AWS but not yet implemented in the SDK
	// https://docs.aws.amazon.com/sdkref/latest/guide/feature-ss-endpoints.html
	optFns := make([]func(*config.LoadOptions) error, 0)
	if endpointURL := os.Getenv("AWS_ENDPOINT_URL_S3"); endpointURL != "" {
		optFns = append(optFns, config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{URL: endpointURL}, nil
			})))
	}
	// load the SDK's default configuration, loading additional config
	// and credentials values from the environment variables, shared
	// credentials, and shared configuration files
	cfg, err := config.LoadDefaultConfig(ctx, optFns...)
	return cfg, errors.Wrap(err, "loading aws config")
}

type OperationType string

var (
	OperationTypePut    OperationType = "PUT"
	OperationTypeDelete OperationType = "DELETE"
)

type ObjectVersion struct {
	VersionID string
	Operation OperationType
	Timestamp time.Time
	IsLatest  bool
	ETag      string
}

var ZeroObjectVersion = ObjectVersion{}

type ObjectVersionMap map[string][]ObjectVersion

// Add adds an element to the version map, guarantees the map is always initialized and sorted by timestamp
func (ovm *ObjectVersionMap) Add(key string, versionID string, operation OperationType, timestamp time.Time, isLatest bool, etag string) {
	var sl []ObjectVersion
	if foundSlice, found := (*ovm)[key]; found {
		sl = foundSlice
	} else {
		sl = make([]ObjectVersion, 0)
		if count := len(*ovm) + 1; count%1000 == 0 {
			slog.Debug("loading objects", "count", count)
		}
	}
	sl = append(sl, ObjectVersion{
		VersionID: versionID,
		Operation: operation,
		Timestamp: timestamp,
		IsLatest:  isLatest,
		ETag:      etag,
	})
	slices.SortFunc(sl, func(a, b ObjectVersion) int {
		if a.Timestamp.Before(b.Timestamp) {
			return -1
		} else if a.Timestamp.Equal(b.Timestamp) {
			return 0
		}
		return 1
	})
	(*ovm)[key] = sl
}

// List needs s3:ListBucketVersions permission
func (c *Client) List(ctx context.Context, bucket string, prefix *string) (ListIterator, error) {
	return ListIterator{
		c:                       c,
		bucket:                  bucket,
		prefix:                  prefix,
		loadMore:                true,
		keyMarker:               nil,
		versionIdMarker:         nil,
		pageObjectsWithVersions: make(ObjectVersionMap),
	}, nil
}

type ListIterator struct {
	c                       *Client
	bucket                  string
	prefix                  *string
	loadMore                bool
	keyMarker               *string
	versionIdMarker         *string
	pageObjectsWithVersions ObjectVersionMap
}

func (it *ListIterator) Next(ctx context.Context) (string, []ObjectVersion, error) {
	// we know objects are sorted alphabetically, therefore it is safe to read the first element when we have more than one
	for it.loadMore && len(it.pageObjectsWithVersions) <= 1 {
		// https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListObjectVersions.html
		versions, err := it.c.c.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket:          &it.bucket,
			Prefix:          it.prefix,
			KeyMarker:       it.keyMarker,
			VersionIdMarker: it.versionIdMarker,
			MaxKeys:         10000,
		})
		if err != nil {
			return "", nil, errors.Wrap(err, "listing object versions")
		}

		it.loadMore = versions.IsTruncated
		it.keyMarker = versions.NextKeyMarker
		it.versionIdMarker = versions.NextVersionIdMarker

		// store version info
		for _, v := range versions.Versions {
			it.pageObjectsWithVersions.Add(*v.Key, *v.VersionId, OperationTypePut, *v.LastModified, v.IsLatest, *v.ETag)
		}
		for _, v := range versions.DeleteMarkers {
			it.pageObjectsWithVersions.Add(*v.Key, *v.VersionId, OperationTypeDelete, *v.LastModified, v.IsLatest, "")
		}
	}
	// here we have either more than one object (safe to read the first one),
	// or the last object and no more pages to load (safe to read the remaining one),
	// or no objects
	if len(it.pageObjectsWithVersions) == 0 {
		return "", nil, io.EOF
	}
	key := firstMapKey(it.pageObjectsWithVersions)
	versions := it.pageObjectsWithVersions[key]
	delete(it.pageObjectsWithVersions, key)
	return key, versions, nil
}

func firstMapKey[K cmp.Ordered, V any](m map[K]V) K {
	if len(m) == 0 {
		panic("map is empty")
	}
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys[0]
}

func (c *Client) Copy(ctx context.Context, srcBucket string, destBucket string, key string, version string) error {
	sourceWithVersion := fmt.Sprintf("%s/%s?versionId=%s", srcBucket, key, version)
	_, err := c.c.CopyObject(ctx, &s3.CopyObjectInput{
		CopySource: &sourceWithVersion,
		Bucket:     &destBucket,
		Key:        &key,
	})
	return errors.Wrap(err, "copying object: "+sourceWithVersion)
}

func (c *Client) Delete(ctx context.Context, bucket string, key string) error {
	_, err := c.c.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	return errors.Wrap(err, "deleting object: "+key)
}
