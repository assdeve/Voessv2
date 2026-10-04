# VOESS v2 на роутере OpenWrt (доступ к домашнему серверу и файлам)

## Что нужно заранее
- Белый (публичный) IP. Сравните WAN IP в статусе роутера с тем, что показывает ifconfig.me.
  Если не совпадают или WAN начинается с 100.64-100.127 / 10. - это CGNAT, сервер дома невозможен.
- Адрес-имя: бесплатный DuckDNS (myhome.duckdns.org) + пакеты ddns-scripts, luci-app-ddns на роутере.

## 1. Сборка на компьютере (папка voess2)
Сначала узнайте архитектуру роутера:  ssh root@192.168.1.1 "uname -m; opkg print-architecture; df -h /overlay; free -m"

Windows PowerShell (пример для mipsel, подставьте свою архитектуру):
    $env:GOOS="linux"; $env:GOARCH="mipsle"; $env:GOMIPS="softfloat"
    go mod tidy
    go build -tags server -ldflags="-s -w" -o voess2-router .
Другие архитектуры: arm64 -> GOARCH="arm64";  ARMv7 -> GOARCH="arm"; $env:GOARM="7"
Если файл не влезает: upx --best voess2-router, либо положить на USB-флешку.

Клиент для своего компьютера - обычная сборка (без -tags server):
    $env:GOOS="windows"; $env:GOARCH="amd64"; go build -o voess2.exe .

## 2. Подготовка роутера
    ssh root@192.168.1.1
    mkdir -p /etc/voess
Для выпуска сертификата нужны корневые сертификаты и верное время:
    opkg update && opkg install ca-bundle
    date    # время должно быть правильным (NTP включён по умолчанию)
LuCI (uhttpd) занимает 443 - переведите на LAN-порт 8443:
    uci -q delete uhttpd.main.listen_https
    uci add_list uhttpd.main.listen_https='192.168.1.1:8443'
    uci commit uhttpd && /etc/init.d/uhttpd restart

## 3. Копирование файлов (с компьютера)
    scp -O voess2-router root@192.168.1.1:/usr/bin/voess2
    scp -O server-router.json root@192.168.1.1:/etc/voess/server.json
    scp -O voess2.init root@192.168.1.1:/etc/init.d/voess2
    ssh root@192.168.1.1 "chmod +x /usr/bin/voess2 /etc/init.d/voess2"
(в server.json поправьте domain, uuid и IP вашего сервера в allow)

## 4. Файрвол (LuCI)
Network -> Firewall -> Traffic Rules -> Add:
  Protocol TCP, Source zone wan, Destination zone "Device (input)", Destination port 443, Action accept.
Port Forward для 443 НЕ нужен: сервис работает на самом роутере.
Никогда не пробрасывайте наружу веб-интерфейс роутера.

## 5. Запуск
    /etc/init.d/voess2 enable && /etc/init.d/voess2 start
    logread -e voess2
Проверка снаружи (например, с мобильного интернета): https://myhome.duckdns.org покажет страницу Welcome.

## 6. Клиент
В client.json пропишите server, uuid и forwards, затем:   voess2.exe client -c client.json
Пока клиент запущен, ваш сервер доступен локально:
  SSH/SFTP:  ssh -p 2222 user@127.0.0.1     (WinSCP: хост 127.0.0.1, порт 2222)
  Веб-панель: http://127.0.0.1:8080
  RDP:        127.0.0.1:3390

## Безопасность
- UUID - единственный ключ. Не публикуйте его. Для каждого устройства делайте свой UUID (users в конфиге).
- allow + home_only:true разрешают только перечисленные адреса дома. Остальная сеть и сам роутер недоступны.
- Не включайте log_access без необходимости.

## Проверка на компьютере ДО установки на роутер
1. Сертификат для теста:
       openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -keyout key.pem -out cert.pem -days 30 -subj "/CN=localhost"
2. test-server.json:
       {"listen":"127.0.0.1:8443","cert_file":"cert.pem","key_file":"key.pem",
        "users":[{"name":"t","uuid":"UUID-ИЗ-genuuid"}],
        "allow":["127.0.0.1:8000"],"home_only":true}
3. test-client.json:
       {"server":"localhost:8443","uuid":"ТОТ-ЖЕ-UUID","insecure":true,"socks_listen":"127.0.0.1:1081",
        "forwards":[{"listen":"127.0.0.1:9000","target":"127.0.0.1:8000"}]}
4. Запуск в трёх окнах: `python -m http.server 8000`, `voess2 server -c test-server.json`, `voess2 client -c test-client.json`.
5. http://127.0.0.1:9000 должен показать список файлов (значит, туннель работает),
   а https://localhost:8443 в браузере - страницу Welcome (значит, маскировка работает).
