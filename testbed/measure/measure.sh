#!/bin/zsh
set -eu

MINUTES=${1:?uso: measure.sh <minutos> <diretório de saída>}
OUT=${2:?uso: measure.sh <minutos> <diretório de saída>}
ROOT=${0:A:h:h:h}

mkdir -p "$OUT/bin" "$OUT/data" "$OUT/tls"
cp "$ROOT/agent/bin/pinglume" "$ROOT/probe-server/bin/probe-server" "$OUT/bin/"

cleanup() {
  [[ -n "${AGENT:-}" ]] && kill -INT "$AGENT" 2>/dev/null || true
  sleep 2
  for p in "${SERVERS[@]}"; do kill "$p" 2>/dev/null || true; done
}
trap cleanup EXIT INT TERM

SERVERS=()
for i in 1 2 3; do
  "$OUT/bin/probe-server" -tcp 127.0.0.1:945$i -udp 127.0.0.1:945$i -dns 127.0.0.1:1536$i -admin 127.0.0.1:996$i -tls-dir "$OUT/tls" > "$OUT/probe-server-$i.log" 2>&1 &
  SERVERS+=($!)
  for _ in {1..50}; do [[ -f "$OUT/tls/probe-server.pem" ]] && break; sleep 0.1; done
done

"$OUT/bin/pinglume" run --data-dir "$OUT/data" --port 47850 \
  --dev-target ep-1=127.0.0.1:9451 --dev-target ep-2=127.0.0.1:9452 --dev-target ep-3=127.0.0.1:9453 \
  --dev-dns ep-1=127.0.0.1:15361 --dev-dns ep-2=127.0.0.1:15362 --dev-dns ep-3=127.0.0.1:15363 \
  --dev-udp ep-1=127.0.0.1:9451 \
  --dev-ca "$OUT/tls/probe-server.pem" > "$OUT/agent.log" 2>&1 &
AGENT=$!
sleep 2

{
  echo "inicio=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "minutos=$MINUTES"
  echo "macos=$(sw_vers -productVersion) ($(sw_vers -buildVersion))"
  echo "cpu=$(sysctl -n machdep.cpu.brand_string), $(sysctl -n hw.ncpu) núcleos"
  echo "memoria_gb=$(( $(sysctl -n hw.memsize) / 1073741824 ))"
  echo "go=$(go version | awk '{print $3}')"
  echo "agente=$("$OUT/bin/pinglume" version | awk '{print $2}')"
  echo "commit=$(git -C "$ROOT" rev-parse --short HEAD)"
  echo "sondas=3 endpoints × (tcp a cada 10 s, dns a cada 30 s, https a cada 60 s) + udp_echo a cada 15 s em 1 endpoint, intervalos padrão"
  echo "esperado_h=1860"
} > "$OUT/env.txt"

echo "ts,rss_kb,cpu_s,bytes_in,bytes_out,db_bytes" > "$OUT/samples.csv"
END=$(( $(date +%s) + MINUTES * 60 ))
while (( $(date +%s) < END )); do
  kill -0 "$AGENT" 2>/dev/null || { echo "agente parou" >> "$OUT/env.txt"; break; }
  NET=$(nettop -P -d -L 2 -s 60 -p "$AGENT" -x -J bytes_in,bytes_out 2>/dev/null | awk -F, 'NR==4 {print $2","$3}')
  read RSS CPU <<< "$(ps -o rss=,time= -p "$AGENT")"
  CPU_S=$(echo "$CPU" | awk -F'[:.]' '{ if (NF==3) print $1*60+$2+$3/100; else print $1*3600+$2*60+$3+$4/100 }')
  DB=$(sqlite3 "$OUT/data/pinglume.db" "SELECT (SELECT page_count FROM pragma_page_count()) - (SELECT freelist_count FROM pragma_freelist_count()); SELECT page_size FROM pragma_page_size();" | paste -sd' ' - | awk '{print $1*$2}')
  echo "$(date +%s),$RSS,$CPU_S,${NET:-0,0},$DB" >> "$OUT/samples.csv"
done

kill -INT "$AGENT" 2>/dev/null || true
for _ in {1..50}; do kill -0 "$AGENT" 2>/dev/null || break; sleep 0.1; done
AGENT=
"${0:A:h}/collect.sh" "$OUT"
echo "fim=$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$OUT/env.txt"
