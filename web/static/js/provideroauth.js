import { api } from './api.js';
import { h, icon, toast, formDialog, copy } from './ui.js';

export async function openProviderLogin(provider, account, onChanged) {
  // Open on the click gesture, then navigate after the server supplies the authorization URL.
  const popup = window.open('about:blank', '_blank');
  let session;
  try {
    session = await api.post(`/providers/${provider.id}/oauth/sessions`, { mode: 'browser', account_id: account?.id || '' });
  } catch (err) { popup?.close(); toast(err.message, 'error'); return; }
  if (popup) { popup.opener = null; popup.location = session.authorization_url; }
  const status = h('p', { class: 'note', role: 'status' }, 'Waiting for authorization in your browser...');
  const callback = h('input', { class: 'input', type: 'url', placeholder: 'Paste the complete callback URL', 'aria-label': 'Authorization callback URL', autocomplete: 'off', spellcheck: 'false' });
  const manual = h('button', { class: 'btn btn-sm', type: 'button', onclick: async () => {
    try { await api.post(`/providers/${provider.id}/oauth/sessions/${session.id}/callback`, { callback_url: callback.value }); callback.value = ''; status.textContent = 'Finishing authorization...'; }
    catch (err) { status.textContent = err.message; }
  } }, 'Finish login');
  const open = h('a', { class: 'btn btn-sm', href: session.authorization_url, target: '_blank', rel: 'noopener noreferrer' }, 'Open authorization');
  let finished = false, stopped = false, timer;
  const poll = async () => {
    if (stopped) return;
    try {
      const s = await api.get(`/providers/${provider.id}/oauth/sessions/${session.id}`);
      if (stopped) return;
      if (s.state === 'connected') { finished = true; status.textContent = 'Account connected. You can close this dialog.'; await onChanged(); return; }
      if (['failed', 'expired', 'cancelled'].includes(s.state)) { finished = true; status.textContent = s.error || `Login ${s.state}. Close this dialog and try again.`; return; }
    } catch (err) { status.textContent = err.message; }
    if (!stopped) timer = setTimeout(poll, 1500);
  };
  timer = setTimeout(poll, 1000);
  await formDialog({ title: `${account ? 'Reconnect' : 'Connect'} ${provider.name}`, submitText: 'Close', fields: [{ node: h('div', { class: 'provider-login' },
    status,
    h('div', { class: 'provider-actions' }, open, h('button', { class: 'btn btn-sm', type: 'button', onclick: () => copy(session.authorization_url, 'Authorization URL copied') }, icon('copy'), 'Copy URL')),
    h('p', { class: 'muted' }, 'Using a remote NineGuard server? After consent, your browser may show a localhost connection error. Copy that page’s full URL and paste it below.'),
    callback, manual,
    h('p', { class: 'muted' }, 'Your provider’s subscription and usage terms still apply. Disconnecting forgets the local token; it does not necessarily revoke the provider grant.')
  ) }], onSubmit: () => true });
  stopped = true; clearTimeout(timer);
  if (!finished) { try { await api.del(`/providers/${provider.id}/oauth/sessions/${session.id}`); } catch (err) { toast(err.message, 'warn'); await onChanged(); } }
}
