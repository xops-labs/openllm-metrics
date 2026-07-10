import type { Config } from 'tailwindcss';

const config: Config = {
  content: ['./app/**/*.{ts,tsx}', './components/**/*.{ts,tsx}', './lib/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // Minimal neutral palette - internal admin tool, not a marketing surface.
        canvas: '#0f1115',
        panel: '#171a21',
        border: '#262a33',
        text: '#e5e7eb',
        muted: '#9ca3af',
        accent: '#3b82f6',
        danger: '#ef4444',
        warn: '#f59e0b',
        ok: '#10b981',
      },
      fontFamily: {
        sans: ['ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['ui-monospace', 'SFMono-Regular', 'monospace'],
      },
    },
  },
  plugins: [],
};

export default config;
