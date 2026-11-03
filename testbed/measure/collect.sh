#!/bin/zsh
set -eu

OUT=${1:?uso: collect.sh <diretório da medição>}
DB="$OUT/data/pinglume.db"

sqlite3 -csv "$DB" "SELECT pd.kind, COUNT(*) FROM observations o JOIN probe_definitions pd ON pd.id = o.probe_def_id WHERE o.status != 'cancelled' GROUP BY pd.kind; SELECT 'span_ms', MAX(started_wall_ms) - MIN(started_wall_ms) FROM observations" > "$OUT/counts.csv"
sqlite3 -csv "$DB" "SELECT state, COUNT(*), SUM(end_wall_ms - start_wall_ms) FROM coverage_intervals GROUP BY state ORDER BY state" > "$OUT/coverage.csv"
sqlite3 -csv "$DB" "SELECT start_wall_ms, end_wall_ms FROM coverage_intervals WHERE state = 'suspended' ORDER BY start_wall_ms" > "$OUT/suspended.csv"
pmset -g log | awk '($4 == "Sleep" || $4 == "Wake" || $4 == "DarkWake") && $5 != "Requests" { print $1 " " $2 " " $3 "," $4 }' > "$OUT/pmset.csv"
