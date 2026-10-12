module.exports = async (b) => {
  const rows = () => b.eval(`document.querySelectorAll('.table-card tbody tr').length`);
  await b.open('http://127.0.0.1:18080/#/quotas?status=exhausted', 2000);
  console.log('status=exhausted rows:', await rows(), '| select:', await b.eval(`document.querySelectorAll('.toolbar select')[1].value`));
  await b.open('http://127.0.0.1:18080/#/quotas?q=nomatch', 2000);
  console.log('q=nomatch:', await b.eval(`document.querySelector('.table-card .empty h3')?.textContent`));
  await b.open('http://127.0.0.1:18080/#/quotas?mode=key', 2000);
  console.log('mode=key:', await b.eval(`document.querySelector('.table-card .empty h3')?.textContent`));
  await b.open('http://127.0.0.1:18080/#/quotas?mode=key&unlimited=1', 2000);
  console.log('mode=key&unlimited=1 rows:', await rows());
  // UI control writes the URL
  await b.open('http://127.0.0.1:18080/#/quotas', 2000);
  await b.eval(`(() => { const s = document.querySelectorAll('.toolbar select')[1]; s.value = 'exhausted'; s.dispatchEvent(new Event('change')); })()`);
  await b.wait(600);
  console.log('hash after choosing status:', await b.eval('location.hash'));
  // collapse toggle
  await b.eval(`document.querySelector('.table-card tbody button[aria-expanded]').click()`);
  console.log('rows after collapse:', await rows());
  // dashboard link
  await b.open('http://127.0.0.1:18080/#/dashboard', 3000);
  await b.eval(`[...document.querySelectorAll('.quota-card button')].find(x => x.textContent.includes('View all')).click()`);
  await b.wait(800);
  console.log('View all ->', await b.eval('location.hash'));
};
