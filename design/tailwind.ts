/**
 * MoonTrack Tailwind Config Extension
 *
 * Добавь этот extend в твой tailwind.config.js:
 *
 * module.exports = {
 *   // ... остальная конфигурация
 *   theme: {
 *     extend: moontrackTheme.extend
 *   }
 * }
 */

export const moontrackTheme = {
  extend: {
    colors: {
      // Основные цвета (уже есть в shadcn, но для явного использования)
      background: "hsl(var(--background))",
      foreground: "hsl(var(--foreground))",

      // Семантические цвета
      profit: "hsl(var(--profit))",
      loss: "hsl(var(--loss))",

      // Цвета типов транзакций
      "tx-liquidity": "hsl(var(--tx-liquidity))",
      "tx-gm-pool": "hsl(var(--tx-gm-pool))",
      "tx-transfer": "hsl(var(--tx-transfer))",
      "tx-swap": "hsl(var(--tx-swap))",
      "tx-bridge": "hsl(var(--tx-bridge))",

      // Background варианты для badges
      "tx-liquidity-bg": "hsl(var(--tx-liquidity-bg))",
      "tx-gm-pool-bg": "hsl(var(--tx-gm-pool-bg))",
      "tx-transfer-bg": "hsl(var(--tx-transfer-bg))",
      "tx-swap-bg": "hsl(var(--tx-swap-bg))",
      "tx-bridge-bg": "hsl(var(--tx-bridge-bg))",
    },

    fontFamily: {
      sans: ['Inter', 'system-ui', '-apple-system', 'sans-serif'],
      mono: ['JetBrains Mono', 'Fira Code', 'monospace'],
    },

    borderColor: {
      DEFAULT: "hsl(var(--border))",
      hover: "hsl(var(--border-hover))",
    },

    // Анимации для hover эффектов
    transitionProperty: {
      'colors-border': 'color, background-color, border-color',
    },
  },
};

/**
 * Полный пример tailwind.config.js:
 *
 * import { moontrackTheme } from './moontrack-theme';
 *
 * export default {
 *   darkMode: ["class"],
 *   content: [
 *     "./index.html",
 *     "./src/**\/*.{js,ts,jsx,tsx}",
 *   ],
 *   theme: {
 *     extend: {
 *       ...moontrackTheme.extend,
 *       // твои дополнительные расширения
 *     },
 *   },
 *   plugins: [require("tailwindcss-animate")],
 * }
 */
