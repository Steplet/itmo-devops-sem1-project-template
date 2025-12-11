#!/bin/bash
set -e

echo "Installing Go modules..."
go mod tidy
go mod download

echo "Installed Go modules"

source .env

DB_HOST=${POSTGRES_HOST:-localhost}
DB_PORT=${POSTGRES_PORT:-5432}
DB_NAME=${POSTGRES_DB:-appdb}
DB_USER=${POSTGRES_USER:-postgres}
DB_PASS=${POSTGRES_PASSWORD:-postgres}


echo "SetUp DB..."

CONNECTION_STRING="postgresql://${DB_USER}:${DB_PASS}@${DB_HOST}:${DB_PORT}/${DB_NAME}"
psql "$CONNECTION_STRING" -f init.sql

echo "SetUp DB end!"