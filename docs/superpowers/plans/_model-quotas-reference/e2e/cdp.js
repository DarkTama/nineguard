// Minimal Chrome DevTools driver: node cdp.js <script.js>. Script gets { open, eval, shot, wait, errors }.
const fs = require('fs');

async function connect() {
  const tabs = await (await fetch('http://127.0.0.1:19222/json')).json();
  const tab = tabs.find((t) => t.type === 'page') || tabs[0];
  const ws = new WebSocket(tab.webSocketDebuggerUrl);
  await new Promise((r) => (ws.onopen = r));
  let id = 0;
  const pending = new Map();
  const errors = [];
  ws.onmessage = (m) => {
    const d = JSON.parse(m.data);
    if (d.id && pending.has(d.id)) { pending.get(d.id)(d); pending.delete(d.id); return; }
    if (d.method === 'Runtime.exceptionThrown') errors.push('EXC ' + (d.params.exceptionDetails.exception?.description || d.params.exceptionDetails.text));
    if (d.method === 'Runtime.consoleAPICalled' && d.params.type === 'error') errors.push('console.error ' + d.params.args.map((a) => a.value ?? a.description).join(' '));
    if (d.method === 'Log.entryAdded' && d.params.entry.level === 'error') errors.push('log ' + d.params.entry.text + ' ' + (d.params.entry.url || ''));
  };
  const send = (method, params = {}) => new Promise((res) => { const i = ++id; pending.set(i, res); ws.send(JSON.stringify({ id: i, method, params })); });
  await send('Network.enable'); await send('Network.setCacheDisabled', { cacheDisabled: true }); await send('Page.enable'); await send('Runtime.enable'); await send('Log.enable');
  await send('Emulation.setDeviceMetricsOverride', { width: 1400, height: 1000, deviceScaleFactor: 1, mobile: false });
  const wait = (ms) => new Promise((r) => setTimeout(r, ms));
  return {
    errors,
    wait,
    async open(url, ms = 1500) { await send('Page.navigate', { url: 'about:blank' }); await wait(200); await send('Page.navigate', { url }); await wait(ms); },
    async eval(expr) {
      const r = await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true });
      if (r.result.exceptionDetails) throw new Error(r.result.exceptionDetails.exception?.description || 'eval failed');
      return r.result.result.value;
    },
    async shot(file) { const r = await send('Page.captureScreenshot', { format: 'png' }); fs.writeFileSync(file, Buffer.from(r.result.data, 'base64')); },
    async theme(t) { await this.eval(`document.documentElement.dataset.theme='${t}'`); },
    close() { ws.close(); },
  };
}

(async () => {
  const b = await connect();
  await require(require('path').resolve(process.argv[2]))(b);
  if (b.errors.length) console.log('PAGE ERRORS:\n' + b.errors.join('\n'));
  else console.log('no page errors');
  b.close();
  process.exit(0);
})().catch((e) => { console.error('DRIVER FAILED', e.message); process.exit(1); });
