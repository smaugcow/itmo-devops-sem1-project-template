# цены продуктов

сложный уровень: Go HTTP API и PostgreSQL 15 в Docker, сервер на 8080, база project-sem-1 на 5432, учебный пользователь validator с паролем val1dat0r

```sh
bash scripts/prepare.sh
docker compose up -d
bash scripts/tests.sh 3
curl -F file=@sample_data.zip 'http://localhost:8080/api/v0/prices?type=zip'
curl 'http://localhost:8080/api/v0/prices?start=2024-01-01&end=2024-01-31&min=300&max=1000' -o prices.zip
bash scripts/run.sh
```

POST принимает zip по умолчанию или tar через type, архив передаётся телом запроса либо multipart-полем file, csv содержит id, name, category, price, create_date

неполные строки, некорректные даты, id и цены пропускаются, total_count считает прочитанные csv-записи без заголовка, total_items — добавленные строки, duplicates_count — совпадения с уже сохранёнными строками, включая повторы внутри загрузки

дубликат определён как совпадение всех пяти полей, цена сравнивается численно, это выбранное правило для неоднозначного условия задания, total_categories и total_price считаются по всей базе

GET возвращает zip с data.csv, необязательные start/end задают включительные границы даты YYYY-MM-DD, min/max — включительные положительные целочисленные границы цены, неверные фильтры дают 400

prepare.sh собирает linux/amd64 образ, для другого target можно задать DOCKER_PLATFORM, локальная база доступна только через loopback, рабочие данные сохраняются в Docker volume

run.sh требует yc, jq, Docker, SSH и curl, создаёт либо повторно использует одну прерываемую ВМ Ubuntu 24.04 с 2 vCPU, 2 GB RAM и диском 20 GB, резервное копирование не настраивается, ВМ и диск автоматически не удаляются

перед облачным запуском задать YC_PROFILE, YC_CLOUD_ID, YC_FOLDER_ID, YC_SUBNET_ID, YC_ZONE, YC_SECURITY_GROUP_ID, VM_NAME, SSH_PUBLIC_KEY_FILE, SSH_PRIVATE_KEY_FILE и CONFIRM_CREATE=yes, SSH_USER по умолчанию ubuntu

VM_NAME имеет вид n-UUID и сохраняется для повторных запусков, STATE_DIR по умолчанию .runtime/VM_NAME, не менять имя ради повтора операции, чужая ВМ с тем же именем или исчезнувшая записанная ВМ приводят к отказу

публичный ключ сервера берётся из serial output конкретной ВМ через аутентифицированный API, SSH проверяет его строго, приватные ключи не выводятся, образ передаётся через docker save и SSH, итог успешного run.sh — IP сервера

существующая security group должна разрешать SSH 22 и API 8080, PostgreSQL наружу не публикуется, скрипт не меняет правила сети и не подключает сервисный аккаунт к ВМ

CI на push и PR проверяет Go, Docker и неизменённые официальные тесты всех трёх уровней, cloud job проверяет API и запускает неизменённые тесты сложного уровня на новой ВМ через SSH, запускается отдельно через workflow_dispatch с confirm_create=true только из main

для cloud job нужны secrets YC_SA_JSON и SSH_PRIVATE_KEY, variables YC_CLOUD_ID, YC_FOLDER_ID, YC_SUBNET_ID, YC_ZONE, YC_SECURITY_GROUP_ID, VM_NAME, SSH_USER и SSH_PUBLIC_KEY, JSON авторизованного ключа сервисного аккаунта вводится только в secrets

прерываемая ВМ может остановиться, её автозапуск при необходимости настраивается отдельно, данные остаются на диске, стоимость ресурсов продолжается до их удаления пользователем
