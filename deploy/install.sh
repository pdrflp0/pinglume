#!/bin/bash
set -euo pipefail

LABEL=${1:?uso: install.sh <rótulo> <região> <ip público> <udp|sem-udp>}
REGION=${2:?região}
PUBLIC_IP=${3:?ip público}
UDP_MODE=${4:?udp ou sem-udp}
PORT=443
STATE=/var/lib/pinglume-probe
SERVER_NAME="$LABEL.pinglume.invalid"

case "$UDP_MODE" in
udp) UDP_ADDR="0.0.0.0:$PORT" ;;
sem-udp) UDP_ADDR="" ;;
*)
	echo "modo UDP desconhecido: $UDP_MODE" >&2
	exit 2
	;;
esac

open_port() {
	command -v iptables >/dev/null || return 0
	iptables -C INPUT -p "$1" --dport "$PORT" -j ACCEPT 2>/dev/null ||
		iptables -I INPUT 1 -p "$1" --dport "$PORT" -j ACCEPT
}

id pinglume >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin pinglume
install -m 755 probe-server /usr/local/bin/pinglume-probe-server
install -m 644 pinglume-probe.service /etc/systemd/system/pinglume-probe.service

ARGS="-tcp=0.0.0.0:$PORT -udp=$UDP_ADDR -dns= -admin= -tls-dir=$STATE -server-name=$SERVER_NAME -cert-years=10"
printf 'PROBE_ARGS=%s\n' "$ARGS" >/etc/pinglume-probe.env

open_port tcp
[ -z "$UDP_ADDR" ] || open_port udp
if command -v netfilter-persistent >/dev/null; then
	netfilter-persistent save >/dev/null 2>&1
fi

systemctl daemon-reload
systemctl enable pinglume-probe >/dev/null 2>&1
systemctl restart pinglume-probe

for _ in $(seq 1 20); do
	(exec 3<>"/dev/tcp/127.0.0.1/$PORT") 2>/dev/null && break
	sleep 0.5
done
(exec 3<>"/dev/tcp/127.0.0.1/$PORT")

/usr/local/bin/pinglume-probe-server -print-endpoint="$LABEL" -region="$REGION" -public-ip="$PUBLIC_IP" \
	-tls-dir="$STATE" -server-name="$SERVER_NAME" -tcp="0.0.0.0:$PORT" -udp="$UDP_ADDR"
