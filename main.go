package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/pkg/errors"

	"s3-version-restore/s3"
)

func main() {
	var (
		verbose bool
		prefix  string
	)
	flag.Usage = func() {
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "%s bucket-name RFC3339-timestamp\n", os.Args[0])
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "Flags:\n")
		flag.PrintDefaults()
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), "Example: %s -verbose mybucket \"2023-08-17T18:50:00+02:00\"\n\n", os.Args[0])
	}
	flag.BoolVar(&verbose, "verbose", false, "print debug information")
	flag.StringVar(&prefix, "prefix", "", "only work on a given prefix")
	flag.Parse()
	if len(flag.Args()) != 2 {
		flag.Usage()
		os.Exit(2)
	}

	ctx := context.Background()
	logLevel := slog.LevelInfo
	if verbose {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	bucket := flag.Args()[0]
	referenceTimestamp, err := time.Parse(time.RFC3339, flag.Args()[1])
	if err != nil {
		slog.ErrorContext(ctx, "invalid timestamp:", "err", err)
		os.Exit(1)
	}

	s3Client, err := s3.New(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "could not initialize s3 client:", "err", err)
		os.Exit(1)
	}
	versionIt, err := s3Client.List(ctx, bucket, nil)
	if err != nil {
		slog.ErrorContext(ctx, "could not list versions", "err", err)
		os.Exit(1)
	}
	for {
		key, versions, err := versionIt.Next(ctx)
		if errors.Is(err, io.EOF) {
			break // done
		} else if err != nil {
			slog.ErrorContext(ctx, "could not list versions", "err", err)
			os.Exit(1)
		}
		slog.Debug("checking", "key", key, "versions", versions)
		var versionAtTimestamp s3.ObjectVersion
		var latestVersion s3.ObjectVersion
		for _, v := range versions {
			if v.Timestamp.Before(referenceTimestamp) {
				versionAtTimestamp = v
			}
			if v.IsLatest {
				latestVersion = v
			}
		}

		if latestVersion.ETag == versionAtTimestamp.ETag {
			slog.InfoContext(ctx, "skipping", "key", key, "etag", versionAtTimestamp.ETag)
			continue // already at the state we want, nothing to do
		}

		// 3 things can happen at referenceTimestamp:
		// - file did not exist (no previous PUT) --> delete
		// - file had an active version (a previous PUT) --> copy
		// - file had been deleted (a previous DELETE) --> delete
		if versionAtTimestamp == s3.ZeroObjectVersion || versionAtTimestamp.Operation == s3.OperationTypeDelete { // delete
			slog.InfoContext(ctx, "deleting", "key", key, "version", versionAtTimestamp.VersionID)
			// delete the latest version of the object, this preserves history
			err := s3Client.Delete(ctx, bucket, key)
			if err != nil {
				slog.Error("could not delete object", "key", key, "err", err)
				os.Exit(1)
			}
		} else { // restore
			slog.InfoContext(ctx, "restoring", "key", key, "version", versionAtTimestamp.VersionID)
			// copy the desired version of the object as last, this preserves history
			err := s3Client.Copy(ctx, bucket, bucket, key, versionAtTimestamp.VersionID)
			if err != nil {
				slog.Error("could not restore object", "key", key, "version", versionAtTimestamp.VersionID, "err", err)
				os.Exit(1)
			}
		}
	}
}
