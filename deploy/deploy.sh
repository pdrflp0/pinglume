#!/bin/zsh
set -eu

LABEL=${1:?uso: deploy.sh <rótulo> <região> <ip público> [sem-udp]}
REGION=${2:?região}
IP=${3:?ip público}
UDP_MODE=${4:-udp}
SSH_USER=${SSH_USER:-ubuntu}
SSH_KEY=${SSH_KEY:-$HOME/.ssh/pinglume}
ROOT=${0:A:h:h}
LIST="$ROOT/agent/internal/endpoints/production.json"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

ssh_opts=(-i "$SSH_KEY" -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15)

case $(ssh "${ssh_opts[@]}" "$SSH_USER@$IP" uname -m) in
aarch64) ARCH=arm64 ;;
x86_64) ARCH=amd64 ;;
*)
	echo "arquitetura não suportada" >&2
	exit 1
	;;
esac

(cd "$ROOT/probe-server" && CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH go build -trimpath -o "$WORK/probe-server" .)
cp "$ROOT/deploy/install.sh" "$ROOT/deploy/pinglume-probe.service" "$WORK/"

REMOTE=/tmp/pinglume-deploy
ssh "${ssh_opts[@]}" "$SSH_USER@$IP" "rm -rf $REMOTE && mkdir -p $REMOTE"
scp -q "${ssh_opts[@]}" "$WORK/probe-server" "$WORK/install.sh" "$WORK/pinglume-probe.service" "$SSH_USER@$IP:$REMOTE/"
ENTRY=$(ssh "${ssh_opts[@]}" "$SSH_USER@$IP" \
	"cd $REMOTE && sudo bash install.sh '$LABEL' '$REGION' '$IP' '$UDP_MODE' && rm -rf $REMOTE")

jq --argjson e "$ENTRY" 'map(select(.label != $e.label)) + [$e]' "$LIST" >"$WORK/list.json"
cp "$WORK/list.json" "$LIST"

if curl -sk -m 10 "https://$IP/p" | grep -q pinglume-probe; then
	echo "HTTPS ok em $IP:443"
else
	echo "HTTPS sem resposta em $IP:443: confira a regra de entrada TCP 443 no provedor" >&2
	exit 1
fi
if [ "$UDP_MODE" = udp ]; then
	if { printf 'PGLU-teste'; sleep 3; } | nc -u -w 3 "$IP" 443 | grep -q PGLU; then
		echo "eco UDP ok em $IP:443"
	else
		echo "eco UDP sem resposta em $IP:443: confira a regra de entrada UDP 443 no provedor" >&2
		exit 1
	fi
fi
echo "endpoint $LABEL gravado em $LIST"
