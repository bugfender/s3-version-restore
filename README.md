# S3 Version Restore

Point-in-time recovery of an S3 bucket.

This tool iterates over all objects in a bucket and restores versions at a given timestamp.

## How it works

It modifies the version information in place, so objects are never downloaded/uploaded.

Version information is always added, so information is never lost. Object versions are restored by creating a new 
version with the desired contents.

It is safe to use multiple times to restore different time points.

## Environment variables

You can use [AWS standard environment variables](https://github.com/aws/aws-sdk-go-v2/blob/main/config/env_config.go#L4).
You will typically want to declare `AWS_ACCESS_KEY_ID` and `AWS_ACCESS_KEY`.

You can also define `AWS_ENDPOINT_URL_S3` to specify a custom S3 endpoint.

Note: [endpoint URLs are currently specified](https://docs.aws.amazon.com/sdkref/latest/guide/feature-ss-endpoints.html) but the Go AWS SDK does not implement them, so we implement `AWS_ENDPOINT_URL_S3`.

## Building

```
go build
```

There is also a `build.sh` file to build for multiple platforms.

## Support

This tool is open source and free to use (see `LICENSE`), but you can get commercial 
support from Beenario GmbH for a fee. Contact us if you're interested.
