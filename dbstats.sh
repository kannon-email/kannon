#!/bin/bash

dbmate --migrations-dir stats_db/migrations --env "STATS_DATABASE_URL"  --schema-file stats_db/schema.sql $@