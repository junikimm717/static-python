#!/bin/sh

set -eu

# Build from this tree. GHCR :latest stays Alpine until this lands on master.
docker compose up -d --build spython
docker compose exec spython bash
