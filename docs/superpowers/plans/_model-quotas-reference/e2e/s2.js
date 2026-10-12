// Key modal: open edit via ?edit= deep link, use the picker, validation, save, mode switch.
module.exports = async (b) => {
  const id = require('fs').readFileSync('id.txt', 'utf8');
  await b.open('http://127.0.0.1:18080/#/quotas', 1500);
  await b.eval(`document.querySelector('a[href^="#/endpoints?edit="]').click()`);
  await b.wait(2500);
  console.log('url after deep link:', await b.eval('location.hash'));
  console.log('modal title:', await b.eval(`document.querySelector('.dialog h3')?.textContent`));
  console.log('modes:', await b.eval(`[...document.querySelectorAll('.dialog .tabs .tab')].map(t=>t.textContent+(t.classList.contains('active')?'*':'')).join(' | ')`));
  console.log('rows loaded:', await b.eval(`[...document.querySelectorAll('.dialog table tbody tr')].map(r=>r.children[0].innerText.split('\\n')[0]).join(', ')`));
  await b.shot('modal-model.png');

  // velocity on focus
  await b.eval(`document.querySelector('.dialog table tbody input[type=number]').dispatchEvent(new Event('focus'))`);
  await b.wait(800);
  console.log('velocity text:', await b.eval(`document.querySelector('.dialog .qrow-sub')?.innerText.replace(/\\n/g,' ')`));

  // picker
  await b.eval(`[...document.querySelectorAll('.dialog button')].find(x=>x.textContent.includes('Add Model')).click()`);
  await b.wait(300);
  console.log('picker options:', await b.eval(`[...document.querySelectorAll('.dialog .card label')].map(l=>l.innerText.trim()).join(', ')`));
  await b.eval(`document.querySelector('.dialog .card input[type=checkbox]').click()`);
  await b.eval(`[...document.querySelectorAll('.dialog button')].find(x=>x.textContent==='Add selected').click()`);
  await b.wait(300);
  console.log('rows after add:', await b.eval(`document.querySelectorAll('.dialog table tbody tr').length`));

  // save with empty limit -> blocked
  await b.eval(`[...document.querySelectorAll('.dialog button[type=submit]')][0].click()`);
  await b.wait(500);
  console.log('error on empty limit:', await b.eval(`document.querySelector('.dialog .form-error')?.textContent`));
  await b.shot('modal-error.png');

  // fill the limit and save
  await b.eval(`(()=>{const i=[...document.querySelectorAll('.dialog table tbody input[type=number]')].pop(); i.value='777'; i.dispatchEvent(new Event('input'));})()`);
  await b.eval(`document.querySelector('.dialog button[type=submit]').click()`);
  await b.wait(1500);
  console.log('dialog closed:', await b.eval(`!document.querySelector('.dialog')`));
  console.log('hash cleaned:', await b.eval('location.hash'));
};
