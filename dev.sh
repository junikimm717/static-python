#!/bin/sh

set -eu

docker compose pull spython
docker compose up -d spython
docker compose exec spython ash
