// Tailwind config for the static site. Build once with scripts/build-site-css.sh;
// the generated docs/site/tailwind.css is committed so Pages serves it directly.
module.exports = {
  content: ['docs/index.html'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        ink: { 950: '#020617', 900: '#0B1120', 800: '#0F172A', 700: '#1E293B' },
        line: '#1F2937',
        neon: { green: '#22C55E', magenta: '#A855F7', cyan: '#22D3EE', red: '#F43F5E' },
      },
      fontFamily: {
        display: ['"Russo One"', 'system-ui', 'sans-serif'],
        body: ['"Chakra Petch"', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'monospace'],
      },
      boxShadow: {
        'neon-green': '0 0 0 1px rgba(34,197,94,0.35), 0 0 24px rgba(34,197,94,0.25)',
        'neon-magenta': '0 0 0 1px rgba(168,85,247,0.35), 0 0 28px rgba(168,85,247,0.25)',
      },
    },
  },
};
