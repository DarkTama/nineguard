import { h, icon } from './ui.js';

export const PROVIDER_ICONS = [['','Auto'],['9router','9router'],['omniroute','OmniRoute'],['nineguard','NineGuard'],['claude','Claude'],['anthropic','Anthropic'],['openai','OpenAI'],['openrouter','OpenRouter'],['ollama','Ollama'],['gemini','Gemini'],['generic','Generic'],['custom','Custom image URL']];

export function providerMark(provider) {
  const box = h('span', { class: 'provider-mark', 'aria-hidden': 'true' });
  const fallback = () => box.replaceChildren(h('span', null, (provider.name || 'P').slice(0, 2).toUpperCase()));
  let choice = provider.icon || (provider.family === 'codex' ? 'openai' : provider.family) || '';
  if (!choice) {
    const text = `${provider.name} ${provider.route} ${provider.quota_type}`.toLowerCase();
    choice = PROVIDER_ICONS.find(([id]) => id && !['custom', 'generic'].includes(id) && text.includes(id))?.[0] || 'generic';
  }
  const local = { nineguard: '/appicon.png', '9router': '/provider-icons/9router.svg', omniroute: '/provider-icons/omniroute.svg', claude: '/provider-icons/claude.svg', anthropic: '/provider-icons/anthropic.svg', openai: '/provider-icons/openai.svg', openrouter: '/provider-icons/openrouter.svg', ollama: '/provider-icons/ollama.svg', gemini: '/provider-icons/gemini.svg' };
  let src = local[choice];
  if (!src) { try { const u = new URL(choice); if (['https:', 'http:'].includes(u.protocol)) src = u.href; } catch {} }
  if (src) box.append(h('img', { src, alt: '', referrerpolicy: 'no-referrer', onerror: fallback }));
  else box.append(icon('server'));
  return box;
}
