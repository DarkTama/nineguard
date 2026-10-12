const http = require('http');
http.createServer((req, res) => {
  let b = ''; req.on('data', (c) => b += c);
  req.on('end', () => {
    res.setHeader('Content-Type', 'application/json');
    if (req.url.endsWith('/models')) return res.end(JSON.stringify({ data: ['opus', 'haiku', 'gpt-4o', 'old'].map((id) => ({ id })) }));
    res.end(JSON.stringify({ id: 'x', choices: [{ message: { role: 'assistant', content: 'ok' } }], usage: { prompt_tokens: 10, completion_tokens: 20, total_tokens: 30 } }));
  });
}).listen(18999, () => console.log('upstream up'));
