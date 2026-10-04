#!/bin/sh
# Установка сервера VOESS v2 на Linux-сервер (VPS) одной командой.
#   curl -fsSL https://github.com/OWNER/voess/releases/latest/download/install-linux.sh | sudo sh
# Репозиторий можно задать так:  VOESS_REPO=ник/voess sh install-linux.sh
set -eu
REPO="${VOESS_REPO:-OWNER/voess}"

[ "$(id -u)" = 0 ] || { echo "Запустите от root (sudo)."; exit 1; }
command -v systemctl >/dev/null || { echo "Нужен systemd."; exit 1; }

case "$(uname -m)" in
  x86_64|amd64)  A=linux-amd64 ;;
  aarch64|arm64) A=linux-arm64 ;;
  armv7l|armv7)  A=linux-armv7 ;;
  *) echo "Неподдерживаемая архитектура: $(uname -m)"; exit 1 ;;
esac

echo "Ищу последнюю версию в $REPO ..."
TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
  | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
[ -n "$TAG" ] || { echo "Не удалось найти релиз. Проверьте VOESS_REPO и наличие релиза."; exit 1; }

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
echo "Скачиваю $TAG ($A) ..."
curl -fsSL "https://github.com/$REPO/releases/download/$TAG/voess2-$TAG-$A.tar.gz" | tar -xz -C "$TMP"
D="$TMP/voess2-$TAG-$A"

install -m 755 "$D/voess2" /usr/local/bin/voess2
mkdir -p /etc/voess

if [ ! -f /etc/voess/server.json ]; then
  printf "Домен сервера (A-запись должна указывать на этот сервер): "
  read -r DOMAIN < /dev/tty
  UUID=$(/usr/local/bin/voess2 genuuid)
  cat > /etc/voess/server.json <<EOF
{
  "listen": ":443",
  "domain": "$DOMAIN",
  "users": [ { "name": "me", "uuid": "$UUID" } ]
}
EOF
  chmod 600 /etc/voess/server.json
  echo
  echo "Ваш UUID (запишите, он нужен клиенту): $UUID"
fi

install -m 644 "$D/examples/voess2.service" /etc/systemd/system/voess2.service
systemctl daemon-reload
systemctl enable --now voess2

echo
echo "Готово. Статус:   systemctl status voess2"
echo "Конфиг сервера:   /etc/voess/server.json"
echo "Откройте порт 443/tcp в файрволе, например: ufw allow 443/tcp"
