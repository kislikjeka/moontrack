# План: Пересборка WalletCard компонента

## Проблема

WalletCard использует inline-стили вместо Tailwind классов. Это было добавлено как временный тест для диагностики, но осталось в коде. Inline-стили конфликтуют с Tailwind и создают непредсказуемое поведение.

## Решение

Полностью пересобрать WalletCard используя только Tailwind классы, по образцу WalletCardCompact который работает корректно.

## Файлы для изменения

- `apps/frontend/src/components/domain/WalletCard.tsx` - полная пересборка

## Референсы

- `WalletCardCompact` в том же файле - работающий образец
- `Sidebar.tsx` - использует `flex flex-col gap-1` паттерн

## Изменения

### WalletCard.tsx

Заменить inline-стили на Tailwind классы:

```tsx
<CardContent className="p-4">
  {/* Header row: chain badge + info | status + link */}
  <div className="flex items-start justify-between gap-3">
    {/* Left: chain badge + wallet info */}
    <div className="flex items-center gap-3 min-w-0">
      <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary font-mono text-xs font-medium flex-shrink-0">
        {chainLabel}
      </div>
      <div className="min-w-0">
        <h3 className="font-medium truncate">{wallet.name}</h3>
        {wallet.address && (
          <AddressDisplay
            address={wallet.address}
            className="text-sm text-muted-foreground"
          />
        )}
      </div>
    </div>
    {/* Right: status badge + external link */}
    <div className="flex items-center gap-2 flex-shrink-0">
      <SyncStatusBadge status={wallet.sync_status} />
      <ExternalLink className="h-4 w-4 text-muted-foreground" />
    </div>
  </div>

  {/* Footer: balance + sync time */}
  <div className="mt-4 flex items-end justify-between">
    <div>
      <p className="text-2xl font-semibold">{formatUSD(numValue)}</p>
      <p className="text-sm text-muted-foreground">
        {assetCount} {assetCount === 1 ? 'asset' : 'assets'}
      </p>
    </div>
    {wallet.last_sync_at && (
      <p className="text-xs text-muted-foreground">
        Synced {formatRelativeDate(wallet.last_sync_at)}
      </p>
    )}
  </div>
</CardContent>
```

Ключевые изменения:
1. Убрать все `style={{ }}` атрибуты
2. Использовать `flex items-start justify-between gap-3` для основного контейнера
3. Использовать `flex items-center gap-3 min-w-0` для левой части (badge + info)
4. Использовать `flex items-center gap-2 flex-shrink-0` для правой части (status + icon)
5. Добавить `min-w-0` на левый контейнер для корректной работы truncate

## Верификация

1. Запустить dev сервер: `bun run dev`
2. Открыть страницу /wallets
3. Проверить что:
   - ETH badge и "Main" с адресом на одной линии слева
   - "Pending" badge и иконка ссылки на одной линии справа
   - При hover карточка подсвечивается корректно
   - На узких экранах контент не ломается
