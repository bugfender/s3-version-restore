#!/usr/bin/env bash
set -e
set -o pipefail

platforms=("windows/amd64" "windows/arm64" "linux/amd64" "linux/arm64" "darwin/amd64" "darwin/arm64")
outdir="$PWD/build"
mkdir -p "$outdir"

for platform in "${platforms[@]}"
do
  echo "- $platform"
  platform_split=(${platform//\// })
  GOOS=${platform_split[0]}
  GOARCH=${platform_split[1]}
  output_name=s3-version-restore'-'$GOOS'-'$GOARCH
  if [ "$GOOS" = "windows" ]; then
    output_name+='.exe'
  fi

  env "GOOS=$GOOS" "GOARCH=$GOARCH" go build -o "$outdir/$output_name" .
  if [ $? -ne 0 ]; then
      echo 'An error has occurred! Aborting the script execution...'
    exit 1
  fi
done
