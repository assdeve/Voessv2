#!/bin/sh
# Запускать НА РОУТЕРЕ из распакованной папки релиза:  sh install-openwrt.sh
set -e
D="$(cd "$(dirname "$0")" && pwd)"
[ -f "$D/voess2" ] || { echo "Файл voess2 не найден рядом со скриптом."; exit 1; }

echo "Устанавливаю ca-bundle (нужен для сертификата Let's Encrypt) ..."
(opkg update >/dev/null 2>&1 && opkg install ca-bundle) || echo "Предупреждение: ca-bundle не установлен, поставьте вручную."

mkdir -p /etc/voess
cp "$D/voess2" /usr/bin/voess2 || { echo "Не хватает места в /usr/bin. Положите программу на USB-флешку и поправьте путь в /etc/init.d/voess2"; exit 1; }
chmod +x /usr/bin/voess2
cp "$D/voess2.init" /etc/init.d/voess2
chmod +x /etc/init.d/voess2

if [ ! -f /etc/voess/server.json ]; then
  cp "$D/server-router.json" /etc/voess/server.json
  UUID=$(/usr/bin/voess2 genuuid)
  sed -i "s/ВАШ-UUID-ИЗ-genuuid/$UUID/" /etc/voess/server.json
  chmod 600 /etc/voess/server.json
  echo
  echo "Ваш UUID (запишите, он нужен клиенту): $UUID"
fi

echo
echo "Дальше (подробности в OPENWRT.md):"
echo " 1) отредактируйте /etc/voess/server.json: domain и список allow (адреса вашего домашнего сервера)"
echo " 2) освободите порт 443 у LuCI и добавьте правило файрвола WAN -> Device, TCP 443"
echo " 3) /etc/init.d/voess2 enable && /etc/init.d/voess2 start"
