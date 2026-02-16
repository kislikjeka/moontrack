# MoonTrack Design System Guide

## Обзор

MoonTrack — DeFi Portfolio Tracker с double-entry ledger. 
UI построен на **shadcn/ui + Tailwind CSS** с кастомными темами.

## Визуальный стиль

### Общие принципы
- **Две темы** — светлая и тёмная, переключаются через `<ThemeToggle />`
- **Минимализм** — много воздуха, простые формы
- **Карточки** — с тонким бордером, без теней, subtle hover
- **Цветовое кодирование** — типы транзакций и profit/loss имеют свои цвета

### Не делать
- Не использовать тени (drop-shadow) — только бордеры
- Не использовать градиенты на фонах
- Не перегружать UI декоративными элементами
- Не хардкодить цвета — использовать CSS-переменные для корректной работы тем

## Цветовая палитра

### Основные цвета
| Назначение | CSS переменная | Использование |
|------------|----------------|---------------|
| Primary | `--primary` | Кнопки, ссылки, акценты (бирюзовый) |
| Background | `--background` | Основной фон страницы |
| Card | `--card` | Фон карточек |
| Border | `--border` | Границы элементов |

### Семантические цвета
| Назначение | CSS переменная | Tailwind класс |
|------------|----------------|----------------|
| Profit | `--profit` | `text-profit` |
| Loss | `--loss` | `text-loss` |

### Цвета типов транзакций
| Тип | CSS переменная | Tailwind класс |
|-----|----------------|----------------|
| Liquidity Pool | `--tx-liquidity` | `text-tx-liquidity` (фиолетовый) |
| GM Pool | `--tx-gm-pool` | `text-tx-gm-pool` (оранжевый) |
| Transfer | `--tx-transfer` | `text-tx-transfer` (серый) |
| Swap | `--tx-swap` | `text-tx-swap` (зелёный) |
| Bridge | `--tx-bridge` | `text-tx-bridge` (бирюзовый) |

## Domain-компоненты

При создании UI используй готовые domain-компоненты:

### `<TransactionTypeBadge type="swap" />`
Badge для типа транзакции. Автоматически применяет правильный цвет и иконку.

```tsx
<TransactionTypeBadge type="liquidity" />
<TransactionTypeBadge type="swap" size="lg" />
<TransactionTypeBadge type="bridge" showLabel={false} />
```

### `<PnLValue value={123.45} />`
Отображение прибыли/убытка с правильным цветом.

```tsx
<PnLValue value={1500} />                    // Зелёный: +1,500.00
<PnLValue value={-200} showIcon />           // Красный с иконкой
<PnLValue value={5.5} isPercent showIcon />  // +5.50% с трендом
```

### `<StatCard label="..." value={...} icon={...} />`
KPI-карточка для дашборда.

```tsx
<StatCard
  label="Кошельков"
  value={3}
  icon={Wallet}
  iconColor="primary"
/>
```

### `<ThemeToggle />`
Переключатель светлой/тёмной темы. Сохраняет выбор в localStorage.

```tsx
// В header приложения
<ThemeToggle />

// Оберни приложение в ThemeProvider для корректной инициализации
<ThemeProvider>
  <App />
</ThemeProvider>
```

## Паттерны UI

### Карточки
```tsx
<div className="rounded-lg border border-border bg-card p-4 hover:border-border-hover transition-colors">
  {/* content */}
</div>
```

### Пустые состояния
```tsx
<div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
  <IconComponent className="h-12 w-12 mb-4 opacity-50" />
  <p>Описание пустого состояния</p>
</div>
```

### Числа и валюты
- Большие числа: `text-2xl font-semibold`
- Используй `Intl.NumberFormat` для форматирования
- Для крипто-сумм показывай до 6 знаков после запятой если нужно

### Даты
- Формат: `DD.MM.YYYY` (европейский)
- Для недавних: "сегодня", "вчера", "3 дня назад"

## Tailwind расширения

В `tailwind.config.js` добавлены кастомные цвета:

```js
colors: {
  profit: "hsl(var(--profit))",
  loss: "hsl(var(--loss))",
  "tx-liquidity": "hsl(var(--tx-liquidity))",
  "tx-gm-pool": "hsl(var(--tx-gm-pool))",
  "tx-transfer": "hsl(var(--tx-transfer))",
  "tx-swap": "hsl(var(--tx-swap))",
  "tx-bridge": "hsl(var(--tx-bridge))",
}
```

## Иконки

Используем **Lucide React**. Стандартные размеры:
- В тексте: 16px
- В кнопках: 18px  
- В карточках: 20px
- Декоративные: 24px+

## Типографика

- Основной шрифт: Inter
- Моноширинный (адреса, хеши): JetBrains Mono
- Заголовки: `font-semibold`
- Обычный текст: `font-normal`

## Примеры запросов к Claude Code

### Создать новый экран
> "Создай страницу с деталями позиции LP. Покажи: название пула, вложенные токены, текущую стоимость, PnL, историю операций"

### Добавить компонент
> "Добавь компонент WalletCard для отображения кошелька в списке: иконка сети, короткий адрес, баланс в USD, количество транзакций"

### Улучшить существующий UI
> "Переделай таблицу транзакций: добавь TransactionTypeBadge, форматирование сумм через PnLValue, группировку по дате"
