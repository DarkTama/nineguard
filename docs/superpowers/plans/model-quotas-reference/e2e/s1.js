// Load Quotas page, dashboard, endpoints: capture errors and screenshots.
module.exports = async (b) => {
  await b.open('http://127.0.0.1:18080/#/quotas', 2500);
  console.log('quotas h1:', await b.eval(`document.querySelector('h1')?.textContent`));
  console.log('quotas rows:', await b.eval(`document.querySelectorAll('tbody tr').length`));
  console.log('cards:', await b.eval(`[...document.querySelectorAll('.grid-4 .card')].map(c=>c.innerText.replace(/\\n/g,' ')).join(' | ')`));
  await b.shot('quotas.png');
  await b.open('http://127.0.0.1:18080/#/dashboard', 3000);
  console.log('quota card visible:', await b.eval(`(()=>{const c=document.querySelector('.quota-card');return c&&c.style.display!=='none'?c.innerText.replace(/\\n/g,' | '):'hidden'})()`));
  await b.shot('dashboard.png');
  await b.open('http://127.0.0.1:18080/#/endpoints', 2500);
  console.log('keys quota col:', await b.eval(`[...document.querySelectorAll('table.table tbody tr')].map(r=>r.children[5]?.innerText.replace(/\\n/g,' / ')).join(' || ')`));
  await b.shot('endpoints.png');
};
