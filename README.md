# gormdb — адаптер GORM

`DB(ctx)` (tx внутри `WithinTx`), любой `Dialector` (по умолчанию postgres), `Ping/Close`, `app.Component`. Escape hatch — `Native()`.

Зависит от [database](https://github.com/SaveliiYam/database).

## Установка

```bash
go get github.com/SaveliiYam/gormdb@v0.1.0
```

Требуется Go ≥ 1.25. Лицензия: см. репозиторий.
