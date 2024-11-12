Демо-проект, который создал в процессе изучения работы с веб-сокетами.

В проекте запускаются 2 сервиса, использующие единое хранилище в OpenSearch:
- tagged-chat - приложение с одной страницей чата. Отправка и получение сообщений, а также рассылка тэгов для обновления сообщений происходит через веб-сокет; список тэгов и поиск используют обычные http-запросы
- tag-adder - скрипт, который читает сообщения из хранилища и добавляет к ним тэги, используя языковую модель [snagbreac/russian-reverse-dictionary-semsearch](https://huggingface.co/snagbreac/russian-reverse-dictionary-semsearch). Добавленный тэг сохраняется в хранилище

Для запуска необходимо выполнить команду:

```docker-compose up -d```