# План: Создание Skill для Ledger Development & Testing

## Цель

Создать skill `ledger-development` для Claude Code, который будет автоматически активироваться при работе с ledger, транзакциями и модулями, обеспечивая соблюдение всех правил тестирования и безопасности.

---

## Структура Skill

```
.claude/skills/ledger-development/
├── SKILL.md                           # Основной файл skill (1,500-2,000 слов)
├── references/
│   ├── testing-patterns.md            # Детальные паттерны тестирования
│   ├── security-checklist.md          # Чеклист безопасности
│   └── precision-guide.md             # Работа с big.Int и NUMERIC(78,0)
└── examples/
    ├── handler_test_template.go       # Шаблон теста для handler
    └── concurrent_test_template.go    # Шаблон теста concurrent access
```

---

## Содержимое файлов

### 1. SKILL.md (основной)

**Frontmatter:**
```yaml
---
name: ledger-development
description: This skill should be used when the user asks to "create a new module", "add transaction handler", "implement ledger feature", "write ledger tests", "add concurrent tests", "test financial precision", "check authorization", "fix double-spending", or works on any code in internal/ledger/, internal/module/, or transaction-related code. Provides comprehensive guidance on ledger development, testing patterns, and security requirements for MoonTrack's double-entry accounting system.
---
```

**Body содержит:**
1. **Overview** - Краткое описание ledger системы и ключевых принципов
2. **Testing Requirements** - Обязательные категории тестов при изменении ledger
3. **Security Checklist** - Критические проверки безопасности
4. **Quick Reference** - Таблицы команд и паттернов
5. **References** - Ссылки на детальные guides

### 2. references/testing-patterns.md

Детальное описание:
- **Integration Tests Setup** - TestMain, testDB, helpers
- **Precision Tests** - Тестирование NUMERIC(78,0), big.Int
- **Atomicity Tests** - Rollback, reconciliation, entry immutability
- **Security Tests** - Input validation, negative amounts, future dates
- **Concurrent Tests** - Row-level locking, double-spend prevention
- **Authorization Tests** - Wallet ownership, cross-user access
- **Handler Tests** - Entry generation, price sources, metadata

### 3. references/security-checklist.md

Чеклист безопасности:
- [ ] Row-level locking (`SELECT FOR UPDATE`) для concurrent access
- [ ] Проверка владения wallet (`wallet.UserID == ctx.UserID`)
- [ ] Валидация отрицательных сумм
- [ ] Валидация будущих дат
- [ ] Проверка баланса double-entry (debit == credit)
- [ ] Защита от negative balance
- [ ] Параметризованные SQL запросы

### 4. references/precision-guide.md

Руководство по precision:
- Использование `math/big.Int` вместо float64
- NUMERIC(78,0) в PostgreSQL
- Тестовые значения: 10^78-1, 2^256-1, satoshi, wei
- Конвертация и форматирование

### 5. examples/handler_test_template.go

Шаблон теста handler с:
- Mock services
- Authorization check
- Entry validation
- Price source tracking

### 6. examples/concurrent_test_template.go

Шаблон теста concurrent с:
- sync.WaitGroup
- atomic counters
- Race detector compatible

---

## Trigger Phrases (для description)

Skill активируется когда пользователь говорит:
- "create a new module"
- "add transaction handler"
- "implement ledger feature"
- "write ledger tests"
- "add concurrent tests"
- "test financial precision"
- "check authorization"
- "fix double-spending"
- "add security tests"
- "test atomicity"
- "work on ledger"
- "create new transaction type"

Или работает с файлами:
- `internal/ledger/*`
- `internal/module/*`
- `internal/infra/postgres/*_repo.go`

---

## Ключевые правила из skill

### При создании нового модуля транзакций:

1. **Обязательные тесты:**
   - Integration tests с TestContainers
   - Authorization tests (wallet ownership)
   - Precision tests для amounts
   - Entry validation tests

2. **Security requirements:**
   - Использовать `GetAccountBalanceForUpdate()` в concurrent операциях
   - Проверять `middleware.GetUserIDFromContext()` для авторизации
   - Валидировать все входные данные

3. **Double-entry accounting:**
   - Каждая транзакция создает balanced entries (debit == credit)
   - Использовать `tx.VerifyBalance()` в тестах
   - Тестировать `ReconcileBalance()` после операций

### При изменении существующего кода:

1. Запустить существующие тесты
2. Добавить тесты для новой функциональности
3. Проверить concurrent safety
4. Verify precision preservation

---

## Verification

После создания skill:

```bash
# Проверить структуру
ls -la .claude/skills/ledger-development/

# Проверить SKILL.md frontmatter
head -10 .claude/skills/ledger-development/SKILL.md

# Тест: спросить Claude о создании нового модуля
# Skill должен загрузиться автоматически
```

---

## Файлы для создания

| Файл | Размер | Описание |
|------|--------|----------|
| `SKILL.md` | ~1,800 слов | Core instructions, quick reference |
| `references/testing-patterns.md` | ~3,000 слов | Detailed test patterns |
| `references/security-checklist.md` | ~1,000 слов | Security requirements |
| `references/precision-guide.md` | ~1,500 слов | Big.Int and NUMERIC handling |
| `examples/handler_test_template.go` | ~150 lines | Template for handler tests |
| `examples/concurrent_test_template.go` | ~100 lines | Template for concurrent tests |

---

## Итого

Создание skill `ledger-development` обеспечит:
1. **Автоматическую активацию** при работе с ledger/transaction кодом
2. **Consistent testing** - все тесты следуют одним паттернам
3. **Security by default** - authorization и concurrent safety
4. **Financial precision** - правильная работа с big.Int
5. **Progressive disclosure** - core info в SKILL.md, детали в references
